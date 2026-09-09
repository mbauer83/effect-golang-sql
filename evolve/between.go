package evolve

// The changes between two versions, in either direction.

import (
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Between is the changes that take one version to another, in the order they
// have to be applied.
//
// Either direction: going back inverts each change and reverses the order,
// which is what makes N-1 declared steps answer every one of the N-squared
// pairs. Nothing is precomputed, because there is nothing to precompute --
// folding a handful of changes costs less than remembering the answer.
func (history History) Between(from string, to string) ([]Change, error) {
	start, err := history.positionOf(from)
	if err != nil {
		return nil, err
	}
	end, err := history.positionOf(to)
	if err != nil {
		return nil, err
	}
	switch {
	case start == end:
		return nil, nil
	case start < end:
		return history.forward(start, end), nil
	default:
		return history.backward(start, end)
	}
}

func (history History) forward(start int, end int) []Change {
	changes := []Change{}
	for at := start; at < end; at++ {
		changes = append(changes, history.steps[at]...)
	}
	return changes
}

// backward inverts each change and reverses the order twice over: the steps run
// in reverse, and the changes within each step do too.
//
// Both are necessary. A step that renamed a field and then added another has to
// drop the addition before undoing the rename, or the inverse would be looking
// for a field under a name it no longer has.
func (history History) backward(start int, end int) ([]Change, error) {
	changes := []Change{}
	for at := start; at > end; at-- {
		step := history.steps[at-1]
		// Within the step, each change's inverse needs the version as it was
		// just before that change was applied.
		states := make([]structure.Object, len(step))
		state := history.held[at-1]
		for index, change := range step {
			states[index] = state
			applied, err := change.apply(state)
			if err != nil {
				return nil, err
			}
			state = applied
		}
		for index := len(step) - 1; index >= 0; index-- {
			inverted, err := step[index].inverse(states[index])
			if err != nil {
				return nil, fmt.Errorf("undoing %s in %q of %s: %w",
					step[index].describe(), history.versions[at], history.name, err)
			}
			changes = append(changes, inverted)
		}
	}
	return changes, nil
}

// Stage is one change, with the description as it was just before that change
// was applied.
//
// A projection needs it. Whether a field is a relation, and what it was a
// relation to, is something only the version before the change knows -- a
// removal names a field and nothing else, and what has to be dropped depends
// on whether that field was a column or a table.
type Stage struct {
	Change Change
	Before structure.Node
}

// Stages is the changes that take one version to another, each with the
// description as it was just before it.
//
// The same walk Between does, keeping what it discards. Between is the shorter
// answer for a caller that only wants to know what happened.
func (history History) Stages(from string, to string) ([]Stage, error) {
	changes, err := history.Between(from, to)
	if err != nil {
		return nil, err
	}
	object, err := history.objectAt(from)
	if err != nil {
		return nil, err
	}

	stages := make([]Stage, 0, len(changes))
	for _, change := range changes {
		stages = append(stages, Stage{Change: change, Before: object})
		applied, err := change.apply(object)
		if err != nil {
			// Forward this cannot happen -- the history refused at assembly if
			// it could. Backward it can: an inverse is derived and a derived
			// change that will not apply is worth saying rather than skipping.
			return nil, fmt.Errorf("%s between %q and %q of %s: %w",
				change.describe(), from, to, history.name, err)
		}
		object = applied
	}
	return stages, nil
}
