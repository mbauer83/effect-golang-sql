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
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/naming"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Mapping is a domain schema as a table stores it.
type Mapping[A any] struct {
	domain   schema.Schema[A]
	strategy naming.Strategy
	table    string
	// columns are the names given exactly, by the field's declared name.
	columns map[string]string
	// documents are the fields, by declared name, kept as one document
	// column rather than flattened.
	documents []string
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
	columns := make(map[string]string, len(mapping.columns)+1)
	for declared, given := range mapping.columns {
		columns[declared] = given
	}
	columns[field.Name()] = name
	mapping.columns = columns
	return mapping
}

// AsDocument keeps a value object in one document column -- jsonb on
// Postgres, text elsewhere -- rather than a column per member: for a value that
// is always read whole and never filtered by its parts.
func (mapping Mapping[A]) AsDocument(field schema.ObjectField[A]) Mapping[A] {
	mapping.documents = append(append([]string(nil), mapping.documents...), field.Name())
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
//
// A value object is stored as its members, each a column named after the field
// and the member -- artwork_poster -- unless the mapping keeps it as a
// document.
func (mapping Mapping[A]) Schema() schema.Schema[A] {
	stored := mapping.domain.Spelled(mapping.strategy)
	object, isObject := stored.Structure().(structure.Object)
	if !isObject {
		return stored.WithName(mapping.TableName())
	}
	flat := flattening{strategy: mapping.strategy, documents: map[string]bool{}}
	for _, declared := range mapping.documents {
		flat.documents[mapping.columnName(declared)] = true
	}
	columns, flattens := flat.node(object)
	if !flattens {
		return stored.WithName(mapping.TableName())
	}
	columns.Name = mapping.TableName()
	return schema.TransformOrFail(schema.Dynamic(columns),
		func(row dynamic.Value) (A, error) {
			held, _ := row.(dynamic.Object)
			return schema.FromDynamic(stored, flat.value(object, held))
		},
		func(value A) (dynamic.Value, error) {
			held, err := schema.ToDynamic(stored, value)
			if err != nil {
				return nil, err
			}
			whole, _ := held.(dynamic.Object)
			return flat.row(object, whole), nil
		})
}

// columnName is the column a declared field is stored in, before flattening.
func (mapping Mapping[A]) columnName(declared string) string {
	if given, named := mapping.columns[declared]; named {
		return given
	}
	return mapping.strategy.Spell(declared)
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
