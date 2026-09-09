package ddl

// The clause that says "write this row, and if one with this key is already
// there, make it this one".
//
// Two of the three dialects took it from Postgres and spell it the same way,
// so the shape they share is written once here and each of them names the row
// it was offered. MySQL's is different enough to be its own answer and lives
// with MySQL.

import "strings"

// onConflictClause is the "on conflict … do update" clause the two dialects that
// took it from Postgres both write, with the offered row under whatever name
// each of them gives it.
//
// A key that is the whole row has nothing to assign, and the clause for that
// is "do nothing": a row already present and identical in every column is
// already what the insert was asking for.
func onConflictClause(dialect Dialect, key []string, columns []string, offered string) string {
	assignments := assignments(dialect, key, columns, offered+".")
	target := " (" + names(dialect, key) + ")"
	if len(assignments) == 0 {
		return "on conflict" + target + " do nothing"
	}
	return "on conflict" + target + " do update set " + strings.Join(assignments, ", ")
}

// assignments is one assignment per column that is not part of the key.
func assignments(dialect Dialect, key []string, columns []string, from string) []string {
	keyed := make(map[string]struct{}, len(key))
	for _, column := range key {
		keyed[column] = struct{}{}
	}
	assignments := make([]string, 0, len(columns))
	for _, column := range columns {
		if _, isKey := keyed[column]; isKey {
			continue
		}
		quoted := dialect.Quoted(column)
		assignments = append(assignments, quoted+" = "+from+quoted)
	}
	return assignments
}

// names is a list of identifiers as this dialect writes them.
func names(dialect Dialect, columns []string) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, dialect.Quoted(column))
	}
	return strings.Join(quoted, ", ")
}
