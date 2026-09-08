package unit

// A driver that uses the whole driver.Value contract, and one that goes beyond
// it.
//
// sqlite is a real driver and the right one for the library's end-to-end tests,
// but it produces four of the seven kinds a driver may produce: it has no
// boolean of its own, and it never hands back a value outside the contract. The
// module has exactly one place that holds a value it cannot name, and this is
// what lets that place be exercised over its whole surface rather than over the
// part one driver happens to use.

import (
	"context"
	"errors"
	"io"
	"time"

	stdsql "database/sql"
	"database/sql/driver"
)

// eachKind is the statement this driver answers with one row of every kind.
const eachKind = "select every kind"

// beyondTheContract is the statement it answers with a value no driver is
// allowed to produce.
const beyondTheContract = "select something inexpressible"

// takingEveryKind is the statement whose arguments it keeps, so the binding
// direction of the boundary can be read back.
const takingEveryKind = "insert every kind"

var contractMoment = time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)

// errUnknownStatement is this driver's own error, so a test can ask whether it
// is still reachable after the boundary has wrapped it.
var errUnknownStatement = errors.New("this driver answers three statements and that is all")

func init() {
	stdsql.Register("contract", contractDriver{})
}

type contractDriver struct{}

// Open hands every connection the same slice, so a test reads what was bound
// whichever connection the pool chose.
func (contractDriver) Open(string) (driver.Conn, error) {
	return contractConn{bound: &lastBound}, nil
}

// lastBound is what the driver was last given. One statement per test and no
// concurrency here, so a single slot is enough and a map keyed by nothing in
// particular would only look more careful.
var lastBound []driver.Value

// contractConn answers queries directly. It prepares nothing, because a
// prepared statement would only be a second path to the same answer.
type contractConn struct {
	bound *[]driver.Value
}

func (contractConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("this driver answers queries directly")
}

func (contractConn) Close() error { return nil }

func (contractConn) Begin() (driver.Tx, error) {
	return nil, errors.New("this driver holds nothing to transact over")
}

func (contractConn) QueryContext(
	_ context.Context,
	query string,
	_ []driver.NamedValue,
) (driver.Rows, error) {
	switch query {
	case eachKind:
		return &contractRows{
			names: []string{"text", "integer", "number", "boolean", "bytes", "moment", "nothing"},
			row: []driver.Value{
				"held", int64(7), 1.5, true, []byte{1, 2}, contractMoment, nil,
			},
		}, nil
	case beyondTheContract:
		// int32 is not a driver.Value. Nothing stops a driver returning one,
		// which is why the boundary says so rather than guessing.
		return &contractRows{names: []string{"odd"}, row: []driver.Value{int32(7)}}, nil
	}
	return nil, errUnknownStatement
}

// ExecContext keeps what it was given. The values arrive as driver.Value,
// which is the contract's own vocabulary: what is checked is that each kind
// crossed as the type the contract names for it.
func (conn contractConn) ExecContext(
	_ context.Context,
	query string,
	arguments []driver.NamedValue,
) (driver.Result, error) {
	if query != takingEveryKind {
		return nil, errUnknownStatement
	}
	*conn.bound = (*conn.bound)[:0]
	for _, argument := range arguments {
		*conn.bound = append(*conn.bound, argument.Value)
	}
	return contractResult{}, nil
}

// contractResult knows nothing, which a driver is allowed not to.
type contractResult struct{}

func (contractResult) LastInsertId() (int64, error) {
	return 0, errors.New("this driver does not number its rows")
}

func (contractResult) RowsAffected() (int64, error) {
	return 0, errors.New("this driver does not count what it changed")
}

// contractRows hands back its one row and then ends.
type contractRows struct {
	names []string
	row   []driver.Value
	given bool
}

func (rows *contractRows) Columns() []string { return rows.names }

func (rows *contractRows) Close() error { return nil }

func (rows *contractRows) Next(into []driver.Value) error {
	if rows.given {
		return io.EOF
	}
	rows.given = true
	copy(into, rows.row)
	return nil
}
