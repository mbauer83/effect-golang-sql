package sql

import "errors"

// Fault is what this package can fail with: a statement the database refused, a
// row that could not be read, or a connection that could not be made.
//
// It is not an application failure. A repository's own refusals -- no such
// customer, the order is already paid -- have the application's type, and
// MapError adapts this into them.
type Fault struct {
	// Doing names what was being attempted, which is what a caller acts on.
	Doing string
	// Statement is the SQL, where a fault is about one. It is here because a
	// database error without the statement that caused it is nearly useless,
	// and because the statement is the program's own text rather than a user's.
	Statement string
	Err       error
}

func (fault Fault) Error() string {
	rendered := "sql: " + fault.Doing
	if fault.Statement != "" {
		rendered += " [" + fault.Statement + "]"
	}
	if fault.Err != nil {
		rendered += ": " + fault.Err.Error()
	}
	return rendered
}

// Unwrap keeps errors.Is and errors.As working through the boundary, so a
// caller can still ask a driver whether a constraint was violated.
func (fault Fault) Unwrap() error {
	return fault.Err
}

func faulted(doing string, statement string, err error) Fault {
	return Fault{Doing: doing, Statement: statement, Err: err}
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
