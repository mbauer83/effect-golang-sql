package sql

// An aggregate kept in the tables its mapping describes: saved, found and
// deleted, and listed.
//
// Nothing here names a column or a table. The tables are the ones DDL makes of
// the mapping -- the root, a table for each list or single of entities beneath
// it, and a join table for each list of references -- and the dialect says
// which they are; the values are the domain schema's encoding of the
// aggregate; the key is the column the identity is stored in.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang/effect"
)

// Repository is where aggregates of one kind are kept, found by their
// identity or by what a criterion says of their root.
//
// Every operation runs in the transaction it is given, or in one of its own
// when it is given a database, so several compose into one.
type Repository[A, ID any] struct {
	mapping  Mapping[A]
	stored   schema.Schema[A]
	identity schema.Schema[ID]
	source   Source
	// key are the root's identity columns, in the order declared.
	key    []string
	tables *layouts
	// computed are the root's columns the database fills; alsoUnique says
	// the root has a unique key besides its identity.
	computed   map[string]bool
	alsoUnique bool
	fault      error
}

// NewRepository is where aggregates the mapping describes are kept, found by
// their identity: the root's identity fields, as the identity schema writes a
// value of it -- one value for one field (a field's Shape), an object naming
// each for several.
func NewRepository[A, ID any](mapping Mapping[A], identity schema.Schema[ID]) Repository[A, ID] {
	stored := mapping.Schema()
	repository := Repository[A, ID]{
		mapping: mapping, stored: stored, identity: identity,
		tables:   &layouts{node: stored.Structure()},
		computed: map[string]bool{},
	}
	object, isObject := stored.Structure().(structure.Object)
	if !isObject {
		repository.fault = errors.New("sql: a repository keeps an object")
		return repository
	}
	kinds := make([]ColumnType, 0, len(object.Fields))
	for _, field := range object.Fields {
		if heldBeneath(field.Node) {
			continue
		}
		kinds = append(kinds, ColumnOf(field.Name, KindOf(field.Node)))
		if field.Identity {
			repository.key = append(repository.key, field.Name)
		}
		if field.Computed {
			repository.computed[field.Name] = true
		}
		if !field.Identity && (field.Unique || field.UniqueKey != "") {
			repository.alsoUnique = true
		}
	}
	if len(repository.key) == 0 {
		repository.fault = errors.New("sql: a repository's aggregate has an identity: mark its fields Identity")
	}
	repository.source = From(mapping.TableName(), kinds...)
	return repository
}

// heldBeneath says a member is kept in a table of its own rather than a
// column: entities, and a list of references.
func heldBeneath(node structure.Node) bool {
	if _, entity := structure.EntityBehind(node); entity {
		return true
	}
	if sequence, isSequence := node.(structure.Sequence); isSequence {
		scalar, isScalar := sequence.Element.(structure.Scalar)
		return isScalar && scalar.Refers != nil
	}
	return false
}

// Structure is the aggregate's tables as DDL makes them.
func (repository Repository[A, ID]) Structure() structure.Node { return repository.stored.Structure() }

// TableName is the root table's.
func (repository Repository[A, ID]) TableName() string { return repository.mapping.TableName() }

// Source is the root table, as a query reads it: a listing of a summary's
// schema reads it rather than whole aggregates.
func (repository Repository[A, ID]) Source() Source { return repository.source }

// Of is a field's column in the root table, for a filter or a sort.
func (repository Repository[A, ID]) Of[B any](field schema.Field[A, B]) Expr[B] {
	return repository.mapping.Of(repository.source, field)
}

// Listing is the aggregates read a page at a time, keyed by their identity,
// each whole: what a page's aggregates hold beneath them is read with one
// statement per table for the page. The sorts, sizes and searches it offers
// are the caller's to declare.
func (repository Repository[A, ID]) Listing() Listing[A] {
	listing := NewListing(repository.stored, repository.source, repository.key...)
	listing.tree = repository.tables
	if repository.fault != nil {
		listing.fault = repository.fault
	}
	return listing
}

// Find is the aggregate with that identity, whole, or a fault that is
// ErrNoRows when none is kept.
func (repository Repository[A, ID]) Find[R any](database Querier, spelling Spelling, identity ID) effect.Effect[R, Fault, A] {
	return repository.FindOneBy[R](database, spelling, repository.identityIs(identity))
}

