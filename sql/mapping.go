package sql

// A domain type as a table stores it.
//
// A domain schema says what a value is; a mapping says only how its table
// differs from that. By default it differs in nothing but spelling: the
// table and its columns are the domain's names in snake_case. What a mapping
// states beyond that -- a table's own name, a column's, how a value is stored --
// is stated field by field, through the domain's own field handles, so a field
// renamed in the domain is a compile error here rather than a column that
// silently stops matching.
//
// A mapping adds no rules about values. What a column may hold is what the
// domain says a value may be, so every value the domain can make can be stored.

import (
	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/naming"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Mapping is a domain schema as a table stores it.
type Mapping[A any] struct {
	domain   schema.Schema[A]
	strategy naming.Strategy
	table    string
}

// Map is the domain schema as a table stores it, with the default for
// everything: its names in snake_case.
func Map[A any](domain schema.Schema[A]) Mapping[A] {
	return Mapping[A]{domain: domain, strategy: naming.SnakeCase}
}

// Table gives the table its name, exactly as written.
func (mapping Mapping[A]) Table(name string) Mapping[A] {
	mapping.table = name
	return mapping
}

// Column gives one field's column its name, exactly as written.
func (mapping Mapping[A]) Column(field schema.ObjectField[A], name string) Mapping[A] {
	mapping.domain = mapping.domain.Rename(field, name)
	return mapping
}

// Naming spells the names the mapping does not give exactly by strategy, for a
// database whose tables follow another convention.
func (mapping Mapping[A]) Naming(strategy naming.Strategy) Mapping[A] {
	mapping.strategy = strategy
	return mapping
}

// Represent stores field as a C: to converts its value on the way into the
// table, from converts it back on the way out.
func (mapping Mapping[A]) Represent[B, C any](
	field schema.Field[A, B],
	shape schema.Schema[C],
	to func(B) C,
	from func(C) B,
) Mapping[A] {
	mapping.domain = mapping.domain.Represent(field, shape, to, from)
	return mapping
}

// Schema is what rows are read and written through, and what the table is
// made from: the domain's schema, spelled as the table spells it, under the
// table's name.
func (mapping Mapping[A]) Schema() schema.Schema[A] {
	stored := mapping.domain.Spelled(mapping.strategy)
	return stored.WithName(mapping.TableName())
}

// TableName is the table's name: the one given, or the domain object's name
// spelled by the mapping's strategy.
func (mapping Mapping[A]) TableName() string {
	if mapping.table != "" {
		return mapping.table
	}
	if object, isObject := mapping.domain.Structure().(structure.Object); isObject {
		return mapping.strategy.Spell(object.Name)
	}
	return ""
}
