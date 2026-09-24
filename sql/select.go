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
// answers with and where from -- and a query missing either says so rather
// than composing a statement a server would reject.

import (
	"strconv"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// SelectQuery is a query.
type SelectQuery struct {
	// With are expressions this query names for its own use, each available
	// to the ones after it and to the query itself.
	With []CTE
	// Select is what it answers with, in the order the rows are decoded.
	Select []Selection
	// From is where the rows come from, and Joins what else they are read
	// with.
	From  Source
	Joins []Join
	// Where is which rows it keeps.
	Where Criterion
	// GroupBy is what the rows are collapsed by, and Having which of the
	// groups are kept.
	//
	// Two fields rather than one, because they are two decisions and the
	// second is the one that is easy to get wrong: a criterion over an
	// aggregate belongs in Having and one over a column belongs in Where, and
	// a server will say so -- but only for the direction that is illegal. A
	// column criterion put in Having is legal, runs, and reads every row of
	// every group before discarding it.
	GroupBy []Term
	Having  Criterion
	// OrderBy is what order the rows are read in, and nothing means whatever
	// order the database gives -- which for a single row is every order.
	OrderBy []Ordering
	// After is a position in that order, and the query is the rows following
	// it. Nothing is from the beginning.
	//
	// Here rather than as a criterion the caller ands into Where, because the
	// criterion depends on the order and this is where the order is stated:
	// one statement of it, so a page cannot be read in one order and cut in
	// another.
	After []dynamic.Value
	// Limit is how many at most, and zero is all of them: a limit of no rows is
	// not a question anybody asks.
	Limit int
	// Offset is how many rows of that order to pass over first: a numbered
	// page. It reads every row it passes over, which is why a listing reads a
	// numbered page's keys alone this way and joins the rows to them.
	Offset int
}

// Statement is this query, spelled for a dialect.
func (query SelectQuery) Statement(spelling Spelling) Statement {
	return Compose(spelling, query.parts(spelling)...)
}

// As is this query as a source another one can read from, under a name.
//
// The shorthand for the arrangement a computed value forces: a name given in a
// select list cannot be used in the where clause that computes it, so the
// value is computed by one query and filtered or paged by the one reading
// it.
func (query SelectQuery) As(alias string) Source {
	return FromQuery(query).As(alias)
}

// parts is the query's own pieces, so that a query nested in an expression,
// a source or a common table expression is rendered by the same code as one
// at the top.
func (query SelectQuery) parts(spelling Spelling) []Part {
	if why := query.refusal(); why != nil {
		return []Part{Refusal(why)}
	}
	parts := query.withClause(spelling)
	parts = append(parts, Text("SELECT "))
	for at, selection := range query.Select {
		if at > 0 {
			parts = append(parts, Text(", "))
		}
		parts = append(parts, selection.parts(spelling)...)
	}
	parts = append(parts, Text(" FROM "))
	parts = append(parts, query.From.parts(spelling)...)
	for _, join := range query.Joins {
		parts = append(parts, join.parts(spelling)...)
	}
	parts = append(parts, clause(spelling, " WHERE ", query.rowCriterion())...)
	parts = append(parts, query.groupByParts(spelling)...)
	parts = append(parts, clause(spelling, " HAVING ", query.Having)...)
	if len(query.OrderBy) > 0 {
		parts = append(parts, Text(" ORDER BY "))
		parts = append(parts, orderParts(spelling, query.OrderBy)...)
	}
	if query.Limit > 0 {
		parts = append(parts, Text(" LIMIT "+strconv.Itoa(query.Limit)))
	}
	if query.Offset > 0 {
		parts = append(parts, Text(" OFFSET "+strconv.Itoa(query.Offset)))
	}
	return parts
}

// withClause is the expressions the query names before it asks anything.
func (query SelectQuery) withClause(spelling Spelling) []Part {
	if len(query.With) == 0 {
		return nil
	}
	parts := []Part{Text("WITH ")}
	for at, expression := range query.With {
		if at > 0 {
			parts = append(parts, Text(", "))
		}
		parts = append(parts, expression.parts(spelling)...)
	}
	return append(parts, Text(" "))
}

// rowCriterion is which rows it keeps: what the caller asked, and the page it
// resumes at.
func (query SelectQuery) rowCriterion() Criterion {
	return Both(query.Where, After(query.OrderBy, query.After))
}

func (query SelectQuery) groupByParts(spelling Spelling) []Part {
	if len(query.GroupBy) == 0 {
		return nil
	}
	return append([]Part{Text(" GROUP BY ")},
		commaList(spelling, nodesOf(query.GroupBy))...)
}

// refusal is why this is not a query: a part that could not be
// written, or a part missing that has to be there.
func (query SelectQuery) refusal() error {
	why := []error{
		query.From.refusal(),
		query.Where.node.err,
		query.Having.node.err,
	}
	if len(query.Select) == 0 {
		why = append(why, noSelection())
	}
	if query.From.isEmpty() {
		why = append(why, noSource())
	}
	for _, selection := range query.Select {
		why = append(why, selection.term.err)
	}
	for _, join := range query.Joins {
		why = append(why, join.refusal())
	}
	for _, expression := range query.With {
		why = append(why, expression.query.refusal())
	}
	why = append(why, refusalsIn(query.GroupBy)...)
	for _, one := range query.OrderBy {
		why = append(why, one.term.err)
	}
	return errorsIn(why...)
}

// clause is a criterion under the word that introduces it, and nothing at all
// when it excludes nothing.
func clause(spelling Spelling, word string, criterion Criterion) []Part {
	if criterion.IsEmpty() {
		return nil
	}
	return append([]Part{Text(word)}, criterion.node.parts(spelling)...)
}
