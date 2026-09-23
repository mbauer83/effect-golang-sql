package migrate

// Creating a schema that is not there, and stepping one that is.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/evolve"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// createTables makes the tables as of the target, the first time.
//
// As of the target and not as of version one, then stepping forward: a database
// that starts at 3.0.0 has not skipped anything, it simply never had 1.0.0 to
// alter. What it has to remember is that it is at 3.0.0, so a later migration
// knows where to start.
func createTables[R any](
	within sql.Querier,
	plan Plan,
	target string,
) migration[R, Report] {
	node, err := plan.History.At(target)
	if err != nil {
		return faultFrom[R, Report](
			faultOf("read the history", plan.History.Name(), target, err))
	}
	statements, err := ddl.Create(plan.Dialect, node)
	if err != nil {
		return faultFrom[R, Report](
			faultOf("project the tables", plan.History.Name(), target, err))
	}

	return runInOrder[R](within, plan, statements, "creating the tables", target).
		AndThen(recordVersion[R](within, plan, target, true)).
		As(Report{
			Aggregate: plan.History.Name(),
			To:        target,
			Versions:  []string{target},
			Created:   true,
		})
}

// stepsFor applies one version at a time, recording each as it completes.
//
// One at a time rather than all the statements at once, because the ledger is
// what a second run reads: where the database rolls DDL back the distinction
// does not matter, and where it does not -- MySQL -- the ledger saying which
// step finished last is the difference between continuing and starting over.
func stepsFor[R any](
	within sql.Querier,
	plan Plan,
	current string,
	target string,
) migration[R, Report] {
	path, err := route(plan.History, current, target)
	if err != nil {
		return faultFrom[R, Report](faultOf("plan", plan.History.Name(), target, err))
	}

	chain := effect.For[R, Fault]().Succeed(effect.Unit{})
	for at := 0; at < len(path)-1; at++ {
		chain = chain.AndThen(migrateStep[R](within, plan, path[at], path[at+1]))
	}
	return chain.As(Report{
		Aggregate: plan.History.Name(),
		From:      current,
		To:        target,
		Versions:  path[1:],
	})
}

// migrateStep applies a single version's step and records it.
func migrateStep[R any](
	within sql.Querier,
	plan Plan,
	from string,
	to string,
) migration[R, effect.Unit] {
	return effect.For[R, Fault]().
		Suspend(func() migration[R, effect.Unit] {
			steps, err := actionsBetween(plan, from, to)
			if err != nil {
				return faultFrom[R, effect.Unit](
					faultOf("project a step", plan.History.Name(), to, err))
			}
			return applyActions[R](within, plan, steps, to).
				AndThen(recordVersion[R](within, plan, to, false))
		})
}

// route is the versions to pass through, in order, from one to another.
//
// Every version between them, because each is recorded as it completes and a
// ledger that jumped would not say where a failure left things.
func route(history evolve.History, from string, to string) ([]string, error) {
	versions := history.Versions()
	start, end := -1, -1
	for at, version := range versions {
		if version == from {
			start = at
		}
		if version == to {
			end = at
		}
	}
	if start < 0 || end < 0 {
		return nil, errUnknownLedgerEntry
	}

	path := []string{}
	if start <= end {
		for at := start; at <= end; at++ {
			path = append(path, versions[at])
		}
		return path, nil
	}
	for at := start; at >= end; at-- {
		path = append(path, versions[at])
	}
	return path, nil
}

// runInOrder runs statements in order, stopping at the first that fails.
func runInOrder[R any](
	within sql.Querier,
	plan Plan,
	statements []string,
	op string,
	version string,
) migration[R, effect.Unit] {
	return effect.ForEach(statements, func(statement string) migration[R, effect.Unit] {
		return runStatement[R](within, plan, statement, nil, op, version)
	}).As(effect.Unit{})
}

// actionsBetween is everything one step does, in order.
func actionsBetween(plan Plan, from string, to string) ([]Action, error) {
	stages, err := plan.History.Stages(from, to)
	if err != nil {
		return nil, err
	}
	result := []Action{}
	for _, stage := range stages {
		acts, err := actions(plan.Dialect, stage)
		if err != nil {
			return nil, err
		}
		result = append(result, acts...)
	}
	return result, nil
}

// applyActions runs the actions in order, stopping at the first that fails.
func applyActions[R any](
	within sql.Querier,
	plan Plan,
	steps []Action,
	version string,
) migration[R, effect.Unit] {
	return effect.ForEach(steps, func(action Action) migration[R, effect.Unit] {
		return run[R](within, plan, action, version)
	}).As(effect.Unit{})
}

// recordVersion writes the version into the ledger.
//
// An insert the first time and an update after, spelled out rather than done
// with an upsert: the three dialects spell an upsert three ways, and which of
// the two this is is something the caller already knows.
func recordVersion[R any](
	within sql.Querier,
	plan Plan,
	version string,
	first bool,
) migration[R, effect.Unit] {
	aggregate := plan.History.Name()

	// Said as shapes, so the placeholders are the dialect's. Written as text
	// these carried question marks, which the ledger's own reader did too and
	// which Postgres refuses.
	statement := sql.Compose(plan.Dialect,
		sql.Text("update "+plan.Dialect.QuoteIdentifier(plan.ledger())+
			" set "+plan.Dialect.QuoteIdentifier("version")+" = "),
		sql.Bind(dynamic.OfText(version)),
		sql.Text(" where "+plan.Dialect.QuoteIdentifier("aggregate")+" = "),
		sql.Bind(dynamic.OfText(aggregate)))
	if first {
		statement = sql.InsertQuery{
			Table:   plan.ledger(),
			Columns: []string{"aggregate", "version"},
			Values:  []dynamic.Value{dynamic.OfText(aggregate), dynamic.OfText(version)},
		}.Statement(plan.Dialect)
	}
	return runStatement[R](within, plan,
		statement.Text(), statement.Values(), "recording the version", version)
}
