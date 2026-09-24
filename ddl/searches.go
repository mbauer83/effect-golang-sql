package ddl

// What a listing's searches need of a table, and how each dialect finds rows
// by text with it.
//
// Each kind is served by an index and never by reading every row:
//
//   - a prefix by a column generated from the text lowercased, indexed, and
//     compared in byte order so a range of it is the rows beginning alike;
//   - full text by Postgres's tsvector under a GIN index, MySQL's FULLTEXT
//     index, or an FTS5 table SQLite keeps with triggers;
//   - a substring by Postgres's trigram index, which the other two do not have.
//
// A search's name names what it makes: table_name_search for its index, or
// SQLite's FTS5 table.

import (
	"fmt"
	"strings"

	"github.com/mbauer83/effect-golang-sql/sql"
)

// CreateSearches are the statements that make what a listing's searches of a
// table need: columns generated from the ones searched, and indexes on them.
// A table that already holds rows is searchable once they have run.
//
// It refuses a kind the dialect has no index for.
func CreateSearches(dialect Dialect, table string, searches ...sql.Search) ([]string, error) {
	var statements []string
	for _, search := range searches {
		made, err := createSearch(dialect, table, search)
		if err != nil {
			return nil, err
		}
		statements = append(statements, made...)
	}
	return statements, nil
}

func createSearch(dialect Dialect, table string, search sql.Search) ([]string, error) {
	quote := dialect.QuoteIdentifier
	index := quote(searchName(table, search))
	on := " ON " + quote(table)
	switch {
	case search.Kind == sql.PrefixMatch:
		column := search.Columns[0]
		folded := quote(foldedColumn(column))
		generated := alterTable(dialect, table) + "ADD COLUMN " + folded + " "
		switch dialect.Name() {
		case Postgres.Name():
			return []string{
				generated + `TEXT COLLATE "C" GENERATED ALWAYS AS (LOWER(` + quote(column) + ")) STORED",
				"CREATE INDEX " + index + on + " (" + folded + ")",
			}, nil
		case MySQL.Name():
			// A text column is indexed by a prefix of it: a range of those
			// bytes is still read from the index, and the rest of each
			// candidate from its row.
			return []string{
				generated + "TEXT COLLATE utf8mb4_bin GENERATED ALWAYS AS (LOWER(" + quote(column) + ")) STORED",
				"CREATE INDEX " + index + on + " (" + folded + "(191))",
			}, nil
		default:
			// SQLite adds only a virtual generated column to a table, and
			// indexes it all the same.
			return []string{
				generated + "TEXT GENERATED ALWAYS AS (LOWER(" + quote(column) + ")) VIRTUAL",
				"CREATE INDEX " + index + on + " (" + folded + ")",
			}, nil
		}
	case search.Kind == sql.FullTextMatch && dialect.Name() == Postgres.Name():
		document := quote(documentColumn(search))
		return []string{
			alterTable(dialect, table) + "ADD COLUMN " + document + " TSVECTOR GENERATED ALWAYS AS (TO_TSVECTOR(" +
				dialect.QuoteLiteral(search.Language) + ", " + joinedText(dialect, search.Columns) + ")) STORED",
			"CREATE INDEX " + index + on + " USING GIN (" + document + ")",
		}, nil
	case search.Kind == sql.FullTextMatch && dialect.Name() == MySQL.Name():
		return []string{"CREATE FULLTEXT INDEX " + index + on + " (" + quoteAll(dialect, search.Columns) + ")"}, nil
	case search.Kind == sql.FullTextMatch && dialect.Name() == SQLite.Name():
		return textTable(dialect, table, search), nil
	case search.Kind == sql.SubstringMatch && dialect.Name() == Postgres.Name():
		return []string{
			"CREATE EXTENSION IF NOT EXISTS pg_trgm",
			"CREATE INDEX " + index + on + " USING GIN (" + quote(search.Columns[0]) + " gin_trgm_ops)",
		}, nil
	default:
		return nil, fmt.Errorf("ddl: %s has no index for a %s search, and %q is one",
			dialect.Name(), search.Kind, search.Name)
	}
}

// DropSearches are the statements that remove what searches of a table
// needed: the inverse of CreateSearches, for the migration that stops offering
// them. The table is as it was before they were made.
func DropSearches(dialect Dialect, table string, searches ...sql.Search) []string {
	var statements []string
	for _, search := range searches {
		name := searchName(table, search)
		dropIndex := "DROP INDEX " + dialect.QuoteIdentifier(name) + onTable(dialect, table)
		dropColumn := func(column string) string {
			return alterTable(dialect, table) + "DROP COLUMN " + dialect.QuoteIdentifier(column)
		}
		switch {
		case search.Kind == sql.PrefixMatch:
			statements = append(statements, dropIndex, dropColumn(foldedColumn(search.Columns[0])))
		case search.Kind == sql.FullTextMatch && dialect.Name() == Postgres.Name():
			statements = append(statements, dropIndex, dropColumn(documentColumn(search)))
		case search.Kind == sql.FullTextMatch && dialect.Name() == SQLite.Name():
			for _, event := range []string{"insert", "delete", "update"} {
				statements = append(statements, "DROP TRIGGER "+dialect.QuoteIdentifier(name+"_"+event))
			}
			statements = append(statements, "DROP TABLE "+dialect.QuoteIdentifier(name))
		default:
			statements = append(statements, dropIndex)
		}
	}
	return statements
}

