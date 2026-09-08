package migrate

import "errors"

// Fault is what went wrong, and where.
type Fault struct {
	// Doing names the stage.
	Doing string
	// Aggregate is the history it was about.
	Aggregate string
	// Version is the version it was moving to, where there was one.
	Version string
	Err     error
}

func (fault Fault) Error() string {
	rendered := "migrate: " + fault.Doing
	if fault.Aggregate != "" {
		rendered += " [" + fault.Aggregate
		if fault.Version != "" {
			rendered += " to " + fault.Version
		}
		rendered += "]"
	}
	if fault.Err != nil {
		rendered += ": " + fault.Err.Error()
	}
	return rendered
}

// Unwrap keeps errors.Is and errors.As working through the boundary.
func (fault Fault) Unwrap() error { return fault.Err }

func faulted(doing string, aggregate string, version string, err error) Fault {
	return Fault{Doing: doing, Aggregate: aggregate, Version: version, Err: err}
}

var (
	errNoHistory = errors.New("a migration needs a history to apply")
	errNoDialect = errors.New("a migration needs to know which database it is talking to")
	errNotTaken  = errors.New(
		"another instance holds the migration lock: it is migrating, or it stopped while " +
			"holding it")
	errUnknownRecorded = errors.New(
		"the ledger holds a version this history does not describe: somebody removed a " +
			"version, or this database belongs to another program")
)
