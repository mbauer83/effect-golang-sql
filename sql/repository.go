package sql

// An aggregate kept in the table its mapping describes: saved, found and
// deleted by its identity, and listed.
//
// Nothing here names a column. The columns are the mapping's, the values are
// the domain schema's encoding of the aggregate, and the key is the column the
// identity field is stored in -- so a field renamed or remapped changes every
// statement together.

import (
	"errors"
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang/effect"
)

// Repository is where aggregates of one kind are kept: one row each, in the
// table their mapping describes, found by their identity.
//
// An aggregate whose entities are stored in tables of their own -- a list of
// entities in the domain -- is refused: its rows are not one statement's, and
// a repository that wrote only the root would lose them.
type Repository[A, ID any] struct {
	mapping  Mapping[A]
	stored   schema.Schema[A]
	identity schema.Field[A, ID]
	source   Source
	columns  []string
	key      string
	fault    error
}

// NewRepository is where aggregates the mapping describes are kept, found by
// the identity field.
func NewRepository[A, ID any](mapping Mapping[A], identity schema.Field[A, ID]) Repository[A, ID] {
	stored := mapping.Schema()
	repository := Repository[A, ID]{
		mapping: mapping, stored: stored, identity: identity,
		key: mapping.columnName(identity.Name()),
	}
	object, isObject := stored.Structure().(structure.Object)
	if !isObject {
		repository.fault = errors.New("sql: a repository keeps an object")
		return repository
	}
	kinds := make([]ColumnType, 0, len(object.Fields))
	for _, field := range object.Fields {
		if _, related := structure.EntityBehind(field.Node); related {
			repository.fault = fmt.Errorf("sql: %q holds entities of their own table, which a repository of one row each cannot keep", field.Name)
		}
		repository.columns = append(repository.columns, field.Name)
		kinds = append(kinds, ColumnOf(field.Name, KindOf(field.Node)))
	}
	repository.source = From(mapping.TableName(), kinds...)
	return repository
}

// Structure is the repository's table as DDL makes it.
func (repository Repository[A, ID]) Structure() structure.Node { return repository.stored.Structure() }

// TableName is the repository's table.
func (repository Repository[A, ID]) TableName() string { return repository.mapping.TableName() }

// Source is the repository's table, as a query reads it.
func (repository Repository[A, ID]) Source() Source { return repository.source }

// Of is a field's column in the repository's table, for a filter or a sort.
func (repository Repository[A, ID]) Of[B any](field schema.Field[A, B]) Expr[B] {
	return repository.mapping.Of(repository.source, field)
}

// Listing is the repository's aggregates read a page at a time, keyed by their
// identity; the sorts, page sizes and searches it offers are the caller's to
// declare.
func (repository Repository[A, ID]) Listing() Listing[A] {
	listing := NewListing(repository.stored, repository.source, repository.key)
	if repository.fault != nil {
		listing.fault = repository.fault
	}
	return listing
}

// Save writes the aggregate: a new one is inserted and a kept one replaced,
// in one statement, since whether it is new is not worth a round trip.
func (repository Repository[A, ID]) Save[R any](database Querier, spelling Spelling, value A) effect.Effect[R, Fault, Outcome] {
	row, err := repository.row(value)
	if err != nil {
		return effect.For[R, Fault]().Fail[Outcome](faultOf("save an aggregate", repository.mapping.TableName(), err))
	}
	return Run[R](database, UpsertQuery{
		Table: repository.mapping.TableName(), Columns: repository.columns,
		Key: []string{repository.key}, Values: row,
	}.Statement(spelling))
}

// Find is the aggregate with that identity, or a fault that is ErrNoRows when
// none is kept.
func (repository Repository[A, ID]) Find[R any](database Querier, spelling Spelling, identity ID) effect.Effect[R, Fault, A] {
	if repository.fault != nil {
		return effect.For[R, Fault]().Fail[A](faultOf("find an aggregate", repository.mapping.TableName(), repository.fault))
	}
	return Row[R](database, repository.stored, SelectQuery{
		Select: repository.source.Columns(), From: repository.source, Where: repository.identityIs(identity),
	}.Statement(spelling))
}

// Delete removes the aggregate with that identity; the outcome says whether
// one was kept.
func (repository Repository[A, ID]) Delete[R any](database Querier, spelling Spelling, identity ID) effect.Effect[R, Fault, Outcome] {
	if repository.fault != nil {
		return effect.For[R, Fault]().Fail[Outcome](faultOf("delete an aggregate", repository.mapping.TableName(), repository.fault))
	}
	return Run[R](database, DeleteQuery{
		Table: repository.mapping.TableName(), Where: repository.identityIs(identity),
	}.Statement(spelling))
}

// identityIs is the row of that identity, bound as the identity's schema
// writes it.
func (repository Repository[A, ID]) identityIs(identity ID) Criterion {
	return Apply[bool](EqualTo,
		Term{node: node{kind: aColumn, source: repository.source.table, name: repository.key}},
		boundAs(repository.identity.Shape(), identity))
}

// row is the aggregate's columns, in the order the statements name them; a
// member the value leaves out is bound as null.
func (repository Repository[A, ID]) row(value A) ([]dynamic.Value, error) {
	if repository.fault != nil {
		return nil, repository.fault
	}
	encoded, err := schema.ToDynamic(repository.stored, value)
	if err != nil {
		return nil, err
	}
	object, isObject := encoded.(dynamic.Object)
	if !isObject {
		return nil, errors.New("sql: an aggregate is written as an object")
	}
	values := make([]dynamic.Value, 0, len(repository.columns))
	for _, column := range repository.columns {
		member, present := object.Member(column)
		if !present {
			member = dynamic.Absent{}
		}
		values = append(values, member)
	}
	return values, nil
}
