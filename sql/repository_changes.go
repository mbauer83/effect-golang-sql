package sql

// What a save or a delete says to the database: the rows gone, deleted from
// the bottom up, and the rows new or changed, written from the top down, so a
// holder is there before what it holds and goes after it. Rows of one table
// are deleted and written in batches, a statement for many.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// mostBound is how many values one statement binds at most: within what
// SQLite, Postgres and MySQL all take.
const mostBound = 30000

// changes are the statements that make the kept rows the desired ones. The
// kept rows are the stored ones when read, so an ordered list keeps the
// positions it can; when they are what the caller says was stored, a list
// whose order changed or that gained elements is numbered afresh, and one that
// did not keeps the positions it has.
func changes(spelling Spelling, laid []TableLayout, kept [][]dynamic.Object, desired [][]dynamic.Object, read bool) []Statement {
	var deletes, writes []Statement
	for at := len(laid) - 1; at >= 0; at-- {
		deletes = append(deletes, deleteRows(spelling, laid[at], gone(laid[at], kept[at], desired[at]))...)
	}
	for at, table := range laid {
		switch {
		case table.Position == "" || read:
			wanted := desired[at]
			if table.Position != "" {
				wanted = placeRows(table, kept[at], wanted)
			}
			writes = append(writes, upsertRows(spelling, table, changed(table, kept[at], wanted, ""))...)
		default:
			renumbered, updated := listChanges(table, kept[at], desired[at])
			writes = append(writes, upsertRows(spelling, table, renumbered)...)
			for _, row := range updated {
				writes = append(writes, updateRow(spelling, table, row))
			}
		}
	}
	return append(deletes, writes...)
}

// gone are the kept rows no desired row has the key of.
func gone(table TableLayout, kept []dynamic.Object, desired []dynamic.Object) []dynamic.Object {
	wanted := map[string]bool{}
	for _, row := range desired {
		wanted[keyOf(table, row)] = true
	}
	var rows []dynamic.Object
	for _, row := range kept {
		if !wanted[keyOf(table, row)] {
			rows = append(rows, row)
		}
	}
	return rows
}

// changed are the desired rows new, or different from the kept row of their
// key in any column but the one ignored.
func changed(table TableLayout, kept []dynamic.Object, desired []dynamic.Object, ignored string) []dynamic.Object {
	held := map[string]dynamic.Object{}
	for _, row := range kept {
		held[keyOf(table, row)] = row
	}
	var rows []dynamic.Object
	for _, row := range desired {
		if stored, found := held[keyOf(table, row)]; found && sameRow(stored, row, ignored) {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// listChanges are, for an ordered table whose stored positions are not
// known, the rows of each list numbered afresh when its order changed or it
// gained elements, and otherwise the rows changed in their other columns.
func listChanges(table TableLayout, kept []dynamic.Object, desired []dynamic.Object) ([]dynamic.Object, []dynamic.Object) {
	before := map[string][]dynamic.Object{}
	for _, group := range groupedByHolder(table, kept) {
		before[tupleOf(group[0], table.Parent.Columns)] = group
	}
	var renumbered, updated []dynamic.Object
	for _, group := range groupedByHolder(table, desired) {
		previous := before[tupleOf(group[0], table.Parent.Columns)]
		if sameOrder(table, previous, group) {
			updated = append(updated, changed(table, previous, group, table.Position)...)
			continue
		}
		for at, row := range group {
			renumbered = append(renumbered, withMember(row, table.Position, dynamic.OfInteger(int64(at)*positionGap)))
		}
	}
	return renumbered, updated
}

// sameOrder says the desired list holds only elements the kept one did, in
// the order they were: removing elements moves no other.
func sameOrder(table TableLayout, kept []dynamic.Object, desired []dynamic.Object) bool {
	at := 0
	for _, row := range desired {
		key := keyOf(table, row)
		for at < len(kept) && keyOf(table, kept[at]) != key {
			at++
		}
		if at == len(kept) {
			return false
		}
		at++
	}
	return true
}

// upsertRows writes rows of one table, as many to a statement as it binds.
func upsertRows(spelling Spelling, table TableLayout, rows []dynamic.Object) []Statement {
	if len(rows) == 0 {
		return nil
	}
	columns := make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		columns = append(columns, column.Name)
	}
	perStatement := max(1, mostBound/len(columns))
	var statements []Statement
	for start := 0; start < len(rows); start += perStatement {
		batch := rows[start:min(start+perStatement, len(rows))]
		values := make([][]dynamic.Value, 0, len(batch))
		for _, row := range batch {
			values = append(values, valuesOf(row, columns))
		}
		statements = append(statements,
			UpsertQuery{Table: table.Name, Columns: columns, Key: table.Key, Rows: values}.Statement(spelling))
	}
	return statements
}

// updateRow writes a row's columns but its key and its position.
func updateRow(spelling Spelling, table TableLayout, row dynamic.Object) Statement {
	keyed := map[string]bool{table.Position: true}
	for _, column := range table.Key {
		keyed[column] = true
	}
	var columns []string
	var values []dynamic.Value
	for _, field := range row.Fields {
		if !keyed[field.Name] {
			columns = append(columns, field.Name)
			values = append(values, field.Value)
		}
	}
	return UpdateQuery{Table: table.Name, Columns: columns, Values: values, Where: keyIs(table, row)}.Statement(spelling)
}

// deleteRows removes rows of one table, grouped by all of their key but its
// last column -- the holder they belong to -- as many to a statement as it
// binds.
func deleteRows(spelling Spelling, table TableLayout, rows []dynamic.Object) []Statement {
	if len(rows) == 0 {
		return nil
	}
	holder, last := table.Key[:len(table.Key)-1], table.Key[len(table.Key)-1]
	groups := map[string][]dynamic.Object{}
	var order []string
	for _, row := range rows {
		key := tupleOf(row, holder)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], row)
	}
	var statements []Statement
	for _, key := range order {
		group := groups[key]
		for start := 0; start < len(group); start += mostBound {
			batch := group[start:min(start+mostBound, len(group))]
			values := make([]Expr[bool], 0, len(batch))
			for _, row := range batch {
				value, _ := row.Member(last)
				values = append(values, Expr[bool]{node: node{kind: aValue, value: value}})
			}
			criteria := []Criterion{In(Expr[bool]{node: node{kind: aColumn, source: table.Name, name: last}}, values...)}
			for _, column := range holder {
				value, _ := batch[0].Member(column)
				criteria = append(criteria, columnIs(table.Name, column, value))
			}
			statements = append(statements, DeleteQuery{Table: table.Name, Where: And(criteria...)}.Statement(spelling))
		}
	}
	return statements
}

func keyIs(table TableLayout, row dynamic.Object) Criterion {
	criteria := make([]Criterion, 0, len(table.Key))
	for _, column := range table.Key {
		value, _ := row.Member(column)
		criteria = append(criteria, columnIs(table.Name, column, value))
	}
	return And(criteria...)
}

func columnIs(table string, column string, value dynamic.Value) Criterion {
	return Apply[bool](EqualTo,
		Term{node: node{kind: aColumn, source: table, name: column}},
		Term{node: node{kind: aValue, value: value}})
}

func valuesOf(row dynamic.Object, columns []string) []dynamic.Value {
	values := make([]dynamic.Value, 0, len(columns))
	for _, column := range columns {
		value, present := row.Member(column)
		if !present {
			value = dynamic.Absent{}
		}
		values = append(values, value)
	}
	return values
}

// rootIs is the root row whose key column holds that value.
func rootIs(table TableLayout, key string, value dynamic.Value) Criterion {
	return columnIs(table.Name, key, value)
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
