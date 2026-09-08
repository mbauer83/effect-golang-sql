package migrate

// Working out what is left to do, and doing that once.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// Report is what a migration did.
type Report struct {
	// Aggregate is the history that was applied.
	Aggregate string
	// From is the version the database was at, or empty when it held nothing
	// of this aggregate.
	From string
	// To is the version it is at now.
	To string
	// Applied are the versions it passed through, in order, each of which is
	// now in the ledger. Empty when there was nothing to do.
	Applied []string
	// Created says the tables were made rather than altered, which is what
	// happens the first time.
	Created bool
}

// Nothing reports whether the database was already where it was asked to be.
func (report Report) Nothing() bool {
	return len(report.Applied) == 0
}

type migrating[R any, A any] = effect.Effect[R, Fault, A]

// Apply brings a database to a version of a history, and does nothing if it is
// already there.
//
// Everything happens in **one transaction**: the lock is taken, the ledger is
// read, the steps are applied and each is recorded as it completes. Where the
// database has transactional DDL -- Postgres and SQLite -- that makes the whole
// migration atomic, so a failure at the fourth step leaves a database still at
// the version it started from. MySQL commits its DDL as it goes and cannot
// offer that; what it does offer is that the lock is held throughout and the
// ledger says which step was the last to finish, so running again continues
// from there rather than from the beginning.
//
// The first time, the tables are created as of the target and that version is
// recorded -- because a database that starts at 3.0.0 has not skipped anything,
// it simply never had 1.0.0 to alter.
//
// Either direction. A target earlier than the recorded version runs the
// inverses, which is a thing to do knowingly: some of them cannot restore what
// they dropped.
func Apply[R any](database sql.Beginning, plan Plan) migrating[R, Report] {
	if err := plan.fault(); err != nil {
		return failing[R, Report](
			faulted("reading the plan", plan.History.Name(), plan.Target, err))
	}
	return sql.Transact(database,
		func(fault sql.Fault) Fault {
			return faulted("migrating", plan.History.Name(), plan.target(), fault)
		},
		func(within sql.Querying) migrating[R, Report] {
			return inside[R](within, plan)
		}).
		Named("migrate")
}

// within3 is the whole migration, inside the transaction.
func inside[R any](within sql.Querying, plan Plan) migrating[R, Report] {
	return prepared[R](within, plan).
		AndThen(taken[R](within, plan)).
		AndThen(recordedVersion[R](within, plan)).
		FlatMap(func(current string) migrating[R, Report] {
			return decided[R](within, plan, current)
		}).
		FlatMap(func(report Report) migrating[R, Report] {
			return freed[R](within, plan).As(report)
		})
}

func decided[R any](within sql.Querying, plan Plan, current string) migrating[R, Report] {
	target := plan.target()
	if current == "" {
		return creatingTables[R](within, plan, target)
	}
	// No special case for already being there. Stepping from a version to
	// itself is a path of one and a loop that runs no times, so it reports
	// nothing applied and does nothing -- which is what a special case would
	// have said, with a branch nobody could see fail. A neuter proved the
	// branch redundant and it went.
	return stepping[R](within, plan, current, target)
}

// Current is the version a database holds of an aggregate, or empty when it
// holds none.
//
// It prepares the ledger, because asking what a database holds should not fail
// merely because nothing has ever been migrated into it.
func Current[R any](database sql.Querying, plan Plan) migrating[R, string] {
	if err := plan.fault(); err != nil {
		return failing[R, string](
			faulted("reading the plan", plan.History.Name(), plan.Target, err))
	}
	return prepared[R](database, plan).
		AndThen(recordedVersion[R](database, plan))
}

// prepared makes the ledger if it is not there.
func prepared[R any](database sql.Querying, plan Plan) migrating[R, effect.Unit] {
	statement, err := creating(plan.Dialect, plan.ledger())
	if err != nil {
		return failing[R, effect.Unit](
			faulted("projecting the ledger", plan.History.Name(), "", err))
	}
	return running[R](database, plan, statement, nil, "preparing the ledger", "")
}

func recordedVersion[R any](database sql.Querying, plan Plan) migrating[R, string] {
	dialect := plan.Dialect
	statement := "select " + dialect.Quoted("aggregate") + ", " +
		dialect.Quoted("version") + " from " + dialect.Quoted(plan.ledger()) +
		" where " + dialect.Quoted("aggregate") + " = ?"

	return effect.RunCollect(
		sql.Query[R](database, RecordedSchema, statement,
			dynamic.OfText(plan.History.Name())).TakeStream(1),
	).
		MapError(func(fault sql.Fault) Fault {
			return faulted("reading the ledger", plan.History.Name(), "", fault)
		}).
		Map(func(found []Recorded) string {
			if len(found) == 0 {
				return ""
			}
			return found[0].Version
		})
}

func failing[R, A any](fault Fault) migrating[R, A] {
	return effect.For[R, Fault]().Fail[A](fault)
}

// running runs one statement, naming what it was for if it fails.
func running[R any](
	database sql.Querying,
	plan Plan,
	statement string,
	arguments []dynamic.Value,
	doing string,
	version string,
) migrating[R, effect.Unit] {
	return sql.Execute[R](database, statement, arguments...).
		MapError(func(fault sql.Fault) Fault {
			return faulted(doing, plan.History.Name(), version, fault)
		}).
		As(effect.Unit{})
}
