package ddl

// Indexes a query asks for, and what a server says it does with a statement.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mbauer83/effect-golang-sql/sql"
)

// CreateIndexes are the statements that make the indexes a query is read by
// on a table: a listing's, most often.
//
// One statement for each index declared. An index's included columns are
// Postgres's INCLUDE: carried in the index, not ordered or compared by it.
// MySQL and SQLite have none. There the columns follow the others of an index
// that is not unique, which answers the same queries and changes nothing else.
// A unique index with included columns is refused there: following the others
// would make them part of what is unique -- a weaker rule -- and leaving them
// out would not be the index declared.
func CreateIndexes(dialect Dialect, table string, indexes ...sql.Index) ([]string, error) {
	statements := make([]string, 0, len(indexes))
	for _, index := range indexes {
		if index.Unique && len(index.Include) > 0 && dialect.Name() != Postgres.Name() {
			return nil, fmt.Errorf("%w: %s has no INCLUDE, so the unique index %q on %s cannot carry %s "+
				"without their becoming part of what is unique; declare it without them, and a second, "+
				"non-unique index on %s for the queries that read them",
				ErrUniqueInclude, dialect.Name(), index.Name, strings.Join(index.Columns, ", "),
				strings.Join(index.Include, ", "), strings.Join(append(append([]string(nil), index.Columns...), index.Include...), ", "))
		}
		statements = append(statements, createIndex(dialect, table, index)...)
	}
	return statements, nil
}

// ErrUniqueInclude is a unique index with included columns, declared for a
// dialect that has no INCLUDE.
var ErrUniqueInclude = errors.New("ddl: a unique index with included columns needs INCLUDE")

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

// DropIndexes are the statements that remove indexes a query is no longer read
// by: the inverse of CreateIndexes, for the migration that stops needing them.
func DropIndexes(dialect Dialect, table string, indexes ...sql.Index) []string {
	statements := make([]string, 0, len(indexes))
	for _, index := range indexes {
		statements = append(statements, "DROP INDEX "+dialect.QuoteIdentifier(index.Name)+onTable(dialect, table))
	}
	return statements
}
