package evolve

// The change the closed set cannot express.
//
// Four changes derive their own value migration, because a rename moves a
// member and an addition fills one and a removal drops one, and none of that
// needs a function. What they cannot do is compute: a field split into two, two
// merged into one, a count that was text becoming a number, metres becoming
// millimetres. Those need somebody to say how, in both directions, and there is
// no deriving it.
//
// So this carries the how. It is one change rather than an option on the
// others, because a change that recomputes values is a different kind of thing
// from one that moves them -- and because the four staying derivable is what
// keeps the ordinary case free of functions nobody had to write.

import (
	"context"
	"errors"
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// Recomputation is a change that recomputes values.
//
// The structural part is said in the ordinary four -- a split is two additions
// and a removal -- and it is said in **two lists**, because a computation needs
// both ends present while it runs. The targets have to exist before the values
// move, and the sources cannot go until after: a split that dropped the old
// column first would be computing from a column that is not there, which is a
// mistake this shape makes unmakeable rather than one an author has to remember
// not to make.
//
// Going back reverses the two lists as well as the two directions, so the same
// declaration reads correctly in both: the source is put back, the values move,
// and the targets go.
type Recomputation struct {
	// Name names it, for a report that has to say which change refused.
	Name string
	// Additions is what has to exist before the values move.
	Additions []Change
	// Removals is what goes once they have moved.
	Removals []Change
	// Forward is how a value and a table move to the later version.
	Forward Rewrite
	// Back is how they move to the earlier one. It may be empty, which says
	// the change cannot be undone -- and a migration that would need to says
	// so rather than doing half of it.
	Back Rewrite
}

// changes is the changes in the order they apply: what arrives, then what
// goes.
func (change Recomputation) changes() []Change {
	changes := make([]Change, 0, len(change.Additions)+len(change.Removals))
	changes = append(changes, change.Additions...)
	return append(changes, change.Removals...)
}

// Rewrite is one direction of a recomputation: what happens to a value in
// memory, and what happens to the rows a database already holds.
type Rewrite struct {
	// Value carries one value across. It runs after the fields it needs exist
	// and before the ones it read are dropped, so what it receives has both
	// ends present, and its job is to fill in what only a computation knows.
	Value func(dynamic.Object) (dynamic.Object, error)
	// Rows moves the rows a database already holds.
	//
	// A function and not a list of statements, and that is the difference that
	// matters. It gets the transaction the migration is running in, so it can
	// read what is there, compute in Go, write back, and call out to something
	// else if that is what the change needs -- a lookup service, a checksum, a
	// unit conversion nobody can express in SQL. A statement list could only
	// ever say what one dialect can say in one statement, and would have to
	// say it once per dialect.
	//
	// It runs inside the migration's transaction, so what it writes is
	// committed or rolled back with everything else -- where the database
	// allows that.
	//
	// It is given the dialect as well as the transaction, and that is not
	// convenience: without it a mover writing a statement has to spell its own
	// placeholders, which means picking a server. This port used to hand over
	// the transaction alone, and every mover written against it -- including
	// the one in this module's own examples -- carried a question mark and
	// would have been refused by Postgres.
	Rows func(ctx context.Context, within sql.Querier, spelling sql.Spelling) error
}

// IsEmpty reports whether this direction says anything at all.
func (rewrite Rewrite) IsEmpty() bool {
	return rewrite.Value == nil && rewrite.Rows == nil
}

func (change Recomputation) describe() string {
	if change.Name == "" {
		return "recomputation"
	}
	return change.Name
}

// apply is the structural part, in order: what arrives, then what goes.
func (change Recomputation) apply(before structure.Object) (structure.Object, error) {
	changes := change.changes()
	if len(changes) == 0 {
		return structure.Object{}, fmt.Errorf("%s: %w", change.describe(), errNothingStructural)
	}
	if change.Forward.IsEmpty() {
		// A recomputation that rewrites nothing is the four changes with extra
		// words around them, and saying so beats letting it look like more.
		return structure.Object{}, fmt.Errorf("%s: %w", change.describe(), errNothingToRewrite)
	}
	after := before
	for _, inner := range changes {
		next, err := inner.apply(after)
		if err != nil {
			return structure.Object{}, fmt.Errorf("%s: %w", change.describe(), err)
		}
		after = next
	}
	return after, nil
}

// inverse is the structural inverses reversed, with the two directions swapped.
//
// It refuses when Back says nothing. A recomputation that cannot be undone is
// the ordinary case rather than an oversight -- averaging two columns into one
// loses which was which -- and a migration that pretended otherwise would put a
// database into a state its own description does not describe.
func (change Recomputation) inverse(before structure.Object) (Change, error) {
	if change.Back.IsEmpty() {
		return nil, fmt.Errorf("%s: %w", change.describe(), errNoWayBack)
	}

	// The inverse of what went becomes what arrives, and the inverse of what
	// arrived becomes what goes -- which is what puts the source back before
	// the values move and takes the targets away after.
	additions, state, err := invertRewrite(change, change.Removals, before, len(change.Additions))
	if err != nil {
		return nil, err
	}
	removals, _, err := invertRewrite(change, change.Additions, before, 0)
	if err != nil {
		return nil, err
	}
	_ = state

	return Recomputation{
		Name:      "undoing " + change.describe(),
		Additions: additions,
		Removals:  removals,
		Forward:   change.Back,
		Back:      change.Forward,
	}, nil
}

// invertRewrite is one list's changes, invertRewrite and reversed.
//
// skip is how many of the whole step's changes come before this list, because
// each inverse needs the description as it was just before its own change was
// applied and that means walking from the start.
func invertRewrite(
	change Recomputation,
	list []Change,
	before structure.Object,
	skip int,
) ([]Change, structure.Object, error) {
	changes := change.changes()
	states := make([]structure.Object, len(changes))
	state := before
	for index, inner := range changes {
		states[index] = state
		next, err := inner.apply(state)
		if err != nil {
			return nil, structure.Object{}, err
		}
		state = next
	}

	inverses := make([]Change, 0, len(list))
	for index := len(list) - 1; index >= 0; index-- {
		inverse, err := list[index].inverse(states[skip+index])
		if err != nil {
			return nil, structure.Object{}, fmt.Errorf("undoing %s: %w", change.describe(), err)
		}
		inverses = append(inverses, inverse)
	}
	return inverses, state, nil
}

var (
	errNothingStructural = errors.New(
		"a rewriting says what happens to the description as well as to the values, " +
			"in the ordinary four changes")
	errNothingToRewrite = errors.New(
		"a rewriting that rewrites nothing is the ordinary changes with extra words " +
			"around them")
	errNoValueRewrite = errors.New(
		"this change rewrites the database and says nothing about a value in memory, " +
			"so there is nothing to carry one across with")
	errNoWayBack = errors.New(
		"this change says nothing about going back, and a rewriting is not reversible " +
			"by itself: averaging two columns into one loses which was which")
)
