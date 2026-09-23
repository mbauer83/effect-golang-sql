package sql

// Where a query's rows come from, and what it may ask of them.
//
// A table, a common table expression, or another query -- each under a name the
// query calls it by, because a query with two sources has to say which one a
// column belongs to and a query joined to a second copy of one table has no
// other way.
//
// A source built from a description checks the expressions taken from it,
// against the column's name and against the type the description says it
// holds. That is the schema-driven half: a store hands over the projection of
// the domain's own description, so a member renamed or retyped in the domain is
// a refusal where the query names it rather than a statement a server rejects
// at the first request.

import (
	"fmt"
	"slices"
)

// Source is where rows come from.
type Source struct {
	table    string
	columns  []ColumnType
	subquery *SelectQuery
	alias    string
}

// ColumnType is one column a source has, and what it holds.
type ColumnType struct {
	Name string
	Kind Kind
}

// ColumnOf is a column of a kind, for building a source's column list.
func ColumnOf(name string, kind Kind) ColumnType { return ColumnType{Name: name, Kind: kind} }

// From is a table, and the columns it holds when the caller knows them.
//
// Given none it checks nothing, which is honest about having been told nothing
// rather than a second constructor: a query over a table this module has no
// description of is still a query, and refusing to express it would send the
// caller back to writing SQL.
func From(table string, holds ...ColumnType) Source {
	return Source{table: table, columns: holds}
}

// FromQuery is another query, read as though it were a table.
//
// What a query needs when a value it computes has to be filtered, grouped or
// paged by: a name given in a select list cannot be used in the where clause
// that computes it, so the value is computed by one query and used by the
// one reading it. The columns are the inner query's own selections, so
// expressions taken from it are checked the same way a table's are.
func FromQuery(query SelectQuery) Source {
	return Source{subquery: &query, columns: selectionColumns(query)}
}

// Alias is this source under a name the query calls it by.
func Alias(source Source, alias string) Source {
	source.alias = alias
	return source
}

// As is Alias as a method, for the common case of naming a source where it is
// built.
func (source Source) As(alias string) Source { return Alias(source, alias) }

// Of is one of a source's columns, read as the type the caller says it holds.
//
// Checked twice where the source was told: that the source has a column of
// that name, and that what it holds is what this reads it as. A source that
// was told nothing checks nothing.
//
// A package function rather than a method because Go has no generic methods,
// and the type is the point.
func Of[A any](source Source, name string) Expr[A] {
	column, known := source.columnType(name)
	switch {
	case len(source.columns) > 0 && !known:
		return Refuse[A](unknownColumn(source.sourceName(), name, source.columnNames()))
	case !column.Kind.Admits(kindOf[A]()):
		return Refuse[A](fmt.Errorf("sql: %q.%q holds %s and was read as %s",
			source.sourceName(), name, column.Kind.String(), kindOf[A]().String()))
	}
	return Expr[A]{node: node{kind: aColumn, source: source.alias, name: name}}
}

// Columns is all of this source's columns, in the order it holds them, which is
// what a store reading a whole row asks for.
func (source Source) Columns() []Selection {
	selections := make([]Selection, 0, len(source.columns))
	for _, column := range source.columns {
		selections = append(selections, Selection{
			term: node{kind: aColumn, source: source.alias, name: column.Name},
		})
	}
	return selections
}

func (source Source) columnType(name string) (ColumnType, bool) {
	for _, column := range source.columns {
		if column.Name == name {
			return column, true
		}
	}
	return ColumnType{}, false
}

func (source Source) columnNames() []string {
	names := make([]string, 0, len(source.columns))
	for _, column := range source.columns {
		names = append(names, column.Name)
	}
	return names
}

// sourceName is what this source is known as, for a refusal that has to name it.
func (source Source) sourceName() string {
	switch {
	case source.alias != "":
		return source.alias
	case source.table != "":
		return source.table
	default:
		return "a derived table"
	}
}

// isEmpty reports whether a query was given no source at all.
func (source Source) isEmpty() bool {
	return source.table == "" && source.subquery == nil
}

func (source Source) parts(spelling Spelling) []Part {
	parts := []Part{}
	if source.subquery != nil {
		parts = append(parts, Text("("))
		parts = append(parts, source.subquery.parts(spelling)...)
		parts = append(parts, Text(")"))
	} else {
		parts = append(parts, Text(spelling.QuoteIdentifier(source.table)))
	}
	if source.alias != "" {
		parts = append(parts, Text(" "+spelling.QuoteIdentifier(source.alias)))
	}
	return parts
}

func (source Source) refusal() error {
	if source.subquery == nil {
		return nil
	}
	return source.subquery.refusal()
}

func noSource() error {
	return fmt.Errorf("sql: a reading has to say where its rows come from")
}

func noSelection() error {
	return fmt.Errorf("sql: a reading has to say what it answers with")
}

func unknownColumn(table string, name string, text []string) error {
	slices.Sort(text)
	return fmt.Errorf("sql: %q has no column %q; it has %v", table, name, text)
}
