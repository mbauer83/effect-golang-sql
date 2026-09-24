package sql

// What a save or a delete says to the database: the rows gone, deleted from
// the bottom up, and the rows new or changed, written from the top down, so a
// holder is there before what it holds and goes after it.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// changes are the statements that make the kept rows the desired ones.
func changes(spelling Spelling, laid []TableLayout, existing [][]dynamic.Object, desired [][]dynamic.Object) []Statement {
	var deletes, writes []Statement
	for at := len(laid) - 1; at >= 0; at-- {
		wanted := map[string]bool{}
		for _, row := range desired[at] {
			wanted[keyOf(laid[at], row)] = true
		}
		for _, row := range existing[at] {
			if !wanted[keyOf(laid[at], row)] {
				deletes = append(deletes, deleteRow(spelling, laid[at], row))
			}
		}
	}
	for at := range laid {
		held := map[string]dynamic.Object{}
		for _, row := range existing[at] {
			held[keyOf(laid[at], row)] = row
		}
		for _, row := range desired[at] {
			if kept, found := held[keyOf(laid[at], row)]; found && sameRow(kept, row) {
				continue
			}
			writes = append(writes, upsertRow(spelling, laid[at], row))
		}
	}
	return append(deletes, writes...)
}

func upsertRow(spelling Spelling, table TableLayout, row dynamic.Object) Statement {
	columns := make([]string, 0, len(row.Fields))
	values := make([]dynamic.Value, 0, len(row.Fields))
	for _, field := range row.Fields {
		columns = append(columns, field.Name)
		values = append(values, field.Value)
	}
	return UpsertQuery{Table: table.Name, Columns: columns, Key: table.Key, Values: values}.Statement(spelling)
}

func deleteRow(spelling Spelling, table TableLayout, row dynamic.Object) Statement {
	criteria := make([]Criterion, 0, len(table.Key))
	for _, column := range table.Key {
		value, _ := row.Member(column)
		criteria = append(criteria, Apply[bool](EqualTo,
			Term{node: node{kind: aColumn, source: table.Name, name: column}},
			Term{node: node{kind: aValue, value: value}}))
	}
	return DeleteQuery{Table: table.Name, Where: And(criteria...)}.Statement(spelling)
}

// rootIs is the root row whose key column holds that value.
func rootIs(table TableLayout, key string, value dynamic.Value) Criterion {
	return Apply[bool](EqualTo,
		Term{node: node{kind: aColumn, source: table.Name, name: key}},
		Term{node: node{kind: aValue, value: value}})
}

// runAll runs the statements in order.
func runAll[R any](database Querier, statements []Statement) effect.Effect[R, Fault, effect.Unit] {
	return effect.ForEach(statements, func(statement Statement) effect.Effect[R, Fault, Outcome] {
		return Run[R](database, statement)
	}).As(effect.Unit{})
}

// inTransaction runs work in a transaction of its own, or in the one it is
// given: a Querier that can begin one is a database, and one that cannot is a
// transaction already.
func inTransaction[R, A any](database Querier, work func(Querier) effect.Effect[R, Fault, A]) effect.Effect[R, Fault, A] {
	if beginner, can := database.(Beginner); can {
		return Transact(beginner, func(fault Fault) Fault { return fault }, work)
	}
	return work(database)
}
