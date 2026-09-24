package ddl

// The statements the tables are.

import (
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Create is the statements that make an aggregate's tables, in the order they
// have to be run: a parent before the children that reference it.
//
// Statements rather than one string, because a caller runs them one at a time
// through a port that takes one statement -- and because only Postgres would
// let the whole lot be one transaction anyway.
//
// Not idempotent, and deliberately not. "create table if not exists" would
// make the tables skippable and leave the indexes failing on a second run,
// because MySQL has no such clause for an index -- so a schema that half
// re-ran would be worse than one that did not. These statements make a schema
// once. Changing an existing one is a migration, which the plan's section 7
// records and which is not this.
func Create(dialect Dialect, node structure.Node) ([]string, error) {
	tables, err := Tables(dialect, node)
	if err != nil {
		return nil, err
	}
	return renderCreate(dialect, tables), nil
}

// renderCreate is the statements that make a set of tables and their indexes, in
// the order they have to be run.
func renderCreate(dialect Dialect, tables []Table) []string {
	statements := make([]string, 0, len(tables)*2)
	for _, table := range tables {
		statements = append(statements, table.Create(dialect))
		for _, index := range table.Indexes {
			statements = append(statements, index.Create(dialect, table.Name))
		}
	}
	return statements
}

// Drop is the statements that remove them, in the order they have to be run:
// the children before the parent they reference.
func Drop(dialect Dialect, node structure.Node) ([]string, error) {
	tables, err := Tables(dialect, node)
	if err != nil {
		return nil, err
	}
	statements := make([]string, 0, len(tables))
	for at := len(tables) - 1; at >= 0; at-- {
		statements = append(statements,
			"DROP TABLE IF EXISTS "+dialect.QuoteIdentifier(tables[at].Name))
	}
	return statements, nil
}

// Create is the statement that makes one table.
func (table Table) Create(dialect Dialect) string {
	out := &strings.Builder{}
	comment(out, "", table.Comment)
	out.WriteString("CREATE TABLE " + dialect.QuoteIdentifier(table.Name) + " (\n")

	parts := make([]string, 0, len(table.Columns)+1+len(table.ForeignKeys))
	for _, column := range table.Columns {
		parts = append(parts, column.definition(dialect, table.Name))
	}
	if len(table.PrimaryKey) > 0 {
		parts = append(parts, "  PRIMARY KEY ("+quoteAll(dialect, table.PrimaryKey)+")")
	}
	for _, key := range table.ForeignKeys {
		parts = append(parts, key.definition(dialect))
	}

	out.WriteString(strings.Join(parts, ",\n"))
	out.WriteString("\n)" + dialect.TableSuffix())
	return out.String()
}

func (column Column) definition(dialect Dialect, table string) string {
	out := &strings.Builder{}
	comment(out, "  ", column.Comment)
	for _, note := range column.Notes {
		out.WriteString("  -- " + note + "\n")
	}

	out.WriteString("  " + dialect.QuoteIdentifier(column.Name) + " " + column.Type)
	// A generated key already says not null in the dialect's own spelling, so
	// saying it again would be a syntax error in one of them.
	if !column.Nullable && !column.Identity {
		out.WriteString(" NOT NULL")
	}
	if column.Default != "" {
		out.WriteString(" DEFAULT " + column.Default)
	}
	for _, check := range column.Checks {
		out.WriteString("\n    " + check.definition(dialect, table, column.Name))
	}
	return out.String()
}

func (key ForeignKey) definition(dialect Dialect) string {
	out := "  FOREIGN KEY (" + quoteAll(dialect, key.Columns) + ") REFERENCES " +
		dialect.QuoteIdentifier(key.Table) + " (" + quoteAll(dialect, key.Targets) + ")"
	switch key.OnDelete {
	case structure.Cascade:
		out += " ON DELETE CASCADE"
	case structure.SetNull:
		out += " ON DELETE SET NULL"
	}
	return out
}

// Create is the statement that makes one index.
//
// Separate from the table, because MySQL will take an index inside a create and
// Postgres will not -- and one form both accept is better than two spellings of
// the same thing.
func (index Index) Create(dialect Dialect, table string) string {
	unique := ""
	if index.Unique {
		unique = "UNIQUE "
	}
	return "CREATE " + unique + "INDEX " + dialect.QuoteIdentifier(index.Name) +
		" ON " + dialect.QuoteIdentifier(table) + " (" + quoteAll(dialect, index.Columns) + ")"
}

func quoteAll(dialect Dialect, names []string) string {
	identifiers := make([]string, 0, len(names))
	for _, name := range names {
		identifiers = append(identifiers, dialect.QuoteIdentifier(name))
	}
	return strings.Join(identifiers, ", ")
}

func comment(out *strings.Builder, indent string, doc string) {
	if doc == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(doc), "\n") {
		out.WriteString(indent + "-- " + strings.TrimSpace(line) + "\n")
	}
}

// columnClause writes a column being added or restated, without the comments.
//
// A comment belongs above a column in a create statement, where somebody reads
// the schema. In an alter it would be a comment in a migration nobody reads
// twice, so what is written here is the definition alone.
func columnClause(dialect Dialect, column Column) string {
	out := dialect.QuoteIdentifier(column.Name) + " " + column.Type
	if !column.Nullable && !column.Identity {
		out += " NOT NULL"
	}
	if column.Default != "" {
		out += " DEFAULT " + column.Default
	}
	return out
}

// addedColumnClause is a column being added, with its checks: in a column's
// own definition is the one place SQLite takes a check after a table exists.
// A column being restated keeps the checks it had, so columnClause writes
// none.
func addedColumnClause(dialect Dialect, table string, column Column) string {
	out := columnClause(dialect, column)
	for _, check := range column.Checks {
		out += " " + check.definition(dialect, table, column.Name)
	}
	return out
}
