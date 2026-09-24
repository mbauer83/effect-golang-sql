package sql

// An aggregate's rows across its tables: read a level at a time, assembled
// into the aggregate's value, and taken apart again into each table's rows.
//
// Every table beneath the root is read with one statement, for all the rows
// the level above it read -- so an aggregate of three tables is three
// statements whether it holds one child or a thousand, and a page of forty
// aggregates is the same three.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// readTree reads, for the root rows given, every table beneath them: read[i]
// are table i's rows, read[0] the roots.
func readTree[R any](database Querier, spelling Spelling, laid []TableLayout, roots []dynamic.Object) effect.Effect[R, Fault, [][]dynamic.Object] {
	read := make([][]dynamic.Object, len(laid))
	read[0] = roots
	var level func(at int) effect.Effect[R, Fault, [][]dynamic.Object]
	level = func(at int) effect.Effect[R, Fault, [][]dynamic.Object] {
		if at == len(laid) {
			return effect.For[R, Fault]().Succeed(read)
		}
		statement, holders := belowStatement(spelling, laid, at, read)
		if !holders {
			return level(at + 1)
		}
		return effect.RunCollect(rawRows[R](database, statement)).
			FlatMap(func(rows []dynamic.Object) effect.Effect[R, Fault, [][]dynamic.Object] {
				read[at] = rows
				return level(at + 1)
			})
	}
	return level(1)
}

// belowStatement reads table at's rows for the holders already read, in the
// order their lists hold them; nothing when there are no holders.
func belowStatement(spelling Spelling, laid []TableLayout, at int, read [][]dynamic.Object) (Statement, bool) {
	table := laid[at]
	holders := read[holderOf(laid, at)]
	keys := make([]Expr[bool], 0, len(holders))
	seen := map[string]bool{}
	for _, holder := range holders {
		value, _ := holder.Member(table.Parent.Target)
		if identity := canonical(value); !seen[identity] {
			seen[identity] = true
			keys = append(keys, Expr[bool]{node: node{kind: aValue, value: value}})
		}
	}
	if len(keys) == 0 {
		return Statement{}, false
	}
	source := From(table.Name, table.Columns...)
	query := SelectQuery{
		Select: source.Columns(), From: source,
		Where: In(Expr[bool]{node: node{kind: aColumn, source: table.Name, name: table.Parent.Column}}, keys...),
	}
	if table.Position != "" {
		query.OrderBy = []Ordering{{term: node{kind: aColumn, source: table.Name, name: table.Position}}}
	}
	return query.Statement(spelling), true
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
					if table.Parent != nil && (field.Name == table.Parent.Column || field.Name == table.Position) {
						continue
					}
					object.Fields = append(object.Fields, field)
				}
				for below := at + 1; below < len(laid); below++ {
					if laid[below].Parent == nil || laid[below].Parent.Table != table.Name {
						continue
					}
					link := laid[below].Parent
					identity, _ := row.Member(link.Target)
					held := built[below][canonical(identity)]
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
				holder, _ := row.Member(table.Parent.Column)
				built[at][canonical(holder)] = append(built[at][canonical(holder)], value)
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

// takeApart is an aggregate's value as each table's rows: rows[i] are table
// i's, every column named, a holder's key and a list's position filled in.
func takeApart(laid []TableLayout, root dynamic.Object) [][]dynamic.Object {
	rows := make([][]dynamic.Object, len(laid))
	entities := make([][]dynamic.Object, len(laid))
	rows[0], entities[0] = []dynamic.Object{columnsOf(laid[0], root, nil)}, []dynamic.Object{root}
	for at := 1; at < len(laid); at++ {
		table := laid[at]
		for _, holder := range entities[holderOf(laid, at)] {
			identity, _ := holder.Member(table.Parent.Target)
			member, _ := holder.Member(table.Parent.Field)
			var elements []dynamic.Value
			switch held := member.(type) {
			case dynamic.List:
				elements = held.Elements
			case dynamic.Object:
				elements = []dynamic.Value{held}
			}
			for position, element := range elements {
				fixed := map[string]dynamic.Value{table.Parent.Column: identity}
				if table.Position != "" {
					fixed[table.Position] = dynamic.OfInteger(int64(position))
				}
				if table.Parent.Element != "" {
					fixed[table.Parent.Element] = element
					rows[at] = append(rows[at], columnsOf(table, dynamic.Object{}, fixed))
					continue
				}
				entity, _ := element.(dynamic.Object)
				entities[at] = append(entities[at], entity)
				rows[at] = append(rows[at], columnsOf(table, entity, fixed))
			}
		}
	}
	return rows
}

// columnsOf is a table's row from an entity's members and the columns the
// table fixes, every column present -- an absent member as null.
func columnsOf(table TableLayout, entity dynamic.Object, fixed map[string]dynamic.Value) dynamic.Object {
	row := dynamic.Object{Fields: make([]dynamic.Field, 0, len(table.Columns))}
	for _, column := range table.Columns {
		value, present := fixed[column.Name]
		if !present {
			value, present = entity.Member(column.Name)
		}
		if !present {
			value = dynamic.Absent{}
		}
		row.Fields = append(row.Fields, dynamic.Field{Name: column.Name, Value: value})
	}
	return row
}

// keyOf is a row's key, as one text to compare by.
func keyOf(table TableLayout, row dynamic.Object) string {
	parts := make([]string, 0, len(table.Key))
	for _, column := range table.Key {
		value, _ := row.Member(column)
		parts = append(parts, canonical(value))
	}
	return strings.Join(parts, "\x00")
}

// sameRow says two rows hold the same values, as the database would keep
// them. A difference it cannot see past -- a driver's text for a moment --
// only writes a row again.
func sameRow(left dynamic.Object, right dynamic.Object) bool {
	if len(left.Fields) != len(right.Fields) {
		return false
	}
	for _, field := range left.Fields {
		other, present := right.Member(field.Name)
		if !present || canonical(field.Value) != canonical(other) {
			return false
		}
	}
	return true
}

// canonical is a value as a key compares it: text whether a driver gave text
// or bytes, a truth as the number SQLite and MySQL keep, a moment in UTC.
func canonical(value dynamic.Value) string {
	switch held := value.(type) {
	case dynamic.Text:
		return "t" + held.Value
	case dynamic.Bytes:
		return "t" + string(held.Value)
	case dynamic.Integer:
		return "n" + strconv.FormatInt(held.Value, 10)
	case dynamic.Number:
		if held.Value == float64(int64(held.Value)) {
			return "n" + strconv.FormatInt(int64(held.Value), 10)
		}
		return "n" + strconv.FormatFloat(held.Value, 'g', -1, 64)
	case dynamic.Boolean:
		if held.Value {
			return "n1"
		}
		return "n0"
	case dynamic.Timestamp:
		return "m" + held.Value.UTC().Format(time.RFC3339Nano)
	case dynamic.Absent, nil:
		return "z"
	default:
		// A document: two are the same when they print the same, and one a
		// driver read back as text is written again.
		return "d" + fmt.Sprint(held)
	}
}
