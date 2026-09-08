package migrate

// What a migration is asked to do, and the few things about it that are
// configurable.

import (
	"strings"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

// Plan is one migration.
//
// A struct rather than five positional arguments, because four of them are
// strings and interfaces that would be easy to hand over in the wrong order --
// and because the optional two then have somewhere to be optional.
type Plan struct {
	// Dialect is the database's own spelling. It has to be the dialect the
	// database actually is, which is the one thing here nothing can check.
	Dialect ddl.Dialect
	// History is the aggregate's versions.
	History evolve.History
	// Target is the version to bring the database to. Empty means the latest
	// the history declares, which is what a deployment usually wants.
	Target string
	// Ledger is the table recording what has been applied. Empty means
	// "schema_version".
	//
	// Configurable because a database may already have a table of that name,
	// or a convention of its own, or two applications sharing one schema that
	// each want their own.
	Ledger string
	// Lock is how several instances agree on which of them migrates. Nil means
	// they do not, which is right when one process migrates and wrong when
	// every replica tries.
	Lock Lock
}

func (plan Plan) ledger() string {
	if strings.TrimSpace(plan.Ledger) == "" {
		return "schema_version"
	}
	return plan.Ledger
}

// target is the version asked for, or the latest.
func (plan Plan) target() string {
	if strings.TrimSpace(plan.Target) == "" {
		return plan.History.Latest()
	}
	return plan.Target
}

// fault is why the plan cannot be carried out, if it cannot.
func (plan Plan) fault() error {
	switch {
	case plan.Dialect == nil:
		return errNoDialect
	case plan.History.Fault() != nil:
		return plan.History.Fault()
	case plan.History.Latest() == "":
		return errNoHistory
	}
	if _, err := plan.History.At(plan.target()); err != nil {
		return err
	}
	return nil
}
