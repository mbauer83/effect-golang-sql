package evolve

// Applying a change to a version, and inverting it.

import (
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

func (change Added) apply(before structure.Object) (structure.Object, error) {
	if change.Field.Name == "" {
		return structure.Object{}, errNoName
	}
	if _, fieldNamed := fieldNamed(before, change.Field.Name); fieldNamed {
		return structure.Object{}, fmt.Errorf("%q: %w", change.Field.Name, errAlreadyThere)
	}
	if _, relation := structure.EntityBehind(change.Field.Node); !relation {
		if !change.Field.Optional && change.Field.Default == nil && !change.Field.Computed {
			// The rows that already exist have no value for it, and a database
			// will not add such a column to a table that is not empty.
			//
			// A relation is exempt, and not as a favour: what a relation adds
			// is a table of its own, which starts empty, so no row that
			// already exists needs anything for it. Every parent simply has
			// none of the new thing.
			return structure.Object{}, fmt.Errorf("%q: %w", change.Field.Name, errUnsupplied)
		}
	}
	after := copied(before)
	after.Fields = append(after.Fields, change.Field)
	return after, nil
}

func (change Added) inverse(structure.Object) (Change, error) {
	return Removed{Name: change.Field.Name}, nil
}

func (change Removed) apply(before structure.Object) (structure.Object, error) {
	if change.Name == "" {
		return structure.Object{}, errNoName
	}
	if _, fieldNamed := fieldNamed(before, change.Name); !fieldNamed {
		return structure.Object{}, fmt.Errorf("%q: %w", change.Name, errUnknownField)
	}
	after := structure.Object{Name: before.Name, Doc: before.Doc}
	for _, field := range before.Fields {
		if field.Name != change.Name {
			after.Fields = append(after.Fields, field)
		}
	}
	return after, nil
}

// inverse puts the field back as it was, which it can only do because the
// version before the removal still describes it.
//
// The column comes back and the values do not. That is what makes a down
// migration best-effort, and it is why the restored field is made optional: a
// required column with no values is a column no row satisfies.
func (change Removed) inverse(before structure.Object) (Change, error) {
	field, fieldNamed := fieldNamed(before, change.Name)
	if !fieldNamed {
		return nil, fmt.Errorf("%q: %w", change.Name, errUnknownField)
	}
	if _, relation := structure.EntityBehind(field.Node); !relation {
		if field.Default == nil && !field.Computed {
			field.Optional = true
		}
	}
	return Added{Field: field}, nil
}

func (change Renamed) apply(before structure.Object) (structure.Object, error) {
	if change.From == "" || change.To == "" {
		return structure.Object{}, errNoName
	}
	if _, fieldNamed := fieldNamed(before, change.From); !fieldNamed {
		return structure.Object{}, fmt.Errorf("%q: %w", change.From, errUnknownField)
	}
	if _, taken := fieldNamed(before, change.To); taken {
		return structure.Object{}, fmt.Errorf("%q: %w", change.To, errAlreadyThere)
	}
	after := copied(before)
	for at, field := range after.Fields {
		if field.Name == change.From {
			// In place, so a rename does not reorder the fields -- which would
			// change the argument order of every statement built from this.
			after.Fields[at].Name = change.To
		}
	}
	return after, nil
}

// inverse is the same rename the other way, which is the one change in this set
// that loses nothing at all.
func (change Renamed) inverse(structure.Object) (Change, error) {
	return Renamed{From: change.To, To: change.From}, nil
}

func (change Retyped) apply(before structure.Object) (structure.Object, error) {
	if change.Name == "" {
		return structure.Object{}, errNoName
	}
	if change.Node == nil {
		return structure.Object{}, fmt.Errorf("%q: %w", change.Name, errNoShape)
	}
	if _, fieldNamed := fieldNamed(before, change.Name); !fieldNamed {
		return structure.Object{}, fmt.Errorf("%q: %w", change.Name, errUnknownField)
	}
	after := copied(before)
	for at, field := range after.Fields {
		if field.Name == change.Name {
			after.Fields[at].Node = change.Node
		}
	}
	return after, nil
}

func (change Retyped) inverse(before structure.Object) (Change, error) {
	field, fieldNamed := fieldNamed(before, change.Name)
	if !fieldNamed {
		return nil, fmt.Errorf("%q: %w", change.Name, errUnknownField)
	}
	return Retyped{Name: change.Name, Node: field.Node}, nil
}

// copied is the object with its own field slice, so applying a change does not
// reach back into the version before it.
//
// A description is shared -- two endpoints may hold the same one -- so a step
// that edited in place would change what the earlier version publishes.
func copied(before structure.Object) structure.Object {
	after := structure.Object{Name: before.Name, Doc: before.Doc}
	after.Fields = append(after.Fields, before.Fields...)
	return after
}

func fieldNamed(object structure.Object, name string) (structure.Field, bool) {
	for _, field := range object.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return structure.Field{}, false
}

// Describe names a change, for a report that has to say which one it was
// about.
//
// Public because a projection and a migrator both report on changes they were
// handed, and neither can name one otherwise.
func Describe(change Change) string {
	return change.describe()
}

// Applied is the description one change makes of another.
//
// Public because a migrator walking inside a rewriting needs it: the second of
// its structural changes is written against the shape the first made, and only
// this knows what that is. Everything else about a step reaches a projection
// through Stages.
func Applied(change Change, before structure.Object) (structure.Object, error) {
	return change.apply(before)
}
