package sql

// What a save or a delete says to the database: rows gone, deleted from the
// bottom up; rows new, inserted, and rows changed, updated by their key, from
// the top down -- so a holder is there before what it holds and goes after it.
// New rows and gone rows of one table are batched, a statement for many.
//
// No upserts: on MySQL an upsert updates whatever row collides on any unique
// key, so a new row whose unique value another aggregate holds would change
// that aggregate. An insert is refused instead, on every server.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// mostBound is how many values one statement binds at most: within what
// SQLite, Postgres and MySQL all take.
const mostBound = 30000

// changes are the statements that make the kept rows the desired ones. Where
// placed[i] is true, table i's kept rows hold their stored positions, and a
// list keeps the positions it can; where false, its lists kept their order and
// gained nothing, and positions are left as they are.
func changes(spelling Spelling, laid []TableLayout, kept [][]dynamic.Object, desired [][]dynamic.Object, placed []bool) []Statement {
	var deletes, writes []Statement
	for at := len(laid) - 1; at >= 0; at-- {
		deletes = append(deletes, deleteRows(spelling, laid[at], gone(laid[at], kept[at], desired[at]))...)
	}
	for at, table := range laid {
		wanted, ignored := desired[at], table.Position
		if table.Position != "" && placed[at] {
			wanted, ignored = placeRows(table, kept[at], wanted), ""
		}
		held := map[string]dynamic.Object{}
		for _, row := range kept[at] {
			held[keyOf(table, row)] = row
		}
		var inserted []dynamic.Object
		var updates []Statement
		for _, row := range wanted {
			stored, found := held[keyOf(table, row)]
			switch {
			case !found:
				inserted = append(inserted, row)
			case !sameRow(stored, row, ignored):
				updates = append(updates, updateRow(spelling, table, row, ignored))
			}
		}
		writes = append(append(writes, insertRows(spelling, table, inserted, nil)...), updates...)
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

// reordered says a list of the table changed order or gained an element
// between the two values, so where its elements stand has to be read.
func reordered(table TableLayout, before []dynamic.Object, after []dynamic.Object) bool {
	earlier := map[string][]dynamic.Object{}
	for _, group := range groupedByHolder(table, before) {
		earlier[tupleOf(group[0], table.Parent.Columns)] = group
	}
	for _, group := range groupedByHolder(table, after) {
		if !sameOrder(table, earlier[tupleOf(group[0], table.Parent.Columns)], group) {
			return true
		}
	}
	return false
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

// insertRows writes new rows of one table, as many to a statement as it
// binds, leaving out the columns left to the database.
func insertRows(spelling Spelling, table TableLayout, rows []dynamic.Object, omitted map[string]bool) []Statement {
	if len(rows) == 0 {
		return nil
	}
	var columns []string
	for _, column := range table.Columns {
		if !omitted[column.Name] {
			columns = append(columns, column.Name)
		}
	}
	perStatement := max(1, mostBound/max(1, len(columns)))
	var statements []Statement
	for start := 0; start < len(rows); start += perStatement {
		batch := rows[start:min(start+perStatement, len(rows))]
		values := make([][]dynamic.Value, 0, len(batch))
		for _, row := range batch {
			values = append(values, valuesOf(row, columns))
		}
		statements = append(statements, InsertQuery{Table: table.Name, Columns: columns, Rows: values}.Statement(spelling))
	}
	return statements
}

// updateRow writes a row's columns but its key, and but the column ignored.
func updateRow(spelling Spelling, table TableLayout, row dynamic.Object, ignored string) Statement {
	keyed := map[string]bool{}
	for _, column := range table.Key {
		keyed[column] = true
	}
	var columns []string
	var values []dynamic.Value
	for _, field := range row.Fields {
		if !keyed[field.Name] && (ignored == "" || field.Name != ignored) {
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

// everyTable is true for each table: every table's positions are known.
func everyTable(laid []TableLayout) []bool {
	placed := make([]bool, len(laid))
	for at := range placed {
		placed[at] = true
	}
	return placed
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
