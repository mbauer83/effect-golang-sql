package migrate

import "errors"

// Fault is what went wrong, and where.
type Fault struct {
	// Op names the stage.
	Op string
	// Aggregate is the history it was about.
	Aggregate string
	// Version is the version it was moving to, where there was one.
	Version string
	Err     error
}

func (fault Fault) Error() string {
	message := "migrate: " + fault.Op
	if fault.Aggregate != "" {
		message += " [" + fault.Aggregate
		if fault.Version != "" {
			message += " to " + fault.Version
		}
		message += "]"
	}
	if fault.Err != nil {
		message += ": " + fault.Err.Error()
	}
	return message
}

// Unwrap keeps errors.Is and errors.As working through the boundary.
func (fault Fault) Unwrap() error { return fault.Err }

func faultOf(op string, aggregate string, version string, err error) Fault {
	return Fault{Op: op, Aggregate: aggregate, Version: version, Err: err}
}

var (
	errNoHistory = errors.New("a migration needs a history to apply")
	errNoDialect = errors.New("a migration needs to know which database it is talking to")
	errNotTaken  = errors.New(
		"another instance holds the migration lock: it is migrating, or it stopped while " +
			"holding it")
	errNothingToRun = errors.New(
		"a rewriting that says nothing about moving anything is the ordinary changes with " +
			"extra words around them")
	errNotAnObject = errors.New(
		"a version is a set of named fields, and this one has none")
	errUnknownLedgerEntry = errors.New(
		"the ledger holds a version this history does not describe: somebody removed a " +
			"version, or this database belongs to another program")
)
