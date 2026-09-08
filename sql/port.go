package sql

// What this package needs of a database, and no more.
//
// Three operations and a cursor. database/sql is itself an abstraction over
// drivers, so a second one earns its place only because pgx's native interface
// is not database/sql: an adapter for either fits behind this, and an
// application that depends on it depends on neither.

import (
	"context"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// Querying is a database, or a transaction on one. The two answer the same
// operations, which is what lets a repository be written once and run either
// way.
type Querying interface {
	// Query runs a statement that returns rows.
	Query(ctx context.Context, statement string, arguments []dynamic.Value) (Cursor, error)
	// Execute runs a statement that returns none.
	Execute(ctx context.Context, statement string, arguments []dynamic.Value) (Outcome, error)
}

// Beginning is a database that can start a transaction. A transaction cannot,
// which is why this is separate: nested transactions are a different feature
// with different semantics, and a type that offered one it does not have would
// be lying.
type Beginning interface {
	Begin(ctx context.Context) (Transaction, error)
}

// Cursor walks a result set one row at a time, so a large one need not be held.
type Cursor interface {
	// Next advances to the next row, reporting whether there was one.
	Next() bool
	// Row is the current row, as the named values it is.
	Row() (dynamic.Object, error)
	// Err is why the walk stopped, if it was not the end.
	Err() error
	// Close releases the rows. It is called by the stream that owns the cursor.
	Close() error
}

// Outcome is what a statement that returns no rows has to say for itself.
type Outcome struct {
	// Changed is how many rows the statement affected, where the driver knows.
	Changed int64
}

// Transaction is work that will be committed or rolled back as a whole.
type Transaction interface {
	Querying
	Commit() error
	Rollback() error
}