// FindOneBy is the one aggregate whose root the criterion finds, whole: a
// fault that is ErrNoRows when there is none, and ErrSeveralRows when there
// are more -- a criterion that is not a key is asking FindBy's question.
func (repository Repository[A, ID]) FindOneBy[R any](database Querier, spelling Spelling, where Criterion) effect.Effect[R, Fault, A] {
	return repository.FindBy[R](database, spelling, where).
		FlatMap(func(found []A) effect.Effect[R, Fault, A] {
			switch len(found) {
			case 1:
				return effect.For[R, Fault]().Succeed(found[0])
			case 0:
				return effect.For[R, Fault]().Fail[A](faultOf("find an aggregate", repository.TableName(), ErrNoRows))
			default:
				return effect.For[R, Fault]().Fail[A](faultOf("find an aggregate", repository.TableName(), ErrSeveralRows))
			}
		})
}

// FindBy is every aggregate whose root the criterion finds, whole, in that
// order: for a set the criterion keeps small -- one owner's copies of a film.
// A set that grows without bound is a listing's, read a page at a time.
func (repository Repository[A, ID]) FindBy[R any](database Querier, spelling Spelling, where Criterion, order ...Ordering) effect.Effect[R, Fault, []A] {
	laid, err := repository.layout(spelling)
	if err != nil {
		return effect.For[R, Fault]().Fail[[]A](faultOf("find an aggregate", repository.TableName(), err))
	}
	return repository.kept[R](database, spelling, laid, where, order...).
		FlatMap(func(read [][]dynamic.Object) effect.Effect[R, Fault, []A] {
			found := make([]A, 0, len(read[0]))
			for _, root := range assemble(laid, read) {
				value, err := schema.FromDynamic(repository.stored, root)
				if err != nil {
					return effect.For[R, Fault]().Fail[[]A](faultOf("find an aggregate", repository.TableName(), err))
				}
				found = append(found, value)
			}
			return effect.For[R, Fault]().Succeed(found)
		})
}

// kept is what is kept of the aggregates the criterion finds: their root rows,
// in that order, and every row beneath them.
func (repository Repository[A, ID]) kept[R any](database Querier, spelling Spelling, laid []TableLayout, where Criterion, order ...Ordering) effect.Effect[R, Fault, [][]dynamic.Object] {
	source := From(laid[0].Name, laid[0].Columns...)
	statement := SelectQuery{Select: source.Columns(), From: source, Where: where, OrderBy: order}.Statement(spelling)
	return effect.RunCollect(rawRows[R](database, statement)).
		FlatMap(func(roots []dynamic.Object) effect.Effect[R, Fault, [][]dynamic.Object] {
			return readTree[R](database, spelling, laid, roots)
		})
}

// layout is the aggregate's tables in this dialect.
func (repository Repository[A, ID]) layout(spelling Spelling) ([]TableLayout, error) {
	if repository.fault != nil {
		return nil, repository.fault
	}
	return repository.tables.of(spelling)
}

// rowsOf is the aggregate as each of its tables' rows.
func (repository Repository[A, ID]) rowsOf(spelling Spelling, value A) ([]TableLayout, [][]dynamic.Object, error) {
	laid, err := repository.layout(spelling)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := schema.ToDynamic(repository.stored, value)
	if err != nil {
		return nil, nil, err
	}
	object, isObject := encoded.(dynamic.Object)
	if !isObject {
		return nil, nil, errors.New("sql: an aggregate is written as an object")
	}
	return laid, takeApart(laid, object), nil
}

// identityIs is the root row of that identity, bound as the identity's schema
// writes it: one value for one column, or a member for each.
func (repository Repository[A, ID]) identityIs(identity ID) Criterion {
	written, err := schema.ToDynamic(repository.identity, identity)
	if err != nil {
		return Refuse[bool](err)
	}
	table := repository.TableName()
	object, several := written.(dynamic.Object)
	if !several {
		if len(repository.key) != 1 {
			return Refuse[bool](fmt.Errorf("sql: %s is identified by %s, and one value was given", table, strings.Join(repository.key, ", ")))
		}
		return columnIs(table, repository.key[0], written)
	}
	criteria := make([]Criterion, 0, len(object.Fields))
	for _, member := range object.Fields {
		column := repository.mapping.columnName(member.Name)
		if !holds(repository.key, column) {
			return Refuse[bool](fmt.Errorf("sql: %s is identified by %s, and not by %s", table, strings.Join(repository.key, ", "), column))
		}
		criteria = append(criteria, columnIs(table, column, member.Value))
	}
	if len(criteria) != len(repository.key) {
		return Refuse[bool](fmt.Errorf("sql: %s is identified by %s together", table, strings.Join(repository.key, ", ")))
	}
	return And(criteria...)
}