// textTable is SQLite's FTS5 table of a table's columns, holding no copy of
// them, and the triggers that keep it as the table changes -- and, for rows
// already there, the rebuild that indexes them.
func textTable(dialect Dialect, table string, search sql.Search) []string {
	quote := dialect.QuoteIdentifier
	name := searchName(table, search)
	text := quote(name)
	columns := quoteAll(dialect, search.Columns)
	values := func(row string) string {
		qualified := make([]string, 0, len(search.Columns))
		for _, column := range search.Columns {
			qualified = append(qualified, row+"."+quote(column))
		}
		return strings.Join(qualified, ", ")
	}
	add := "INSERT INTO " + text + " (rowid, " + columns + ") VALUES (new.rowid, " + values("new") + ");"
	remove := "INSERT INTO " + text + " (" + text + ", rowid, " + columns + ") VALUES ('delete', old.rowid, " + values("old") + ");"
	trigger := func(event string, body string) string {
		return "CREATE TRIGGER " + quote(name+"_"+strings.ToLower(event)) + " AFTER " + event + " ON " + quote(table) +
			" BEGIN " + body + " END"
	}
	return []string{
		"CREATE VIRTUAL TABLE " + text + " USING fts5(" + columns + ", content=" + dialect.QuoteLiteral(table) + ")",
		trigger("INSERT", add),
		trigger("DELETE", remove),
		trigger("UPDATE", remove+" "+add),
		"INSERT INTO " + text + " (" + text + ") VALUES ('rebuild')",
	}
}

// joinedText is the columns as one text, a missing one as nothing.
func joinedText(dialect Dialect, columns []string) string {
	pieces := make([]string, 0, len(columns))
	for _, column := range columns {
		pieces = append(pieces, "COALESCE("+dialect.QuoteIdentifier(column)+", '')")
	}
	return strings.Join(pieces, " || ' ' || ")
}

// searchName is what a search's index -- or SQLite's FTS5 table -- is called.
func searchName(table string, search sql.Search) string {
	return table + "_" + search.Name + "_search"
}

// foldedColumn is the column a prefix search compares: the text lowercased.
func foldedColumn(column string) string { return column + "_lower" }

// documentColumn is the column Postgres keeps a full-text search's words in.
func documentColumn(search sql.Search) string { return search.Name + "_search" }

// searchSyntax is how a dialect writes a search a query asks for.
func searchSyntax(dialect Dialect, search sql.Search, table string, qualifier string) (sql.Syntax, bool) {
	quote := dialect.QuoteIdentifier
	column := func(name string) string { return quote(qualifier) + "." + quote(name) }
	switch {
	case search.Kind == sql.PrefixMatch:
		folded := column(foldedColumn(search.Columns[0]))
		return sql.Phrase("("+folded+" >= LOWER(", ") AND "+folded+" < LOWER(", "))"), true
	case search.Kind == sql.FullTextMatch && dialect.Name() == Postgres.Name():
		return sql.Phrase("("+column(documentColumn(search))+" @@ PLAINTO_TSQUERY("+
			dialect.QuoteLiteral(search.Language)+", ", "))"), true
	case search.Kind == sql.FullTextMatch && dialect.Name() == MySQL.Name():
		// Boolean mode with every word required, as the other two ask; natural
		// language mode would rank rows holding any of them.
		searched := make([]string, 0, len(search.Columns))
		for _, name := range search.Columns {
			searched = append(searched, column(name))
		}
		return sql.Phrase("MATCH ("+strings.Join(searched, ", ")+") AGAINST (CONCAT('+', REPLACE(",
			", ' ', ' +')) IN BOOLEAN MODE)"), true
	case search.Kind == sql.FullTextMatch && dialect.Name() == SQLite.Name():
		text := quote(searchName(table, search))
		return sql.Phrase(column("rowid")+" IN (SELECT rowid FROM "+text+" WHERE "+text+" MATCH ", ")"), true
	case search.Kind == sql.SubstringMatch && dialect.Name() == Postgres.Name():
		return sql.Phrase(column(search.Columns[0])+" ILIKE ", ""), true
	default:
		return nil, false
	}
}

// SearchSyntax is how Postgres finds rows by text.
func (dialect postgres) SearchSyntax(search sql.Search, table string, qualifier string) (sql.Syntax, bool) {
	return searchSyntax(dialect, search, table, qualifier)
}

// SearchSyntax is how MySQL finds rows by text.
func (dialect mysql) SearchSyntax(search sql.Search, table string, qualifier string) (sql.Syntax, bool) {
	return searchSyntax(dialect, search, table, qualifier)
}

// SearchSyntax is how SQLite finds rows by text.
func (dialect sqlite) SearchSyntax(search sql.Search, table string, qualifier string) (sql.Syntax, bool) {
	return searchSyntax(dialect, search, table, qualifier)
}
