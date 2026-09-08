// Package sql runs statements against a database and decodes rows through a
// Schema.
//
// A row is a set of named values, which is an object -- so a row is decoded by
// the same Schema that decodes a request body, and this package needs no
// description of its own. That is the whole point of the schema layer exposing
// its structure and of the universal representation existing: one description
// serves the wire and the table.
//
// It follows the shape every transport here follows:
//
//	a connection is a scoped resource
//	a result set is a Stream, so a large one need not be held
//	rows are decoded by a Schema
//	cancellation reaches the driver through the context
//
// A transaction is a scoped resource too: it commits when the work succeeds and
// rolls back when it fails or is interrupted, which is AcquireRelease and
// nothing new.
//
// The port is narrow on purpose. database/sql is itself an abstraction over
// drivers, and this is a second one only because pgx's native interface is not
// database/sql -- so an adapter for either fits behind the same three
// operations.
package sql
