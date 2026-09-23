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
	// Versions are the versions it passed through, in order, each of which is
	// now in the ledger. Empty when there was nothing to do.
	Versions []string
	// Created says the tables were made rather than altered, which is what
	// happens the first time.
	Created bool
}

// IsEmpty reports whether the database was already where it was asked to be.
func (report Report) IsEmpty() bool {
	return len(report.Versions) == 0
}

type migration[R any, A any] = effect.Effect[R, Fault, A]

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
func Apply[R any](database sql.Beginner, plan Plan) migration[R, Report] {
	if err := plan.fault(); err != nil {
		return faultFrom[R, Report](
			faultOf("reading the plan", plan.History.Name(), plan.Target, err))
	}
	return sql.Transact(database,
		func(fault sql.Fault) Fault {
			return faultOf("migrating", plan.History.Name(), plan.target(), fault)
		},
		func(within sql.Querier) migration[R, Report] {
			return migrateWithin[R](within, plan)
		}).
		WithName("migrate")
}

// within3 is the whole migration, migrateWithin the transaction.
func migrateWithin[R any](within sql.Querier, plan Plan) migration[R, Report] {
	return prepareLedger[R](within, plan).
		AndThen(takeLock[R](within, plan)).
		AndThen(ledgerVersion[R](within, plan)).
		FlatMap(func(current string) migration[R, Report] {
			return decideDirection[R](within, plan, current)
		}).
		FlatMap(func(report Report) migration[R, Report] {
			return releaseLock[R](within, plan).As(report)
		})
}

func decideDirection[R any](within sql.Querier, plan Plan, current string) migration[R, Report] {
	target := plan.target()
	if current == "" {
		return createTables[R](within, plan, target)
	}
	// No special case for already being there. Stepping from a version to
	// itself is a path of one and a loop that runs no times, so it reports
	// nothing applied and does nothing -- which is what a special case would
	// have said, with a branch nobody could see fail. A neuter proved the
	// branch redundant and it went.
	return stepsFor[R](within, plan, current, target)
}

// Current is the version a database holds of an aggregate, or empty when it
// holds none.
//
// It prepares the ledger, because asking what a database holds should not fail
// merely because nothing has ever been migrated into it.
func Current[R any](database sql.Querier, plan Plan) migration[R, string] {
	if err := plan.fault(); err != nil {
		return faultFrom[R, string](
			faultOf("reading the plan", plan.History.Name(), plan.Target, err))
	}
	return prepareLedger[R](database, plan).
		AndThen(ledgerVersion[R](database, plan))
}

// prepareLedger makes the ledger if it is not there.
func prepareLedger[R any](database sql.Querier, plan Plan) migration[R, effect.Unit] {
	statement, err := createLedgerStatement(plan.Dialect, plan.ledger())
	if err != nil {
		return faultFrom[R, effect.Unit](
			faultOf("projecting the ledger", plan.History.Name(), "", err))
	}
	return runStatement[R](database, plan, statement, nil, "preparing the ledger", "")
}

func ledgerVersion[R any](database sql.Querier, plan Plan) migration[R, string] {
	// A query rather than text, so the placeholder is the dialect's: this
	// statement carried a question mark and Postgres refused it at start-up.
	statement := sql.SelectQuery{
		Select: sql.SelectColumns("aggregate", "version"),
		From:   sql.From(plan.ledger()),
		Where:  sql.ColumnEquals("aggregate", plan.History.Name()),
	}.Statement(plan.Dialect)

	return effect.RunCollect(
		sql.Rows[R](database, LedgerEntrySchema, statement).TakeStream(1),
	).
		MapError(func(fault sql.Fault) Fault {
			return faultOf("reading the ledger", plan.History.Name(), "", fault)
		}).
		Map(func(found []LedgerEntry) string {
			if len(found) == 0 {
				return ""
			}
			return found[0].Version
		})
}

func faultFrom[R, A any](fault Fault) migration[R, A] {
	return effect.For[R, Fault]().Fail[A](fault)
}

// runStatement runs one statement, naming what it was for if it fails.
func runStatement[R any](
	database sql.Querier,
	plan Plan,
	statement string,
	arguments []dynamic.Value,
	op string,
	version string,
) migration[R, effect.Unit] {
	return sql.Execute[R](database, statement, arguments...).
		MapError(func(fault sql.Fault) Fault {
			return faultOf(op, plan.History.Name(), version, fault)
		}).
		As(effect.Unit{})
}
