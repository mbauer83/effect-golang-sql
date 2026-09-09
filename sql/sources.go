package sql

// Where a reading's rows come from, and what it may ask of them.
//
// A table, a named expression, or another reading -- each under a name the
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
	table   string
	holds   []Holding
	derived *Reading
	alias   string
}

// Holding is one column a source has, and what it holds.
type Holding struct {
	Name string
	Kind Kind
}

// Holds is a column of a kind, for building a source's column list.
func Holds(name string, kind Kind) Holding { return Holding{Name: name, Kind: kind} }

// From is a table, and the columns it holds when the caller knows them.
//
// Given none it checks nothing, which is honest about having been told nothing
// rather than a second constructor: a query over a table this module has no
// description of is still a query, and refusing to express it would send the
// caller back to writing SQL.
func From(table string, holds ...Holding) Source {
	return Source{table: table, holds: holds}
}

// Deriving is another reading, read as though it were a table.
//
// What a query needs when a value it computes has to be filtered, grouped or
// paged by: a name given in a select list cannot be used in the where clause
// that computes it, so the value is computed by one reading and used by the
// one reading it. The columns are the inner reading's own selections, so
// expressions taken from it are checked the same way a table's are.
func Deriving(reading Reading) Source {
	return Source{derived: &reading, holds: selectionHoldings(reading)}
}

// Named is this source under a name the query calls it by.
func Named(source Source, alias string) Source {
	source.alias = alias
	return source
}

// As is Named as a method, for the common case of naming a source where it is
// built.
func (source Source) As(alias string) Source { return Named(source, alias) }

// Of is one of a source's columns, read as the type the caller says it holds.
//
// Checked twice where the source was told: that the source has a column of
// that name, and that what it holds is what this reads it as. A source that
// was told nothing checks nothing.
//
// A package function rather than a method because Go has no generic methods,
// and the type is the point.
func Of[A any](source Source, name string) Expr[A] {
	holdingNamed, known := source.holdingNamed(name)
	switch {
	case len(source.holds) > 0 && !known:
		return Refusing[A](unknownColumn(source.sourceName(), name, source.named()))
	case !holdingNamed.Kind.Admits(kindOf[A]()):
		return Refusing[A](fmt.Errorf("sql: %q.%q holds %s and was read as %s",
			source.sourceName(), name, holdingNamed.Kind.Named(), kindOf[A]().Named()))
	}
	return Expr[A]{node: node{kind: aColumn, source: source.alias, name: name}}
}

// Every is all of this source's columns, in the order it holds them, which is
// what a store reading a whole row asks for.
func (source Source) Every() []Selection {
	chosen := make([]Selection, 0, len(source.holds))
	for _, heldValue := range source.holds {
		chosen = append(chosen, Selection{
			term: node{kind: aColumn, source: source.alias, name: heldValue.Name},
		})
	}
	return chosen
}

func (source Source) holdingNamed(name string) (Holding, bool) {
	for _, heldValue := range source.holds {
		if heldValue.Name == name {
			return heldValue, true
		}
	}
	return Holding{}, false
}

func (source Source) named() []string {
	makeed := make([]string, 0, len(source.holds))
	for _, heldValue := range source.holds {
		makeed = append(makeed, heldValue.Name)
	}
	return makeed
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

// isNothing reports whether a reading was given no source at all.
func (source Source) isNothing() bool {
	return source.table == "" && source.derived == nil
}

func (source Source) parts(spelling Spelling) []Part {
	parts := []Part{}
	if source.derived != nil {
		parts = append(parts, Text("("))
		parts = append(parts, source.derived.selection(spelling)...)
		parts = append(parts, Text(")"))
	} else {
		parts = append(parts, Text(spelling.Quoted(source.table)))
	}
	if source.alias != "" {
		parts = append(parts, Text(" "+spelling.Quoted(source.alias)))
	}
	return parts
}

func (source Source) sourceRefusal() error {
	if source.derived == nil {
		return nil
	}
	return source.derived.readingRefusal()
}

func noSource() error {
	return fmt.Errorf("sql: a reading has to say where its rows come from")
}

func nothingSelected() error {
	return fmt.Errorf("sql: a reading has to say what it answers with")
}

func unknownColumn(table string, name string, text []string) error {
	slices.Sort(text)
	return fmt.Errorf("sql: %q has no column %q; it has %v", table, name, text)
}
