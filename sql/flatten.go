package sql

// Value objects as a table stores them: each member a column of its own, named
// after the field that holds it.
//
// A value object with no identity -- an artwork, an address -- is part of the
// row that holds it. Stored as its members, each part can be filtered and
// indexed and needs no JSON functions to read, and a value object whose parts
// are all null is told apart from one whose parts are empty. A field the
// mapping keeps as a document is left whole.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/naming"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// valueObjectBehind is the value object a field holds, and whether it holds one
// that may be absent.
func valueObjectBehind(node structure.Node) (structure.Object, bool, bool) {
	nullable := false
	for {
		switch shape := node.(type) {
		case structure.Nullable:
			node, nullable = shape.Inner, true
		case structure.Reference:
			if shape.Resolve == nil {
				return structure.Object{}, false, false
			}
			node = shape.Resolve()
		case structure.Object:
			return shape, !shape.IsEntity() && len(shape.Fields) > 0, nullable
		default:
			return structure.Object{}, false, false
		}
	}
}

// flattening flattens one object's value objects, spelling each member's column
// as the field and the member, by the mapping's strategy.
type flattening struct {
	strategy naming.Strategy
	// documents are the fields, by their spelled names, kept whole.
	documents map[string]bool
}

func (flat flattening) joined(outer string, inner string) string {
	return flat.strategy.Spell(outer + "_" + inner)
}

func (flat flattening) flattens(field structure.Field) (structure.Object, bool, bool) {
	if flat.documents[field.Name] {
		return structure.Object{}, false, false
	}
	return valueObjectBehind(field.Node)
}

// node is the object's description with its value objects' members in place of
// them. A member of a value object that may be absent may be absent too.
func (flat flattening) node(object structure.Object) (structure.Object, bool) {
	fields := make([]structure.Field, 0, len(object.Fields))
	changed := false
	for _, field := range object.Fields {
		inner, flattens, nullable := flat.flattens(field)
		if !flattens {
			fields = append(fields, field)
			continue
		}
		changed = true
		innerFlat, _ := flattening{strategy: flat.strategy}.node(inner)
		for _, member := range innerFlat.Fields {
			member.Name = flat.joined(field.Name, member.Name)
			member.Exact = false
			member.Identity = false
			member.Optional = member.Optional || field.Optional || nullable
			fields = append(fields, member)
		}
	}
	object.Fields = fields
	return object, changed
}

// row is a value's members as the table stores them.
func (flat flattening) row(object structure.Object, value dynamic.Object) dynamic.Object {
	out := dynamic.Object{Fields: make([]dynamic.Field, 0, len(value.Fields))}
	for _, field := range object.Fields {
		member, present := value.Member(field.Name)
		inner, flattens, _ := flat.flattens(field)
		if !flattens {
			if present {
				out.Fields = append(out.Fields, dynamic.Field{Name: field.Name, Value: member})
			}
			continue
		}
		nested, isObject := member.(dynamic.Object)
		if !present || !isObject {
			continue
		}
		for _, part := range (flattening{strategy: flat.strategy}).row(inner, nested).Fields {
			out.Fields = append(out.Fields, dynamic.Field{Name: flat.joined(field.Name, part.Name), Value: part.Value})
		}
	}
	return out
}

// value is a row's columns as the value they store: each value object gathered
// from its members, and absent when every member is.
func (flat flattening) value(object structure.Object, row dynamic.Object) dynamic.Object {
	return flat.gather(object, func(name string) (dynamic.Value, bool) { return row.Member(name) })
}

func (flat flattening) gather(object structure.Object, column func(string) (dynamic.Value, bool)) dynamic.Object {
	out := dynamic.Object{Fields: make([]dynamic.Field, 0, len(object.Fields))}
	for _, field := range object.Fields {
		inner, flattens, nullable := flat.flattens(field)
		if !flattens {
			if member, present := column(field.Name); present {
				out.Fields = append(out.Fields, dynamic.Field{Name: field.Name, Value: member})
			}
			continue
		}
		outer := field.Name
		nested := flattening{strategy: flat.strategy}.gather(inner, func(name string) (dynamic.Value, bool) {
			return column(flat.joined(outer, name))
		})
		if allAbsent(nested) && (field.Optional || nullable) {
			if nullable {
				out.Fields = append(out.Fields, dynamic.Field{Name: field.Name, Value: dynamic.Absent{}})
			}
			continue
		}
		out.Fields = append(out.Fields, dynamic.Field{Name: field.Name, Value: nested})
	}
	return out
}

func allAbsent(object dynamic.Object) bool {
	for _, field := range object.Fields {
		if _, absent := field.Value.(dynamic.Absent); !absent {
			if nested, isObject := field.Value.(dynamic.Object); !isObject || !allAbsent(nested) {
				return false
			}
		}
	}
	return true
}
