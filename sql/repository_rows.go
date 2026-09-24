package sql

// An aggregate's value taken apart into each table's rows, and rows compared
// as the database keeps them.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// takeApart is an aggregate's value as each table's rows: rows[i] are table
// i's, every column named, a holder's key and a list's position filled in.
func takeApart(laid []TableLayout, root dynamic.Object) [][]dynamic.Object {
	rows := make([][]dynamic.Object, len(laid))
	entities := make([][]dynamic.Object, len(laid))
	rows[0], entities[0] = []dynamic.Object{columnsOf(laid[0], root, nil)}, []dynamic.Object{root}
	for at := 1; at < len(laid); at++ {
		table := laid[at]
		above := holderOf(laid, at)
		for index, holder := range entities[above] {
			fixed := map[string]dynamic.Value{}
			for place, column := range table.Parent.Columns {
				fixed[column], _ = rows[above][index].Member(table.Parent.Targets[place])
			}
			member, _ := holder.Member(table.Parent.Field)
			var elements []dynamic.Value
			switch held := member.(type) {
			case dynamic.List:
				elements = held.Elements
			case dynamic.Object:
				elements = []dynamic.Value{held}
			}
			for position, element := range elements {
				row := map[string]dynamic.Value{}
				for column, value := range fixed {
					row[column] = value
				}
				if table.Position != "" {
					row[table.Position] = dynamic.OfInteger(int64(position) * positionGap)
				}
				if table.Parent.Element != "" {
					row[table.Parent.Element] = element
					rows[at] = append(rows[at], columnsOf(table, dynamic.Object{}, row))
					continue
				}
				entity, _ := element.(dynamic.Object)
				entities[at] = append(entities[at], entity)
				rows[at] = append(rows[at], columnsOf(table, entity, row))
			}
		}
	}
	return rows
}

// tupleOf is the values of those columns of a row, as one text to compare by.
func tupleOf(row dynamic.Object, columns []string) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		value, _ := row.Member(column)
		parts = append(parts, canonical(value))
	}
	return strings.Join(parts, "\x00")
}

func holds(names []string, name string) bool {
	for _, held := range names {
		if held == name {
			return true
		}
	}
	return false
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
func keyOf(table TableLayout, row dynamic.Object) string { return tupleOf(row, table.Key) }

// sameRow says two rows hold the same values, as the database would keep
// them. A difference it cannot see past -- a driver's text for a moment --
// only writes a row again.
// A column ignored is not compared.
func sameRow(left dynamic.Object, right dynamic.Object, ignored string) bool {
	if len(left.Fields) != len(right.Fields) {
		return false
	}
	for _, field := range left.Fields {
		if field.Name == ignored {
			continue
		}
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
