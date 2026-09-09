package sql

// A statement and the values it binds, kept together and spelled by the
// dialect.
//
// Two things go wrong when a caller writes a statement as text and hands the
// values alongside it. The spelling of a bound value is not the same in every
// dialect -- Postgres numbers them and the others do not -- so a statement
// composed with one spelling is refused by a server that wants the other, and
// by nothing before it. And the text and the values are two lists that have to
// agree in order and in length, which they do until somebody inserts a
// condition in the middle.
//
// So a statement is described here as the pieces it is made of, with the
// values in place among them, and rendered once by the dialect that will run
// it. A caller never writes a placeholder, and the values come out in the
// order the statement reads them because they were never in a separate list.
//
// This is deliberately the lower of the two ways to say a statement in this
// package. The shapes in statements.go say what a repository does -- read this
// row, write it, replace it -- and are what a store should reach for. This is
// for the statements an application writes because their shape is its own: a
// read model's expression, an aggregate, a join.

import (
	"errors"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// Placeholders is how a dialect writes the nth value a statement binds.
//
// Declared here rather than taken from ddl, and the reason is the dependency's
// direction: ddl reaches this package through evolve, so it cannot be reached
// from here. What that forces is what should have been written anyway -- this
// package says the one thing it needs of a dialect, and ddl's dialects satisfy
// it without being asked to.
type Placeholders interface {
	Placeholder(ordinal int) string
}

// Spelling is the whole of what a statement needs from a dialect.
//
// Five answers, and each is something only the server's own syntax decides:
// what to call it in a refusal, how it writes the nth bound value, how it
// writes an identifier, how it writes a literal no server would bind, how it
// says "write this row, replacing what is there", and which operations it can
// perform and how.
//
// Narrow on purpose. Everything else about a statement -- which rows, which
// order, how a page is cut, where a bracket goes -- is the same on every
// server and is decided above this line.
type Spelling interface {
	Placeholders
	Operations
	// Name is what to call this dialect in a refusal that has to say which one
	// could not do something.
	Name() string
	// Quoted is an identifier as this dialect writes it.
	Quoted(name string) string
	// Text is a string literal in this dialect's own quoting, for the pieces
	// no server will bind: a separator, a format, a unit.
	Text(value string) string
	// Replacing is the clause that says "and if a row with this key is already
	// there, make it this one".
	Replacing(key []string, columns []string) string
}

// Part is one piece of a statement: some text, some values, or the reason
// there is no statement.
type Part struct {
	text    string
	values  []dynamic.Value
	binds   bool
	refused error
}

// Text is a piece of a statement written as itself.
//
// Identifiers in it are the caller's to quote, through the dialect's own
// Quoted -- this says nothing about them, because a statement whose shape is
// the caller's has names the caller chose.
func Text(said string) Part {
	return Part{text: said}
}

// Bind is one or more values the statement binds, in the order given.
//
// Several rather than one because that is how they occur -- the columns of an
// insert, the members of an "in" -- and rendering them together is what puts
// the commas in one place. None is nothing at all, so a caller need not branch
// on an empty list.
func Bind(values ...dynamic.Value) Part {
	return Part{values: values, binds: true}
}

// Condition is a predicate as pieces of a statement written by hand.
//
// The bridge between the two ways of saying a statement, and the reason it
// exists is Following: a hand-written read model still has a page to cut, and
// a keyset comparison written out again beside a tested one is the copy that
// gets it wrong. The values it binds are numbered by the Compose it is spliced
// into, like every other piece.
func Condition(spelling Spelling, criterion Criterion) []Part {
	return criterion.node.parts(spelling)
}

// Refused is a piece that could not be written, and why.
//
// A statement carries its refusals rather than returning them, because the
// refusals are about the query's shape -- a column no source has -- and a
// shape is decided once while a statement is composed on every request. So a
// caller composes as it always did and the runners refuse to send a statement
// that says it is broken, with the reason. There is no path by which a refused
// statement reaches a server.
func Refused(why error) Part {
	return Part{refused: why}
}

// Computed is an expression as pieces of a statement written by hand.
//
// The other half of the bridge: a hand-written statement can still compute a
// value the way the specification does, so an operation a dialect spells its
// own way is spelled its own way here too.
func Computed(spelling Spelling, term Term) []Part {
	return term.node.parts(spelling)
}

// Composed is a rendered statement: the text a driver will see, and the values
// it binds, in agreement by construction -- or the reasons it could not be
// written.
type Composed struct {
	text    string
	values  []dynamic.Value
	refused error
}

// Compose renders the pieces for one dialect.
//
// The ordinal a dialect may want is counted here, across every piece, which is
// the reason this is one function rather than a method on each piece: a
// statement's second value is its second wherever in the statement it was
// written.
func Compose(marks Placeholders, parts ...Part) Composed {
	said := strings.Builder{}
	values := make([]dynamic.Value, 0, len(parts))
	refusals := make([]error, 0)
	bound := 0
	for _, part := range parts {
		if part.refused != nil {
			refusals = append(refusals, part.refused)
			continue
		}
		if !part.binds {
			said.WriteString(part.text)
			continue
		}
		makeed := make([]string, 0, len(part.values))
		for _, value := range part.values {
			bound++
			makeed = append(makeed, marks.Placeholder(bound))
			values = append(values, value)
		}
		said.WriteString(strings.Join(makeed, ", "))
	}
	return Composed{
		text:    said.String(),
		values:  values,
		refused: errors.Join(refusals...),
	}
}

// Text is the statement as the driver will see it.
func (composed Composed) Text() string { return composed.text }

// Values are what it binds, in the order it reads them.
func (composed Composed) Values() []dynamic.Value { return composed.values }

// Refused is why there is no statement, and nothing when there is one.
func (composed Composed) Refused() error { return composed.refused }

// errorsIn is the refusals of several things at once, and nothing when none of
// them refused.
func errorsIn(why ...error) error { return errors.Join(why...) }
