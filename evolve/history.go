package evolve

// An aggregate's versions, and the steps between them.
//
// Version one is declared and every later one is derived, which is the whole
// reason a step and a declaration cannot disagree about what the declaration
// became: there is nothing to disagree with.

import (
	"fmt"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Named is an aggregate whose history has not started yet.
//
// It exists so that the aggregate's name and its first version's name are not
// two adjacent strings in one call, which is the kind of signature a caller
// gets the wrong way round exactly once.
type Named struct {
	name string
}

// Of names the aggregate a history is of.
//
// A fully-qualified name rather than a Go package path or a struct name,
// because that is what makes a declaration a contract between services rather
// than a detail of one program -- the same reason a protobuf service carries
// its own.
func Of(name string) Named {
	return Named{name: name}
}

// History is what an aggregate has been.
type History struct {
	name     string
	versions []string
	held     []structure.Object
	steps    [][]Change
	fault    error
}

// Starting declares the first version.
//
// The version is named and not numbered. A position would renumber every later
// version whenever one was inserted, and it would give a document tagged
// "2.1.0" nothing to match against but a convention -- where a name is what the
// document, the service that wrote it and the service that reads it already
// agree on. Which name comes first is the declaration's business and no scheme
// is imposed: "1.0.0" and "logistics.Pallet.v2" are both names, and the order
// is the order they are declared in.
func (named Named) Starting(version string, node structure.Node) History {
	object, isObject := node.(structure.Object)
	switch {
	case strings.TrimSpace(named.name) == "":
		return History{fault: errNoName}
	case strings.TrimSpace(version) == "":
		return History{name: named.name, fault: errNoVersionName}
	case !isObject:
		return History{name: named.name, fault: errNotAnObject}
	}
	return History{
		name:     named.name,
		versions: []string{version},
		held:     []structure.Object{object},
	}
}

// Then declares the next version as a list of changes to the one before it.
//
// The version it describes is derived, so the order is the order these are
// declared in and is monotonic by construction. A step with no changes is
// refused: a version that is the same as the one before it is not a version,
// and naming one would make every consumer look for a difference there is none
// of.
func (history History) Then(version string, changes ...Change) History {
	if history.fault != nil {
		return history
	}
	switch {
	case strings.TrimSpace(version) == "":
		history.fault = errNoVersionName
		return history
	case history.knows(version):
		history.fault = fmt.Errorf("%q of %s: %w", version, history.name, errVersionTwice)
		return history
	case len(changes) == 0:
		history.fault = fmt.Errorf("%q of %s: %w", version, history.name, errNoChanges)
		return history
	}

	after := history.held[len(history.held)-1]
	for _, change := range changes {
		applied, err := change.applied(after)
		if err != nil {
			history.fault = fmt.Errorf("%s in %q of %s: %w",
				change.describe(), version, history.name, err)
			return history
		}
		after = applied
	}

	history.versions = append(history.versions, version)
	history.held = append(history.held, after)
	history.steps = append(history.steps, changes)
	return history
}

// Fault is why the history cannot be used, if it cannot.
//
// A declaration mistake is reported here rather than when something tries to
// migrate, which is the same rule the routing tree and an endpoint follow. Go
// cannot check a step's totality at compile time -- a version is a value and
// not a type -- so this is where it is checked, and a test that asks is a test
// that has checked it.
func (history History) Fault() error {
	return history.fault
}

// Name is the fully-qualified name this history is of.
func (history History) Name() string { return history.name }

// Versions are the version names, in the order they were declared.
func (history History) Versions() []string {
	return append([]string(nil), history.versions...)
}

// Latest is the most recently declared version's name.
func (history History) Latest() string {
	if len(history.versions) == 0 {
		return ""
	}
	return history.versions[len(history.versions)-1]
}

// At is the description as of one version.
func (history History) At(version string) (structure.Node, error) {
	object, err := history.objectAt(version)
	if err != nil {
		return nil, err
	}
	return object, nil
}

func (history History) knows(version string) bool {
	for _, held := range history.versions {
		if held == version {
			return true
		}
	}
	return false
}

// positionOf is where a version sits in the declared order.
func (history History) positionOf(version string) (int, error) {
	if history.fault != nil {
		return 0, history.fault
	}
	for at, held := range history.versions {
		if held == version {
			return at, nil
		}
	}
	return 0, fmt.Errorf("%s has %s, and not %q: %w",
		history.name, strings.Join(history.versions, ", "), version, errNoSuchVersion)
}

func (history History) objectAt(version string) (structure.Object, error) {
	at, err := history.positionOf(version)
	if err != nil {
		return structure.Object{}, err
	}
	return history.held[at], nil
}
