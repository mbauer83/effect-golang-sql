package sql

// What else a query's rows are read with, and what it names for its own use.
//
// A join relates a second source to the rows already there; a common table
// expression is a query available to the one that names it. Both are here
// because both are ways of bringing more rows into a query, and both are
// checked the same way a table is: an expression taken from either is taken
// from a source that knows its columns.

import "fmt"

// Join is a second source, and what relates its rows to the ones already
// there.
type Join struct {
	source Source
	on     Criterion
	outer  bool
}

// InnerJoin keeps the rows that match on both sides.
func InnerJoin(source Source, on Criterion) Join {
	return Join{source: source, on: on}
}

// LeftJoin keeps every row already there, matched or not -- a left outer
// join.
//
// The one people reach for and the one that changes an answer: a shelf read by
// joining what somebody owns to what they have watched loses every disc they
// have not watched, and loses it silently.
//
// There is no right outer join here, because a right outer join is this one
// written the other way round; and no full outer, because MySQL has none and a
// specification that emitted one would compose a statement one of the three
// servers cannot run. A program that needs one on a server that has it
// declares the operation and writes the clause with Compose.
func LeftJoin(source Source, on Criterion) Join {
	return Join{source: source, on: on, outer: true}
}

func (join Join) parts(spelling Spelling) []Part {
	word := " JOIN "
	if join.outer {
		word = " LEFT JOIN "
	}
	parts := []Part{Text(word)}
	parts = append(parts, join.source.parts(spelling)...)
	parts = append(parts, Text(" ON "))
	return append(parts, join.on.node.parts(spelling)...)
}

func (join Join) refusal() error {
	return errorsIn(join.source.refusal(), join.on.node.err)
}

// CTE is a query under a name, available to the query that names
// it -- a common table expression.
//
// Worth having beside FromQuery for the two things a derived table cannot do:
// be read twice in one query without being computed twice, and be read by a
// name that says what it is. Where a server materialises it is the server's
// decision and not this module's.
type CTE struct {
	name  string
	query SelectQuery
}

// With is a query under a name.
func With(name string, query SelectQuery) CTE {
	return CTE{name: name, query: query}
}

// Source is this expression as somewhere to read from, knowing the columns its
// reading answers with.
func (cte CTE) Source() Source {
	return Source{table: cte.name, columns: selectionColumns(cte.query)}
}

func (cte CTE) parts(spelling Spelling) []Part {
	parts := []Part{Text(spelling.QuoteIdentifier(cte.name) + " AS (")}
	parts = append(parts, cte.query.parts(spelling)...)
	return append(parts, Text(")"))
}

// selectionColumns is what a query's selections answer to, with what each holds
// where the query's own expressions said.
func selectionColumns(query SelectQuery) []ColumnType {
	holds := make([]ColumnType, 0, len(query.Select))
	for _, selection := range query.Select {
		if name := selection.Name(); name != "" {
			holds = append(holds, ColumnType{Name: name})
		}
	}
	return holds
}

// ColumnsEqual is the rows where a column of one source holds what a column of
// another does: a join's condition on a key.
//
// Neither column's Go type is named, because a key's two ends hold the same
// kind by being a key; what is checked is that each source has the column,
// where it was told its columns, and that the two hold kinds that can be
// compared.
func ColumnsEqual(left Source, leftColumn string, right Source, rightColumn string) Criterion {
	leftTerm, leftKind := columnTerm(left, leftColumn)
	rightTerm, rightKind := columnTerm(right, rightColumn)
	if !leftKind.Admits(rightKind) {
		return Apply[bool](EqualTo, Refuse[bool](fmt.Errorf(
			"sql: %q.%q holds %s and %q.%q holds %s, so a join cannot compare them",
			left.sourceName(), leftColumn, leftKind.String(),
			right.sourceName(), rightColumn, rightKind.String())).Term(), rightTerm)
	}
	return Apply[bool](EqualTo, leftTerm, rightTerm)
}

func columnTerm(source Source, name string) (Term, Kind) {
	column, known := source.columnType(name)
	if len(source.columns) > 0 && !known {
		return Refuse[bool](unknownColumn(source.sourceName(), name, source.columnNames())).Term(), OfUnknown
	}
	// Qualified always, by the alias or else the table: both ends of a key are
	// often called the same -- two tables with an id -- and a condition that
	// named a column bare would be one the server refuses as ambiguous.
	qualifier := source.alias
	if qualifier == "" {
		qualifier = source.table
	}
	return Term{node: node{kind: aColumn, source: qualifier, name: name}}, column.Kind
}
