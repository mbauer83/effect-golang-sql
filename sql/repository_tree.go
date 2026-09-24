package sql

// An aggregate's rows across its tables: read a level at a time, assembled
// into the aggregate's value, and taken apart again into each table's rows.
//
// Every table beneath the root is read with one statement, for all the rows
// the level above it read -- so an aggregate of three tables is three
// statements whether it holds one child or a thousand, and a page of forty
// aggregates is the same three.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// readTree reads, for the root rows given, every table beneath them: read[i]
// are table i's rows, read[0] the roots.
//
// Each table is read with one statement whatever the level: its rows are those
// whose holder is among the rows of the level above -- asked as a query of
// that level, back to the roots -- so the only values bound are the roots'
// keys, however many rows lie between.
func readTree[R any](database Querier, spelling Spelling, laid []TableLayout, roots []dynamic.Object) effect.Effect[R, Fault, [][]dynamic.Object] {
	read := make([][]dynamic.Object, len(laid))
	read[0] = roots
	if len(roots) == 0 || len(laid) == 1 {
		return effect.For[R, Fault]().Succeed(read)
	}
	var level func(at int) effect.Effect[R, Fault, [][]dynamic.Object]
	level = func(at int) effect.Effect[R, Fault, [][]dynamic.Object] {
		if at == len(laid) {
			return effect.For[R, Fault]().Succeed(read)
		}
		table := laid[at]
		source := From(table.Name, table.Columns...)
		query := SelectQuery{Select: source.Columns(), From: source, Where: rowsBeneath(laid, at, roots)}
		if table.Position != "" {
			query.OrderBy = []Ordering{{term: node{kind: aColumn, source: table.Name, name: table.Position}}}
		}
		return effect.RunCollect(rawRows[R](database, query.Statement(spelling))).
			FlatMap(func(rows []dynamic.Object) effect.Effect[R, Fault, [][]dynamic.Object] {
				read[at] = rows
				return level(at + 1)
			})
	}
	return level(1)
}

// rowsBeneath is table at's rows that belong to the roots: its holder column
// among the holders' own, which are asked as a query of theirs.
func rowsBeneath(laid []TableLayout, at int, roots []dynamic.Object) Criterion {
	if at == 0 {
		root := laid[0]
		keys := make([]Expr[bool], 0, len(roots))
		for _, row := range roots {
			value, _ := row.Member(root.Key[0])
			keys = append(keys, Expr[bool]{node: node{kind: aValue, value: value}})
		}
		return In(Expr[bool]{node: node{kind: aColumn, source: root.Name, name: root.Key[0]}}, keys...)
	}
	table, holder := laid[at], laid[holderOf(laid, at)]
	holders := From(holder.Name, holder.Columns...)
	column := func(source string, name string) Term {
		return Term{node: node{kind: aColumn, source: source, name: name}}
	}
	above := rowsBeneath(laid, holderOf(laid, at), roots)
	link := table.Parent
	if len(link.Columns) == 1 {
		return InQuery(Expr[bool]{node: column(table.Name, link.Columns[0]).node},
			SelectQuery{Select: SelectTerms(column(holder.Name, link.Targets[0])), From: holders, Where: above})
	}
	// A holder keyed by several columns: the rows whose holder is one of
	// theirs, asked of each row.
	matched := make([]Criterion, 0, len(link.Columns)+1)
	for index, name := range link.Columns {
		matched = append(matched, Apply[bool](EqualTo, column(holder.Name, link.Targets[index]), column(table.Name, name)))
	}
	return Exists(SelectQuery{Select: SelectTerms(column(holder.Name, link.Targets[0])), From: holders,
		Where: And(append(matched, above)...)})
}

// holderOf is the index of the table whose rows hold table at's.
func holderOf(laid []TableLayout, at int) int {
	for index := range at {
		if laid[index].Name == laid[at].Parent.Table {
			return index
		}
	}
	return 0
}

// assemble is each root read, with every table beneath it put back as the
// members they are: a list, one entity, or references.
func assemble(laid []TableLayout, read [][]dynamic.Object) []dynamic.Object {
	// built[i] are table i's rows as values, grouped by the holder they
	// belong to, in the order read.
	built := make([]map[string][]dynamic.Value, len(laid))
	var roots []dynamic.Object
	for at := len(laid) - 1; at >= 0; at-- {
		table := laid[at]
		built[at] = map[string][]dynamic.Value{}
		for _, row := range read[at] {
			var value dynamic.Value
			if table.Parent != nil && table.Parent.Element != "" {
				value, _ = row.Member(table.Parent.Element)
			} else {
				object := dynamic.Object{}
				for _, field := range row.Fields {
					if table.Parent != nil && (holds(table.Parent.Columns, field.Name) || field.Name == table.Position) {
						continue
					}
					object.Fields = append(object.Fields, field)
				}
				for below := at + 1; below < len(laid); below++ {
					if laid[below].Parent == nil || laid[below].Parent.Table != table.Name {
						continue
					}
					link := laid[below].Parent
					held := built[below][tupleOf(row, link.Targets)]
					switch {
					case !link.Single:
						object.Fields = append(object.Fields, dynamic.Field{Name: link.Field, Value: dynamic.List{Elements: nonNil(held)}})
					case len(held) > 0:
						object.Fields = append(object.Fields, dynamic.Field{Name: link.Field, Value: held[0]})
					default:
						object.Fields = append(object.Fields, dynamic.Field{Name: link.Field, Value: dynamic.Absent{}})
					}
				}
				value = object
				if at == 0 {
					roots = append(roots, object)
				}
			}
			if table.Parent != nil {
				holder := tupleOf(row, table.Parent.Columns)
				built[at][holder] = append(built[at][holder], value)
			}
		}
	}
	return roots
}

func nonNil(values []dynamic.Value) []dynamic.Value {
	if values == nil {
		return []dynamic.Value{}
	}
	return values
}
