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

var (
	errNoRows      = errors.New("the statement returned no rows")
	errSeveralRows = errors.New("the statement returned more than one row")
	errNotAnObject = errors.New("a row is a set of named values, and this schema describes something else")
)
