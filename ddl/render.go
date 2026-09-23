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
			"drop table if exists "+dialect.QuoteIdentifier(tables[at].Name))
	}
	return statements, nil
}

// Create is the statement that makes one table.
func (table Table) Create(dialect Dialect) string {
	out := &strings.Builder{}
	comment(out, "", table.Doc)
	out.WriteString("create table " + dialect.QuoteIdentifier(table.Name) + " (\n")

	parts := make([]string, 0, len(table.Columns)+1+len(table.ForeignKeys))
	for _, column := range table.Columns {
		parts = append(parts, column.definition(dialect))
	}
	if len(table.PrimaryKey) > 0 {
		parts = append(parts, "  primary key ("+quoteAll(dialect, table.PrimaryKey)+")")
	}
	for _, key := range table.ForeignKeys {
		parts = append(parts, key.definition(dialect))
	}

	out.WriteString(strings.Join(parts, ",\n"))
	out.WriteString("\n)" + dialect.TableSuffix())
	return out.String()
}

func (column Column) definition(dialect Dialect) string {
	out := &strings.Builder{}
	comment(out, "  ", column.Doc)
	for _, note := range column.Notes {
		out.WriteString("  -- " + note + "\n")
	}

	out.WriteString("  " + dialect.QuoteIdentifier(column.Name) + " " + column.Type)
	// A generated key already says not null in the dialect's own spelling, so
	// saying it again would be a syntax error in one of them.
	if !column.Nullable && !column.Identity {
		out.WriteString(" not null")
	}
	if column.Default != "" {
		out.WriteString(" default " + column.Default)
	}
	return out.String()
}

func (key ForeignKey) definition(dialect Dialect) string {
	out := "  foreign key (" + quoteAll(dialect, key.Columns) + ") references " +
		dialect.QuoteIdentifier(key.Table) + " (" + quoteAll(dialect, key.Targets) + ")"
	if key.Cascade {
		out += " on delete cascade"
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
		unique = "unique "
	}
	return "create " + unique + "index " + dialect.QuoteIdentifier(index.Name) +
		" on " + dialect.QuoteIdentifier(table) + " (" + quoteAll(dialect, index.Columns) + ")"
}

func quoteAll(dialect Dialect, names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, dialect.QuoteIdentifier(name))
	}
	return strings.Join(quoted, ", ")
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
		out += " not null"
	}
	if column.Default != "" {
		out += " default " + column.Default
	}
	return out
}
