package sql

// An aggregate kept in the tables its mapping describes: saved, found and
// deleted by its identity, and listed.
//
// Nothing here names a column or a table. The tables are the ones DDL makes of
// the mapping -- the root, a table for each list or single of entities beneath
// it, and a join table for each list of references -- and the dialect says
// which they are; the values are the domain schema's encoding of the
// aggregate; the key is the column the identity is stored in.

import (
	"errors"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang/effect"
)

// Repository is where aggregates of one kind are kept, found by their
// identity.
type Repository[A, ID any] struct {
	mapping  Mapping[A]
	stored   schema.Schema[A]
	identity schema.Field[A, ID]
	source   Source
	key      string
	tables   *layouts
	fault    error
}

// NewRepository is where aggregates the mapping describes are kept, found by
// the identity field.
func NewRepository[A, ID any](mapping Mapping[A], identity schema.Field[A, ID]) Repository[A, ID] {
	stored := mapping.Schema()
	repository := Repository[A, ID]{
		mapping: mapping, stored: stored, identity: identity,
		key:    mapping.columnName(identity.Name()),
		tables: &layouts{node: stored.Structure()},
	}
	object, isObject := stored.Structure().(structure.Object)
	if !isObject {
		repository.fault = errors.New("sql: a repository keeps an object")
		return repository
	}
	kinds := make([]ColumnType, 0, len(object.Fields))
	for _, field := range object.Fields {
		if !heldBeneath(field.Node) {
			kinds = append(kinds, ColumnOf(field.Name, KindOf(field.Node)))
		}
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

// Source is the root table, as a query reads it.
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
	listing := NewListing(repository.stored, repository.source, repository.key)
	listing.tree = repository.tables
	if repository.fault != nil {
		listing.fault = repository.fault
	}
	return listing
}

// SaveRoot writes the aggregate's root row alone -- inserted, or replaced
// under the same identity -- and leaves what it holds beneath it as it is: one
// statement, for a change to the root's own members.
func (repository Repository[A, ID]) SaveRoot[R any](database Querier, spelling Spelling, value A) effect.Effect[R, Fault, Outcome] {
	laid, rows, err := repository.rowsOf(spelling, value)
	if err != nil {
		return effect.For[R, Fault]().Fail[Outcome](faultOf("save an aggregate", repository.TableName(), err))
	}
	return Run[R](database, upsertRow(spelling, laid[0], rows[0][0]))
}

// Save writes the whole aggregate, in one transaction: the root inserted or
// replaced, and beneath it only what changed. What is kept is read -- one
// statement per table -- and compared by key: a row gone is deleted, a row new
// or changed is written, a row the same is left alone.
func (repository Repository[A, ID]) Save[R any](database Querier, spelling Spelling, value A) effect.Effect[R, Fault, effect.Unit] {
	laid, desired, err := repository.rowsOf(spelling, value)
	if err != nil {
		return effect.For[R, Fault]().Fail[effect.Unit](faultOf("save an aggregate", repository.TableName(), err))
	}
	root, _ := desired[0][0].Member(repository.key)
	return inTransaction[R](database, func(within Querier) effect.Effect[R, Fault, effect.Unit] {
		return repository.kept[R](within, spelling, laid, rootIs(laid[0], repository.key, root)).
			FlatMap(func(existing [][]dynamic.Object) effect.Effect[R, Fault, effect.Unit] {
				return runAll[R](within, changes(spelling, laid, existing, desired))
			})
	})
}

// Find is the aggregate with that identity, whole, or a fault that is
// ErrNoRows when none is kept.
func (repository Repository[A, ID]) Find[R any](database Querier, spelling Spelling, identity ID) effect.Effect[R, Fault, A] {
	laid, err := repository.layout(spelling)
	if err != nil {
		return effect.For[R, Fault]().Fail[A](faultOf("find an aggregate", repository.TableName(), err))
	}
	return repository.kept[R](database, spelling, laid, repository.identityIs(identity)).
		FlatMap(func(read [][]dynamic.Object) effect.Effect[R, Fault, A] {
			roots := assemble(laid, read)
			if len(roots) == 0 {
				return effect.For[R, Fault]().Fail[A](faultOf("find an aggregate", repository.TableName(), ErrNoRows))
			}
			found, err := schema.FromDynamic(repository.stored, roots[0])
			if err != nil {
				return effect.For[R, Fault]().Fail[A](faultOf("find an aggregate", repository.TableName(), err))
			}
			return effect.For[R, Fault]().Succeed(found)
		})
}

// Delete removes the aggregate with that identity and everything beneath it,
// in one transaction; the outcome says whether one was kept.
func (repository Repository[A, ID]) Delete[R any](database Querier, spelling Spelling, identity ID) effect.Effect[R, Fault, Outcome] {
	laid, err := repository.layout(spelling)
	if err != nil {
		return effect.For[R, Fault]().Fail[Outcome](faultOf("delete an aggregate", repository.TableName(), err))
	}
	return inTransaction[R](database, func(within Querier) effect.Effect[R, Fault, Outcome] {
		return repository.kept[R](within, spelling, laid, repository.identityIs(identity)).
			FlatMap(func(existing [][]dynamic.Object) effect.Effect[R, Fault, Outcome] {
				gone := changes(spelling, laid, existing, make([][]dynamic.Object, len(laid)))
				return runAll[R](within, gone).As(Outcome{RowsAffected: int64(len(existing[0]))})
			})
	})
}

// kept is what is kept of the aggregate the criterion finds: its root row and
// every row beneath it.
func (repository Repository[A, ID]) kept[R any](database Querier, spelling Spelling, laid []TableLayout, where Criterion) effect.Effect[R, Fault, [][]dynamic.Object] {
	source := From(laid[0].Name, laid[0].Columns...)
	statement := SelectQuery{Select: source.Columns(), From: source, Where: where}.Statement(spelling)
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
// writes it.
func (repository Repository[A, ID]) identityIs(identity ID) Criterion {
	return Apply[bool](EqualTo,
		Term{node: node{kind: aColumn, source: repository.TableName(), name: repository.key}},
		boundAs(repository.identity.Shape(), identity))
}
