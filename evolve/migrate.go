package evolve

// Carrying a value from one version to another.
//
// The same declared changes that say what the description became say what the
// value becomes, which is the point of declaring them: a rename moves the
// member, an addition fills it from the default, a removal drops it. Nothing
// here is a function somebody wrote twice and has to keep in step with the
// shape, and nothing is inferred.

import (
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Migrate carries a value from one version to another, in either direction.
//
// The value is the universal representation, which is what lets this work
// without a Go type per version: cross a Go value into it with ToDynamic,
// migrate, and cross back with the target version's schema. Typed at the edges
// and untyped in the middle, exactly as the protobuf codec is -- and the reason
// the N-squared typed compositions the plan imagined turn out to be
// unnecessary here.
func (history History) Migrate(
	from string,
	to string,
	value dynamic.Value,
) (dynamic.Value, error) {
	changes, err := history.Between(from, to)
	if err != nil {
		return nil, err
	}
	object, isObject := value.(dynamic.Object)
	if !isObject {
		return nil, fmt.Errorf("%s: %w", history.name, errNotAnObjectValue)
	}

	for _, change := range changes {
		moved, err := carriedValues(change, object)
		if err != nil {
			return nil, fmt.Errorf("%s from %q to %q of %s: %w",
				change.describe(), from, to, history.name, err)
		}
		object = moved
	}
	return object, nil
}

// carriedValues is what one change does to a value.
func carriedValues(change Change, value dynamic.Object) (dynamic.Object, error) {
	switch shape := change.(type) {
	case Added:
		return addColumnSteps(shape, value)
	case Removed:
		return dropColumnSteps(shape.Name, value), nil
	case Renamed:
		return moving(shape, value), nil
	case Rewritten:
		return rewriteSteps(shape, value)
	case Retyped:
		// The shape changed and the value is left as it is. Converting it
		// would mean guessing how -- a number to a string is a format nobody
		// stated, a string to a number is a parse that may fail -- so what
		// comes out is checked against the target version's schema, which is
		// where a value that no longer fits is reported.
		return value, nil
	default:
		return dynamic.Object{}, fmt.Errorf("%T is not a change this can carry a value through", change)
	}
}

// addColumnSteps puts in what the new field holds for a value that predates it.
//
// A default is written; an optional field is left absent, which is what
// optional means. A computed field is left absent too: whatever computes it
// will, and a value invented here would be one nobody asked for.
func addColumnSteps(change Added, value dynamic.Object) (dynamic.Object, error) {
	if _, already := value.Member(change.Field.Name); already {
		// The value has a member the version it came from did not describe.
		// Tolerated on the way in, so tolerated here: it is dropped rather
		// than colliding, because the field being added is the one the target
		// version describes.
		value = dropColumnSteps(change.Field.Name, value)
	}
	switch fallback := change.Field.Default.(type) {
	case structure.DefaultTo:
		return with(value, change.Field.Name, fallback.Value), nil
	case structure.DefaultNow:
		// Not this layer's to invent: an instant put in here would be the
		// moment the migration ran rather than anything about the value, and
		// the database's own default is what fills the column. Absent, and
		// the field is one a caller does not supply anyway.
		return value, nil
	default:
		return value, nil
	}
}

func dropColumnSteps(name string, value dynamic.Object) dynamic.Object {
	after := dynamic.Object{Fields: make([]dynamic.Field, 0, len(value.Fields))}
	for _, field := range value.Fields {
		if field.Name != name {
			after.Fields = append(after.Fields, field)
		}
	}
	return after
}

// moving carries a member to its new name, in place.
//
// In place, so a rename does not reorder the members: the representation's
// objects are ordered, and a value that came out in a different order would
// encode differently for no reason anybody stated.
func moving(change Renamed, value dynamic.Object) dynamic.Object {
	after := dynamic.Object{Fields: make([]dynamic.Field, 0, len(value.Fields))}
	for _, field := range value.Fields {
		if field.Name == change.From {
			field.Name = change.To
		}
		after.Fields = append(after.Fields, field)
	}
	return after
}

func with(value dynamic.Object, name string, valueGiven dynamic.Value) dynamic.Object {
	after := dynamic.Object{Fields: make([]dynamic.Field, 0, len(value.Fields)+1)}
	after.Fields = append(after.Fields, value.Fields...)
	after.Fields = append(after.Fields, dynamic.Field{Name: name, Value: valueGiven})
	return after
}

// rewriteSteps adds, computes, then drops.
//
// In that order, because a computation needs both ends present: what it
// receives has the new members there -- absent or defaulted -- and the old ones
// still there to compute from, and what it returns has the old ones taken away.
func rewriteSteps(change Rewritten, value dynamic.Object) (dynamic.Object, error) {
	moved, err := stepsBetween(change, change.Adding, value)
	if err != nil {
		return dynamic.Object{}, err
	}
	if change.Forward.Value == nil {
		// Statements only: the database recomputes and a value in memory has
		// nothing to recompute from. Saying so beats returning a value that
		// looks migrated and is not.
		return dynamic.Object{}, fmt.Errorf("%s: %w", change.describe(), errNoValueRewrite)
	}
	written, err := change.Forward.Value(moved)
	if err != nil {
		return dynamic.Object{}, fmt.Errorf("%s: %w", change.describe(), err)
	}
	// The sources go last, so the computation above had both ends present.
	return stepsBetween(change, change.Dropping, written)
}

func stepsBetween(
	change Rewritten,
	list []Change,
	value dynamic.Object,
) (dynamic.Object, error) {
	moved := value
	for _, heldValue := range list {
		applied, err := carriedValues(heldValue, moved)
		if err != nil {
			return dynamic.Object{}, fmt.Errorf("%s: %w", change.describe(), err)
		}
		moved = applied
	}
	return moved, nil
}
