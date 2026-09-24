package sql

// What a lazy collection does: a statement or two each, scoped to one owner.

import (
	"errors"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// Placement is where an element is put in an ordered collection: first, last,
// or beside another element.
type Placement[E any] struct {
	at      placing
	element E
}

type placing int

const (
	atEnd placing = iota
	atStart
	beforeOne
	afterOne
)

// First, Last, Before and After are the places an element can be put.
func (collection Collection[ID, E]) First() Placement[E] { return Placement[E]{at: atStart} }
func (collection Collection[ID, E]) Last() Placement[E]  { return Placement[E]{at: atEnd} }
func (collection Collection[ID, E]) Before(element E) Placement[E] {
	return Placement[E]{at: beforeOne, element: element}
}
func (collection Collection[ID, E]) After(element E) Placement[E] {
	return Placement[E]{at: afterOne, element: element}
}

// ErrNotInCollection is an element asked about that the owner's collection
// does not hold: a place beside it, or it moved.
var ErrNotInCollection = errors.New("sql: that element is not in this collection")

// Insert puts element in owner's collection, at the place given when a person
// orders it and at the end otherwise. An element already there is refused as
// ErrAlreadyThere, by the table's key rather than by a lookup first.
func (collection Collection[ID, E]) Insert[R any](database Querier, spelling Spelling, owner ID, element E, at Placement[E]) effect.Effect[R, Fault, effect.Unit] {
	row := collectionRow[ID, E]{Owner: owner, Element: element}
	if !collection.ordered {
		return collection.write[R](database, spelling, row)
	}
	return collection.keyFor[R](database, spelling, owner, at, nil).
		FlatMap(func(key string) effect.Effect[R, Fault, effect.Unit] {
			row.Position = key
			return collection.write[R](database, spelling, row)
		})
}

// Move puts an element of owner's collection at another place: one row
// written, whatever the collection's size.
func (collection Collection[ID, E]) Move[R any](database Querier, spelling Spelling, owner ID, element E, to Placement[E]) effect.Effect[R, Fault, effect.Unit] {
	return collection.keyFor[R](database, spelling, owner, to, &element).
		FlatMap(func(key string) effect.Effect[R, Fault, effect.Unit] {
			return collection.execute[R](database, UpdateQuery{
				Table: collection.source.table, Columns: []string{position},
				Values: []dynamic.Value{dynamic.OfText(key)},
				Where:  And(collection.ownerIs(owner), collection.elementIs(element)),
			}.Statement(spelling))
		})
}

// Remove takes element out of owner's collection.
func (collection Collection[ID, E]) Remove[R any](database Querier, spelling Spelling, owner ID, element E) effect.Effect[R, Fault, effect.Unit] {
	return collection.execute[R](database, DeleteQuery{
		Table: collection.source.table,
		Where: And(collection.ownerIs(owner), collection.elementIs(element)),
	}.Statement(spelling))
}

// Has reports whether owner's collection holds element.
func (collection Collection[ID, E]) Has[R any](database Querier, spelling Spelling, owner ID, element E) effect.Effect[R, Fault, bool] {
	return collection.Listing(owner).Count[R](database, spelling, collection.elementIs(element)).
		Map(func(count int64) bool { return count > 0 })
}

// Listing is owner's collection read a page at a time: in the order a person
// put it in, or in the elements' own order.
func (collection Collection[ID, E]) Listing(owner ID) Listing[E] {
	elements := Listing[E]{
		shape: elementRow(collection.element, collection.elementOf), source: collection.source,
		key: []string{collection.element}, defaultSize: 25, largestSize: 100,
		scope: collection.ownerIs(owner), fault: collection.fault,
	}
	if collection.ordered {
		return elements.Sort("placed", Ordering{term: node{kind: aColumn, name: position}})
	}
	return elements.Sort("value", Ordering{term: node{kind: aColumn, name: collection.element}})
}

// elementRow reads an element out of a row of the collection's table.
func elementRow[E any](column string, element schema.Schema[E]) schema.Schema[E] {
	return schema.Struct[E]("", schema.FieldAt(column, element, func(value *E) *E { return value }))
}

// write is one row inserted.
func (collection Collection[ID, E]) write[R any](database Querier, spelling Spelling, row collectionRow[ID, E]) effect.Effect[R, Fault, effect.Unit] {
	stored := collection.stored.Schema()
	values, err := Arguments(stored, row)
	if err != nil {
		return effect.For[R, Fault]().Fail[effect.Unit](faultOf("insert into a collection", "", err))
	}
	return collection.execute[R](database, InsertQuery{
		Table: collection.source.table, Columns: Columns(stored), Values: values,
	}.Statement(spelling))
}

func (collection Collection[ID, E]) execute[R any](database Querier, statement Statement) effect.Effect[R, Fault, effect.Unit] {
	if collection.fault != nil {
		return effect.For[R, Fault]().Fail[effect.Unit](faultOf("change a collection", "", collection.fault))
	}
	return Run[R](database, statement).As(effect.Unit{})
}
