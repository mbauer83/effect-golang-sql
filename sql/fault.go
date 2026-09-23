package sql

import (
	"context"
	"errors"
	"strings"
)

// Fault is what this package can fail with: a statement the database refused, a
// row that could not be read, or a connection that could not be made.
//
// It is not an application failure. A repository's own refusals -- no such
// customer, the order is already paid -- have the application's type, and
// MapError adapts this into them.
type Fault struct {
	// Op names what was being attempted, which is what a caller acts on.
	Op string
	// Statement is the SQL, where a fault is about one. It is here because a
	// database error without the statement that caused it is nearly useless,
	// and because the statement is the program's own text rather than a user's.
	Statement string
	Err       error
}

func (fault Fault) Error() string {
	message := "sql: " + fault.Op
	if fault.Statement != "" {
		message += " [" + fault.Statement + "]"
	}
	if fault.Err != nil {
		message += ": " + fault.Err.Error()
	}
	return message
}

// Unwrap keeps errors.Is and errors.As working through the boundary, so a
// caller can still ask a driver whether a constraint was violated.
func (fault Fault) Unwrap() error {
	return fault.Err
}

// Is answers for the conditions this package names about a driver's own
// refusals, so a caller writes errors.Is and does not have to know three
// drivers' error shapes.
//
// Only additive: anything else falls through to Unwrap, so the sentinels this
// package fails with keep matching exactly as before.
func (fault Fault) Is(target error) bool {
	return target == ErrAlreadyThere && isAlreadyThere(fault.Err)
}

func faultOf(op string, statement string, err error) Fault {
	return Fault{Op: op, Statement: statement, Err: err}
}

// What QueryRow refuses with, exported because a caller has to be able to tell
// them apart.
//
// A repository asking for one row by its identity is asking a question with
// three answers, not two: the thing is there, the thing is not there, or the
// database could not be reached. The first two are ordinary and the third is a
// failure, and a caller that could not distinguish them would have to treat a
// film nobody has saved and a database that is down as the same event.
//
// A Fault unwraps, so errors.Is reaches these through one.
var (
	// ErrNoRows is a statement that returned none where one was asked for.
	ErrNoRows = errors.New("the statement returned no rows")
	// ErrSeveralRows is a statement that returned more than one where one was
	// asked for, which is noticed rather than quietly truncated: a question
	// phrased as one row and answered with two is a question about something
	// other than what the caller thought.
	ErrSeveralRows = errors.New("the statement returned more than one row")
)

var errNotAnObject = errors.New("a row is a set of named values, and this schema describes something else")

// statementFault is a statement that was never composed, as a fault.
//
// Not a database error, because no database was asked: a column no source has
// or an operation the dialect cannot perform is a mistake in the query, and
// saying so with the query's own words beats a syntax error from a server that
// was handed something half-written.
func statementFault(why error) Fault {
	return faultOf("composing", "a statement this query could not compose", why)
}

// ErrAlreadyThere is a statement the database refused because a value it
// requires to be unique is already there.
//
// Named here rather than left to each caller, because it is the one driver
// condition an application routinely has to act on: an identity a client
// chose, a natural key, a row somebody is inserting twice. Every one of those
// is a conflict the client can do something about, and a store that could not
// tell it from a database being down would answer the first with the status of
// the second.
//
// It is also the only correct way to establish it. Reading first and then
// inserting is a check two concurrent writers both pass, so the unique index
// is the arbiter whatever the application does -- and this is how its verdict
// comes back as something other than a failure.
//
//	errors.Is(faulted, sql.ErrAlreadyThere)
var ErrAlreadyThere = errors.New(
	"a value this database requires to be unique is already there")

// isAlreadyThere reports whether a driver's error is that condition.
//
// Through interfaces the drivers already satisfy rather than by importing
// them, which is what keeps this package free of a dependency on any one:
// pgx's error offers SQLState, modernc's sqlite offers Code, and neither
// package is mentioned here.
//
// MySQL's driver offers neither -- its error carries the number in a field --
// so that one is recognised by the text its Error method produces. Worth
// saying plainly rather than hiding: it is the weakest of the three and the
// only one a driver could break without a compile error.
func isAlreadyThere(err error) bool {
	if err == nil {
		return false
	}
	var withState interface{ SQLState() string }
	if errors.As(err, &withState) {
		// 23505 is unique_violation in the SQL standard's class 23,
		// integrity constraint violation.
		return withState.SQLState() == "23505"
	}
	var withCode interface{ Code() int }
	if errors.As(err, &withCode) {
		// 1555 is a duplicate primary key and 2067 a duplicate on any other
		// unique index; SQLite reports them as extended result codes.
		return withCode.Code() == 1555 || withCode.Code() == 2067
	}
	return strings.Contains(err.Error(), mysqlDuplicateEntry)
}

// mysqlDuplicateEntry is how MySQL's driver spells error 1062.
const mysqlDuplicateEntry = "Error 1062"

// isContextDone reports whether an error is a driver saying that work
// ended because its context did.
//
// Which is not a failure to clean up: it is cleanup that happened without
// being asked. A cancelled context takes the connection with it, so the
// transaction is aborted and the cursor is closed by the time anything here
// asks -- and asking then is what produces the error.
//
// It matters because a fiber's context is cancelled when the fiber completes,
// so it is already cancelled whenever a release runs for work that failed.
// Reading these as real failures made a release turn every typed refusal into
// a cause carrying a defect, which a boundary answers as a five hundred. Two
// releases in this package did that. MEASURED against Postgres: a rollback
// answers "timeout: context already done: context canceled" and a cursor
// close answers "context canceled", and errors.Is matches both to
// context.Canceled.
func isContextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
