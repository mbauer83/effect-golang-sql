package sql

// What a repository does, said once and spelled by the dialect.
//
// A store over a described aggregate writes four kinds of statement: read a
// row by what identifies it, write one, write one over whatever is there, and
// remove one. Every store that has ever been written over this package has
// written those four by hand, and every one of them has had to know how its
// dialect spells a bound value -- which is the knowledge this exists to take
// back.
//
// The shapes are values rather than a builder with methods, because a
// statement is a thing with parts and not a sequence of instructions: a value
// can be read, compared and logged, and a caller filling in fields cannot get
// the order of a fluent chain wrong.
//
// Three of the four, because the select query grew into a file of its own,
// select.go. These three did not: a row is written, replaced or removed, and
// there is nothing about any of them a dialect spells differently except the
// upsert clause and the values they bind.

import (
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// InsertQuery is a row written to one table.
type InsertQuery struct {
	Table   string
	Columns []string
	Values  []dynamic.Value
}

// Statement is this insert, spelled for a dialect.
func (insert InsertQuery) Statement(spelling Spelling) Statement {
	return Compose(spelling,
		Text("insert into "+spelling.QuoteIdentifier(insert.Table)+
			" ("+names(spelling, insert.Columns)+") values ("),
		Bind(insert.Values...),
		Text(")"),
	)
}

// UpsertQuery is a row written over whatever is there under the same key.
//
// One statement rather than a read and a branch: whether a row is new is not a
// question worth a round trip, and two callers deciding it separately is how a
// duplicate key surfaces under load. The clause that says so is the one thing
// the three dialects spell three ways, which is why the dialect writes it.
type UpsertQuery struct {
	Table   string
	Columns []string
	// Key is what a conflict is judged on, which is the row's identity.
	Key    []string
	Values []dynamic.Value
}

// Statement is this upsert, spelled for a dialect.
func (query UpsertQuery) Statement(spelling Spelling) Statement {
	return Compose(spelling,
		Text("insert into "+spelling.QuoteIdentifier(query.Table)+
			" ("+names(spelling, query.Columns)+") values ("),
		Bind(query.Values...),
		Text(") "+spelling.UpsertClause(query.Key, query.Columns)),
	)
}

// DeleteQuery is rows taken out of one table.
type DeleteQuery struct {
	Table string
	Where Criterion
}

// Statement is this delete, spelled for a dialect.
func (query DeleteQuery) Statement(spelling Spelling) Statement {
	parts := []Part{Text("delete from " + spelling.QuoteIdentifier(query.Table))}
	parts = append(parts, clause(spelling, " where ", query.Where)...)
	return Compose(spelling, parts...)
}

// names is a list of identifiers as this dialect writes them.
func names(spelling Spelling, columns []string) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, spelling.QuoteIdentifier(column))
	}
	return strings.Join(quoted, ", ")
}
