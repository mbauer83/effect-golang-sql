package ddl

// A column renamed, and the checks named after it.

import (
	"fmt"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

// renameColumn writes a column being renamed, and writes nothing for a relation.
//
// A relation's field name is not in the database at all: the child table is
// named for the entity and its reference column for the parent, so the name the
// root holds it under appears nowhere. Renaming it changes the description and
// nothing else, which is worth saying rather than leaving a caller to wonder
// why no statement came out.
func renameColumn(
	dialect Dialect,
	root structure.Object,
	change evolve.Rename,
) ([]string, error) {
	field, found := findField(root, change.From)
	if !found {
		return nil, fmt.Errorf("%q: %w", change.From, errNoSuchField)
	}
	if _, related := structure.EntityBehind(field.Node); related {
		return nil, nil
	}
	rename := "RENAME COLUMN " + dialect.QuoteIdentifier(change.From) + " TO " + dialect.QuoteIdentifier(change.To)
	checks, _ := checksFor(dialect, change.From, field.Node, "")
	if len(checks) == 0 {
		return []string{alterTable(dialect, root.Name) + rename}, nil
	}
	return renameWithChecks(dialect, root.Name, field.Node, change, rename), nil
}

// renameWithChecks renames a column its checks are on. A check is named after its
// column, so the names follow the rename: Postgres renames each constraint,
// and MySQL, which refuses to rename a column a check uses, drops the checks
// and makes them again in the same statement. SQLite can do neither to a
// constraint and rewrites the check's expression itself, so there the checks
// keep their names.
func renameWithChecks(dialect Dialect, table string, node structure.Node, change evolve.Rename, rename string) []string {
	before, _ := checksFor(dialect, change.From, node, "")
	after, _ := checksFor(dialect, change.To, node, "")
	switch dialect.Name() {
	case Postgres.Name():
		statements := []string{alterTable(dialect, table) + rename}
		for _, check := range before {
			statements = append(statements, alterTable(dialect, table)+"RENAME CONSTRAINT "+
				dialect.QuoteIdentifier(checkName(table, change.From, check.Rule))+" TO "+
				dialect.QuoteIdentifier(checkName(table, change.To, check.Rule)))
		}
		return statements
	case MySQL.Name():
		clauses := make([]string, 0, len(before)+len(after)+1)
		for _, check := range before {
			clauses = append(clauses, "DROP CHECK "+dialect.QuoteIdentifier(checkName(table, change.From, check.Rule)))
		}
		clauses = append(clauses, rename)
		for _, check := range after {
			clauses = append(clauses, "ADD "+check.definition(dialect, table, change.To))
		}
		return []string{alterTable(dialect, table) + strings.Join(clauses, ", ")}
	default:
		return []string{alterTable(dialect, table) + rename}
	}
}
