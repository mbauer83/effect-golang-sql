package evolve

// What can happen between two versions.
//
// A closed set of four, and the set is the point. A rename is a member of it,
// which is the whole reason this is declared rather than diffed: a diff sees a
// column gone and a column arrived and cannot tell a rename from a
// drop-and-add, so the most developed tool in the TypeScript ecosystem asks
// interactively and its programmatic path cannot do it at all. Declared, it is
// simply known.

import (
	"errors"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Change is one thing that happened. The set is sealed, so a projection
// switches over it and knows it has covered everything, and so a step cannot
// contain something nobody taught the DDL to write.
type Change interface {
	// apply is the version this change makes of the one before it.
	apply(before structure.Object) (structure.Object, error)
	// inverse is the change that undoes it, which needs the version before
	// because undoing a removal means knowing what was removed.
	inverse(before structure.Object) (Change, error)
	// describe names the change for a report that has to say which one
	// refused.
	describe() string
}

// Added is a field that was not there before.
//
// A field the caller must supply has to say where the values for the rows that
// already exist come from, so an Added field that is neither optional nor
// defaulted is refused: the alternative is a column a database will not add to
// a table that has rows in it.
type Added struct {
	Field structure.Field
}

// Removed is a field that is no longer there.
//
// Its inverse puts the column back and cannot put the values back, which is
// what makes a down migration best-effort rather than an undo.
type Removed struct {
	Name string
}

// Renamed is a field that is the same field under another name.
//
// The member of this set that a diff cannot see. Everything else about the
// field stays as it was, because a rename that also changed the type would be
// two changes and saying so is free.
type Renamed struct {
	From string
	To   string
}

// Retyped is a field whose shape changed.
//
// Whether the change is safe is the database's business and differs by dialect:
// widening an integer is nothing, narrowing one may not fit. This says what the
// new shape is and lets the projection say what it costs.
type Retyped struct {
	Name string
	Node structure.Node
}

func (Added) describe() string   { return "adding a field" }
func (Removed) describe() string { return "removing a field" }
func (Renamed) describe() string { return "renaming a field" }
func (Retyped) describe() string { return "changing a field's shape" }

var (
	errNoName       = errors.New("a change names the field it is about")
	errUnknownField = errors.New(
		"the version before this one has no such field, so the change is about " +
			"something that is not there")
	errAlreadyThere = errors.New(
		"the version before this one already has a field of that name -- which, for a " +
			"first step, usually means the first version was declared as the description " +
			"the program holds now: that description already carries the change, so " +
			"freeze the first version as the shape it was and let this step derive the " +
			"one the program holds")
	errUnsupplied = errors.New(
		"a field that is neither optional nor defaulted has no value for the rows that " +
			"already exist: make it optional, or give it a default")
	errNotAnObject   = errors.New("a version is a set of named fields, and this one has none")
	errNoVersionName = errors.New("a version has a name")
	errVersionTwice  = errors.New("this history already has a version of that name")
	errNoSuchVersion = errors.New("this history has no such version")
	errNoShape       = errors.New("a change of shape says what the new shape is")
	errNoChanges     = errors.New(
		"a version that is the same as the one before it is not a version")
	errNotAnObjectValue = errors.New(
		"a version's value is a set of named values, and this is something else")
	errDescriptionDiffers = errors.New(
		"this history's latest version and the description the program holds are not the " +
			"same shape: a description edited without a step beside it leaves the " +
			"migrator building one table and the program selecting another's columns")
)
