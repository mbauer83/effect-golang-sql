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
	// Rows are further rows written by the same statement.
	Rows [][]dynamic.Value
}

// Statement is this insert, spelled for a dialect.
func (insert InsertQuery) Statement(spelling Spelling) Statement {
	return Compose(spelling, valuesParts(spelling, insert.Table, insert.Columns, insert.Values, insert.Rows)...)
}

// valuesParts is INSERT INTO table (columns) VALUES and each row bracketed.
func valuesParts(spelling Spelling, table string, columns []string, values []dynamic.Value, more [][]dynamic.Value) []Part {
	parts := []Part{Text("INSERT INTO " + spelling.QuoteIdentifier(table) +
		" (" + names(spelling, columns) + ") VALUES (")}
	rows := more
	if len(values) > 0 {
		rows = append([][]dynamic.Value{values}, rows...)
	}
	for at, row := range rows {
		if at > 0 {
			parts = append(parts, Text("), ("))
		}
		parts = append(parts, Bind(row...))
	}
	return append(parts, Text(")"))
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
	// Rows are further rows written by the same statement, each in the
	// columns' order: one round trip for many, which is what makes writing a
	// thousand children a handful of statements rather than a thousand.
	Rows [][]dynamic.Value
}

// Statement is this upsert, spelled for a dialect.
//
// On MySQL the upsert has no conflict target: a row that collides on any
// unique key, not only this one, is the row it updates. A table with a unique
// key besides its identity is written some other way there -- as a
// repository does.
func (query UpsertQuery) Statement(spelling Spelling) Statement {
	parts := valuesParts(spelling, query.Table, query.Columns, query.Values, query.Rows)
	return Compose(spelling, append(parts, Text(" "+spelling.UpsertClause(query.Key, query.Columns)))...)
}

// DeleteQuery is rows taken out of one table.
type DeleteQuery struct {
	Table string
	Where Criterion
}

// Statement is this delete, spelled for a dialect.
func (query DeleteQuery) Statement(spelling Spelling) Statement {
	parts := []Part{Text("DELETE FROM " + spelling.QuoteIdentifier(query.Table))}
	parts = append(parts, clause(spelling, " WHERE ", query.Where)...)
	return Compose(spelling, parts...)
}

// UpdateQuery is new values for columns of the rows a criterion admits.
type UpdateQuery struct {
	Table   string
	Columns []string
	Values  []dynamic.Value
	Where   Criterion
}

// Statement is this update, spelled for a dialect.
func (query UpdateQuery) Statement(spelling Spelling) Statement {
	parts := []Part{Text("UPDATE " + spelling.QuoteIdentifier(query.Table) + " SET ")}
	for index, column := range query.Columns {
		if index > 0 {
			parts = append(parts, Text(", "))
		}
		parts = append(parts, Text(spelling.QuoteIdentifier(column)+" = "))
		if index < len(query.Values) {
			parts = append(parts, Bind(query.Values[index]))
		}
	}
	parts = append(parts, clause(spelling, " WHERE ", query.Where)...)
	return Compose(spelling, parts...)
}

// names is a list of identifiers as this dialect writes them.
func names(spelling Spelling, columns []string) string {
	identifiers := make([]string, 0, len(columns))
	for _, column := range columns {
		identifiers = append(identifiers, spelling.QuoteIdentifier(column))
	}
	return strings.Join(identifiers, ", ")
}
