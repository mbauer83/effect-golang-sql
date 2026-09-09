package sql

// Naming a statement's columns and binding its arguments from one schema.
//
// A statement's text is the caller's, because SQL is a language and this is not
// the place to invent a second one. What this offers is the two halves that
// have to agree -- which columns, in which order -- from the same description,
// so they cannot drift apart in the way a hand-written list and a hand-written
// argument list eventually do.

import (
	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Columns are the names a schema's members have, in declared order.
//
// Use them to write the statement, so that the column list and the arguments
// come from one place:
//
//	names := sql.Columns(BookSchema)
//	values, err := sql.Arguments(BookSchema, book)
func Columns[A any](shape schema.Schema[A]) []string {
	object, isObject := shape.Structure().(structure.Object)
	if !isObject {
		return nil
	}
	names := make([]string, 0, len(object.Fields))
	for _, member := range object.Fields {
		names = append(names, member.Name)
	}
	return names
}

// Arguments are a value's members, in the same order Columns gives their names.
//
// An absent optional member binds as null, because a column that is not being
// given a value is what null is for and leaving it out would change which
// column each argument answered to.
func Arguments[A any](shape schema.Schema[A], value A) ([]dynamic.Value, error) {
	crossed, err := schema.ToDynamic(shape, value)
	if err != nil {
		return nil, faultOf("binding arguments", "", err)
	}
	object, isObject := crossed.(dynamic.Object)
	if !isObject {
		return nil, faultOf("binding arguments", "", errNotAnObject)
	}

	bound := make([]dynamic.Value, 0, len(Columns(shape)))
	for _, name := range Columns(shape) {
		member, present := object.Member(name)
		if !present {
			member = dynamic.Absent{}
		}
		bound = append(bound, member)
	}
	return bound, nil
}
