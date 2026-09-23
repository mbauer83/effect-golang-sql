package unit

// A database that records what was asked of it, and a cursor over rows a test
// wrote.
//
// The transaction tests are stated against these rather than against a real
// database, because a database's locking is not a reliable witness to a
// rollback: whether an un-rolled-back transaction blocks the next statement
// depends on the driver, the journal mode and the connection pool, and a test
// that passed for one of those reasons would not be testing this.

import (
	"context"
	"sync"

	stdsql "database/sql"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// recorder is a database whose transaction remembers what it was asked.
type recorder struct {
	mutex      sync.Mutex
	begun      int
	committed  int
	rolledBack int
	statements []string
	rows       []dynamic.Object
}

func (kept *recorder) Begin(context.Context) (sql.Transaction, error) {
	kept.mutex.Lock()
	defer kept.mutex.Unlock()
	kept.begun++
	return &recorderTransaction{kept: kept}, nil
}

func (kept *recorder) queries() []string {
	kept.mutex.Lock()
	defer kept.mutex.Unlock()
	return append([]string(nil), kept.statements...)
}

func (kept *recorder) counts() (int, int, int) {
	kept.mutex.Lock()
	defer kept.mutex.Unlock()
	return kept.begun, kept.committed, kept.rolledBack
}

type recorderTransaction struct {
	kept   *recorder
	closed bool
}

// Query answers with the rows the test put on the database, so that a read
// inside a transaction can be seen to have gone through the transaction.
func (open *recorderTransaction) Query(
	_ context.Context, statement string, _ []dynamic.Value,
) (sql.Cursor, error) {
	open.kept.mutex.Lock()
	defer open.kept.mutex.Unlock()
	open.kept.statements = append(open.kept.statements, statement)
	return &rowCursor{rows: open.kept.rows}, nil
}

func (open *recorderTransaction) Execute(
	_ context.Context, statement string, _ []dynamic.Value,
) (sql.Outcome, error) {
	open.kept.mutex.Lock()
	defer open.kept.mutex.Unlock()
	open.kept.statements = append(open.kept.statements, statement)
	return sql.Outcome{RowsAffected: 1}, nil
}

func (open *recorderTransaction) Commit() error {
	open.kept.mutex.Lock()
	defer open.kept.mutex.Unlock()
	open.kept.committed++
	open.closed = true
	return nil
}

// Rollback answers as a driver does: once the transaction is over, saying so.
func (open *recorderTransaction) Rollback() error {
	open.kept.mutex.Lock()
	defer open.kept.mutex.Unlock()
	if open.closed {
		return stdsql.ErrTxDone
	}
	open.kept.rolledBack++
	open.closed = true
	return nil
}

// rowCursor walks rows a test wrote, which is all a cursor is.
type rowCursor struct {
	rows []dynamic.Object
	at   int
}

func (cursor *rowCursor) Next() bool {
	cursor.at++
	return cursor.at <= len(cursor.rows)
}

func (cursor *rowCursor) Row() (dynamic.Object, error) { return cursor.rows[cursor.at-1], nil }
func (cursor *rowCursor) Err() error                   { return nil }
func (cursor *rowCursor) Close() error                 { return nil }

// tally is what one row of the ledger says.
type tally struct {
	Account string
	Balance int64
}

var talliedSchema = schema.Struct[tally]("Tallied",
	schema.FieldOf("account", schema.Text(),
		func(row tally) string { return row.Account },
		func(row *tally, account string) { row.Account = account }),
	schema.FieldOf("balance", schema.Int64(),
		func(row tally) int64 { return row.Balance },
		func(row *tally, balance int64) { row.Balance = balance }),
)
