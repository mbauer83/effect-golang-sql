package sql

// A query, said as what it asks rather than as text.
//
// One shape, because a select is one thing with parts: what it answers with,
// where the rows come from, what else they are joined to, which of them it
// keeps, how they are grouped, which groups it keeps, what order they are read
// in, where a page resumes and how many rows it holds -- and, before all of
// it, the expressions it names for its own use.
//
// A value rather than a builder with methods, because a query is a thing with
// parts and not a sequence of instructions: it can be read, compared and
// logged, a caller filling in fields cannot get the order of a fluent chain
// wrong, and a store can hold one and vary a field.
//
// Every part is optional except the two that make it a query at all -- what it
// answers with and where from -- and a reading missing either says so rather
// than composing a statement a server would reject.

import (
	"strconv"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// Reading is a query.
type Reading struct {
	// With are expressions this reading names for its own use, each available
	// to the ones after it and to the reading itself.
	With []Expression
	// Select is what it answers with, in the order the rows are decoded.
	Select []Selection
	// From is where the rows come from, and Joining what else they are read
	// with.
	From    Source
	Joining []Join
	// Where is which rows it keeps.
	Where Criterion
	// Grouped is what the rows are collapsed by, and Having which of the
	// groups are kept.
	//
	// Two fields rather than one, because they are two decisions and the
	// second is the one that is easy to get wrong: a criterion over an
	// aggregate belongs in Having and one over a column belongs in Where, and
	// a server will say so -- but only for the direction that is illegal. A
	// column criterion put in Having is legal, runs, and reads every row of
	// every group before discarding it.
	Grouped []Term
	Having  Criterion
	// Ordered is what order the rows are read in, and nothing means whatever
	// order the database gives -- which for a single row is every order.
	Ordered []Ordering
	// After is a position in that order, and the reading is the rows following
	// it. Nothing is from the beginning.
	//
	// Here rather than as a criterion the caller ands into Where, because the
	// criterion depends on the order and this is where the order is stated:
	// one statement of it, so a page cannot be read in one order and cut in
	// another.
	After []dynamic.Value
	// Rows is how many at most, and zero is all of them: a limit of no rows is
	// not a question anybody asks.
	Rows int
}

// Statement is this reading, spelled for a dialect.
func (reading Reading) Statement(spelling Spelling) Composed {
	return Compose(spelling, reading.selection(spelling)...)
}

// Reads is this reading as a source another one can read from, under a name.
//
// The shorthand for the arrangement a computed value forces: a name given in a
// select list cannot be used in the where clause that computes it, so the
// value is computed by one reading and filtered or paged by the one reading
// it.
func (reading Reading) Reads(alias string) Source {
	return Deriving(reading).As(alias)
}

// Answers is this reading as one value of another, which is a scalar subquery
// under the type it answers.
func Answers[A any](reading Reading) Expr[A] { return Answering[A](reading) }

// selection is the reading's own pieces, so that a reading nested in an
// expression, a source or a named expression is rendered by the same code as
// one at the top.
func (reading Reading) selection(spelling Spelling) []Part {
	if why := reading.refused(); why != nil {
		return []Part{Refused(why)}
	}
	parts := reading.naming(spelling)
	parts = append(parts, Text("select "))
	for at, chosen := range reading.Select {
		if at > 0 {
			parts = append(parts, Text(", "))
		}
		parts = append(parts, chosen.parts(spelling)...)
	}
	parts = append(parts, Text(" from "))
	parts = append(parts, reading.From.parts(spelling)...)
	for _, join := range reading.Joining {
		parts = append(parts, join.parts(spelling)...)
	}
	parts = append(parts, clause(spelling, " where ", reading.keeping())...)
	parts = append(parts, reading.grouping(spelling)...)
	parts = append(parts, clause(spelling, " having ", reading.Having)...)
	if len(reading.Ordered) > 0 {
		parts = append(parts, Text(" order by "))
		parts = append(parts, ordering(spelling, reading.Ordered)...)
	}
	if reading.Rows > 0 {
		parts = append(parts, Text(" limit "+strconv.Itoa(reading.Rows)))
	}
	return parts
}

// naming is the expressions the reading names before it asks anything.
func (reading Reading) naming(spelling Spelling) []Part {
	if len(reading.With) == 0 {
		return nil
	}
	parts := []Part{Text("with ")}
	for at, expression := range reading.With {
		if at > 0 {
			parts = append(parts, Text(", "))
		}
		parts = append(parts, expression.parts(spelling)...)
	}
	return append(parts, Text(" "))
}

// keeping is which rows it keeps: what the caller asked, and the page it
// resumes at.
func (reading Reading) keeping() Criterion {
	return Both(reading.Where, Following(reading.Ordered, reading.After))
}

func (reading Reading) grouping(spelling Spelling) []Part {
	if len(reading.Grouped) == 0 {
		return nil
	}
	return append([]Part{Text(" group by ")},
		listed(spelling, nodesOf(reading.Grouped))...)
}

// refused is why this reading is not a query: a part that could not be
// written, or a part missing that has to be there.
func (reading Reading) refused() error {
	why := []error{
		reading.From.refused(),
		reading.Where.held.refused,
		reading.Having.held.refused,
	}
	if len(reading.Select) == 0 {
		why = append(why, nothingSelected())
	}
	if reading.From.isNothing() {
		why = append(why, noSource())
	}
	for _, chosen := range reading.Select {
		why = append(why, chosen.term.refused)
	}
	for _, join := range reading.Joining {
		why = append(why, join.refused())
	}
	for _, expression := range reading.With {
		why = append(why, expression.reading.refused())
	}
	why = append(why, refusalsIn(reading.Grouped)...)
	for _, one := range reading.Ordered {
		why = append(why, one.term.refused)
	}
	return errorsIn(why...)
}

// clause is a criterion under the word that introduces it, and nothing at all
// when it excludes nothing.
func clause(spelling Spelling, word string, criterion Criterion) []Part {
	if criterion.Unsaid() {
		return nil
	}
	return append([]Part{Text(word)}, criterion.held.parts(spelling)...)
}
