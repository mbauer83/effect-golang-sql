package migrate

// How several instances agree on which of them migrates.
//
// Deliberately small. A lock here is one statement to take it and one to give
// it back, taken on the same connection the migration runs on -- which is what
// a transaction gives -- and nothing else. Leader election, leases and
// heartbeats are somebody else's problem and a much larger one; what this
// closes is the ordinary case of every replica starting at once and all of them
// trying to alter the same table.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// Lock is a database's own advisory lock.
//
// Advisory, so it guards this migration against another instance of the same
// migration and nothing else: it does not stop somebody altering the table by
// hand, and it is not a replacement for one process being responsible.
// A lock is composed through the dialect rather than written as text, for the
// reason everything else here is: the key is a bound value, and how a bound
// value is spelled is the server's. A lock statement carrying a question mark
// is refused by Postgres -- and by nothing before it, since the function name
// is right and only the placeholder is wrong.
type Lock interface {
	// Take is the statement that waits for the lock.
	Take(spelling sql.Spelling, key string) sql.Statement
	// Release is the statement that gives it back, and a statement with no text
	// when the database releases it on its own.
	Release(spelling sql.Spelling, key string) sql.Statement
}

// PostgresAdvisory takes a transaction-scoped advisory lock.
//
// Transaction-scoped, so Postgres frees it when the transaction ends however it
// ends -- which is exactly what is wanted and one fewer thing to get wrong than
// releasing it by hand. The key is hashed to the bigint the function takes,
// because Postgres's advisory locks are numbered and this one is named.
var PostgresAdvisory Lock = postgresAdvisory{}

type postgresAdvisory struct{}

func (postgresAdvisory) Take(spelling sql.Spelling, key string) sql.Statement {
	return sql.Compose(spelling,
		sql.Text("select pg_advisory_xact_lock("),
		sql.Bind(dynamic.OfInteger(lockNumber(key))),
		sql.Text(")"))
}

// Release says nothing: the transaction ending is what frees it.
func (postgresAdvisory) Release(sql.Spelling, string) sql.Statement {
	return sql.Statement{}
}

// MySQLNamed takes a named lock.
//
// Session-scoped rather than transaction-scoped, because MySQL has no
// transaction-scoped lock -- which turns out to be what is needed here anyway:
// MySQL's DDL commits as it goes, so a transaction-scoped lock would be freed
// by the first ALTER and the next instance could walk in behind it.
var MySQLNamed Lock = mysqlNamed{}

type mysqlNamed struct{}

// Take waits up to ten seconds, and a caller that waited longer than that for a
// migration to start is a caller whose deployment has gone wrong in some other
// way. It returns zero rather than failing on a timeout, which the migration
// then reports as not having got the lock.
func (mysqlNamed) Take(spelling sql.Spelling, key string) sql.Statement {
	return sql.Compose(spelling,
		sql.Text("select get_lock("),
		sql.Bind(dynamic.OfText(key)),
		sql.Text(", 10)"))
}

func (mysqlNamed) Release(spelling sql.Spelling, key string) sql.Statement {
	return sql.Compose(spelling,
		sql.Text("select release_lock("),
		sql.Bind(dynamic.OfText(key)),
		sql.Text(")"))
}

// lockNumber is the bigint a name hashes to, for a database whose advisory
// locks are numbered rather than named.
//
// FNV-1a, written out rather than taken from hash/fnv because the value has to
// be stable across releases of this package: two instances that hashed the same
// name differently would take two different locks and both proceed.
func lockNumber(key string) int64 {
	var hash uint64 = 14695981039346656037
	for at := 0; at < len(key); at++ {
		hash ^= uint64(key[at])
		hash *= 1099511628211
	}
	// Into the positive half, because the function takes a signed bigint and a
	// negative key is legal but harder to recognise in pg_locks.
	return int64(hash >> 1)
}
