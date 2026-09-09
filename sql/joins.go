package sql

// What else a reading's rows are read with, and what it names for its own use.
//
// A join relates a second source to the rows already there; a named expression
// is a reading available to the one that names it. Both are here because both
// are ways of bringing more rows into a query, and both are checked the same
// way a table is: an expression taken from either is taken from a source that
// knows its columns.

// Join is a second source, and what relates its rows to the ones already
// there.
type Join struct {
	source Source
	on     Criterion
	kept   bool
}

// Joining keeps the rows that match on both sides.
func Joining(source Source, on Criterion) Join {
	return Join{source: source, on: on}
}

// Including keeps every row already there, matched or not -- a left outer
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
func Including(source Source, on Criterion) Join {
	return Join{source: source, on: on, kept: true}
}

func (join Join) parts(spelling Spelling) []Part {
	word := " join "
	if join.kept {
		word = " left join "
	}
	parts := []Part{Text(word)}
	parts = append(parts, join.source.parts(spelling)...)
	parts = append(parts, Text(" on "))
	return append(parts, join.on.node.parts(spelling)...)
}

func (join Join) joinRefusal() error {
	return errorsIn(join.source.sourceRefusal(), join.on.node.refused)
}

// Expression is a reading under a name, available to the reading that names
// it -- a common table expression.
//
// Worth having beside Deriving for the two things a derived table cannot do:
// be read twice in one query without being computed twice, and be read by a
// name that says what it is. Where a server materialises it is the server's
// decision and not this module's.
type Expression struct {
	name    string
	reading Reading
}

// Naming is a reading under a name.
func Naming(name string, reading Reading) Expression {
	return Expression{name: name, reading: reading}
}

// Source is this expression as somewhere to read from, knowing the columns its
// reading answers with.
func (expression Expression) Source() Source {
	return Source{table: expression.name, holds: selectionHoldings(expression.reading)}
}

func (expression Expression) parts(spelling Spelling) []Part {
	parts := []Part{Text(spelling.Quoted(expression.name) + " as (")}
	parts = append(parts, expression.reading.selection(spelling)...)
	return append(parts, Text(")"))
}

// selectionHoldings is what a reading's selections answer to, with what each holds
// where the reading's own expressions said.
func selectionHoldings(reading Reading) []Holding {
	holds := make([]Holding, 0, len(reading.Select))
	for _, chosen := range reading.Select {
		if name := chosen.Named(); name != "" {
			holds = append(holds, Holding{Name: name})
		}
	}
	return holds
}
