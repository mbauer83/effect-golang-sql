package ddl

// Indexes a query asks for, and what a server says it does with a statement.

import (
	"github.com/mbauer83/effect-golang-sql/sql"
)

// CreateIndexes are the statements that make the indexes a query is read by
// on a table: a listing's, most often.
//
// An index's included columns are Postgres's INCLUDE. MySQL and SQLite have
// none, so there they follow the key -- the index then answers the same
// queries alone, and orders by more than asked. For a unique index that would
// change what is unique, so there it is two: the unique index on its key, and
// one on the key and the included columns for the queries they are for.
func CreateIndexes(dialect Dialect, table string, indexes ...sql.Index) []string {
	statements := make([]string, 0, len(indexes))
	for _, index := range indexes {
		statements = append(statements, createIndex(dialect, table, index)...)
	}
	return statements
}

func createIndex(dialect Dialect, table string, index sql.Index) []string {
	create := func(name string, unique bool, columns []string, include string) string {
		kind := "INDEX "
		if unique {
			kind = "UNIQUE INDEX "
		}
		return "CREATE " + kind + dialect.QuoteIdentifier(name) + " ON " + dialect.QuoteIdentifier(table) +
			" (" + quoteAll(dialect, columns) + ")" + include
	}
	switch {
	case len(index.Include) == 0:
		return []string{create(index.Name, index.Unique, index.Columns, "")}
	case dialect.Name() == Postgres.Name():
		return []string{create(index.Name, index.Unique, index.Columns,
			" INCLUDE ("+quoteAll(dialect, index.Include)+")")}
	case index.Unique:
		return []string{
			create(index.Name, true, index.Columns, ""),
			create(index.Name+"_with_included", false, append(append([]string(nil), index.Columns...), index.Include...), ""),
		}
	default:
		return []string{create(index.Name, false, append(append([]string(nil), index.Columns...), index.Include...), "")}
	}
}

// Explain is a statement asked about rather than run: the plan the server
// would read it by, which a test reads to say that a declared query uses an
// index and does not scan its table.
func Explain(dialect Dialect, statement sql.Statement) sql.Statement {
	if dialect.Name() == SQLite.Name() {
		return statement.WithPrefix("EXPLAIN QUERY PLAN ")
	}
	return statement.WithPrefix("EXPLAIN ")
}
