package sql

// A lazy collection as a table stores it: one row per element, beside the
// owner's key, read and changed a statement at a time.

import (
	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Collection is a lazy collection as a table stores it: one row per element,
// keyed by the owner and the element, with the element's place when a person
// orders it.
//
// Every statement it makes is scoped to one owner, whose key leads the table's
// key: one person's list is not reachable through another's, even by an
// element they share.
type Collection[ID, E any] struct {
	stored    Mapping[collectionRow[ID, E]]
	source    Source
	ownerOf   schema.Schema[ID]
	elementOf schema.Schema[E]
	owner     string
	element   string
	ordered   bool
	fault     error
}

// collectionRow is one element of one owner's collection.
type collectionRow[ID, E any] struct {
	Owner    ID
	Element  E
	Position string
}

// position is the column an ordered collection keeps each element's place in.
const position = "position"

// CollectionOf is a lazy collection of an aggregate owner maps, stored in a
// table of its own: owner_collection, with the owner's key and the element. Its
// owner is referred to with a cascading foreign key -- a collection goes with
// its owner -- and an element that is a reference with a key to where targets
// says its target is stored.
func CollectionOf[O, ID, E any](owner Mapping[O], declared schema.Collection[O, ID, E], targets ...TableMapping) Collection[ID, E] {
	strategy := owner.strategy
	ownerObject := owner.Describes()
	identity := declared.Identity()
	collection := Collection[ID, E]{
		ownerOf:   identity.Shape(),
		elementOf: declared.Element(),
		owner:     strategy.Spell(ownerObject + "_" + identity.Name()),
		element:   elementColumn(declared.Element().Structure(), strategy.Spell),
		ordered:   declared.IsOrdered(),
		fault:     declared.Err(),
	}
	table := strategy.Spell(ownerObject + "_" + declared.Name())
	fields := []schema.ObjectField[collectionRow[ID, E]]{
		schema.FieldAt(collection.owner, schema.Ref(declared.Owner(), identity, schema.Cascade),
			func(row *collectionRow[ID, E]) *ID { return &row.Owner }).Identity(),
		schema.FieldAt(collection.element, declared.Element(),
			func(row *collectionRow[ID, E]) *E { return &row.Element }).Identity(),
	}
	columns := []ColumnType{
		ColumnOf(collection.owner, KindOf(identity.Shape().Structure())),
		ColumnOf(collection.element, KindOf(declared.Element().Structure())),
	}
	if collection.ordered {
		fields = append(fields, schema.FieldAt(position, schema.Text().Check(schema.MinLength(1)),
			func(row *collectionRow[ID, E]) *string { return &row.Position }))
		columns = append(columns, ColumnOf(position, OfText))
	}
	collection.stored = Map(schema.Struct[collectionRow[ID, E]](table, fields...)).
		Naming(strategy).
		Referring(append(append([]TableMapping(nil), targets...), owner)...)
	collection.source = From(table, columns...)
	return collection
}

// elementColumn is what an element's column is called: after the aggregate it
// identifies -- film_id -- or value for a plain value.
func elementColumn(element structure.Node, spell func(string) string) string {
	if scalar, isScalar := element.(structure.Scalar); isScalar && scalar.Refers != nil {
		return spell(scalar.Refers.Object + "_" + scalar.Refers.Key)
	}
	return spell("value")
}

// Schema is the collection's table as rows are written through it, and as the
// table is made from.
func (collection Collection[ID, E]) Schema() schema.Schema[collectionRow[ID, E]] {
	return collection.stored.Schema()
}

// Structure is the collection's table as DDL derives it.
func (collection Collection[ID, E]) Structure() structure.Node {
	return collection.stored.Schema().Structure()
}

// Source is the collection's table, as a query reads it.
func (collection Collection[ID, E]) Source() Source { return collection.source }

// ownerIs is the rows of one owner's collection.
func (collection Collection[ID, E]) ownerIs(owner ID) Criterion {
	return collection.equals(collection.owner, collection.ownerOf, owner)
}

func (collection Collection[ID, E]) elementIs(element E) Criterion {
	return collection.equals(collection.element, collection.elementOf, element)
}

// equals compares a column with a value bound as its schema writes it, so an
// identity kept as a named type binds what the table holds.
func (collection Collection[ID, E]) equals[V any](column string, shape schema.Schema[V], value V) Criterion {
	return Apply[bool](EqualTo, Term{node: node{kind: aColumn, source: collection.source.table, name: column}},
		boundAs(shape, value))
}

// boundAs is a value bound as its schema writes it.
func boundAs[V any](shape schema.Schema[V], value V) Term {
	written, err := schema.ToDynamic(shape, value)
	if err != nil {
		return Refuse[bool](err).Term()
	}
	return Term{node: node{kind: aValue, value: written}}
}
