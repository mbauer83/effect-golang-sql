package migrate

// Creating a schema that is not there, and stepping one that is.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/evolve"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// creatingTables makes the tables as of the target, the first time.
//
// As of the target and not as of version one, then stepping forward: a database
// that starts at 3.0.0 has not skipped anything, it simply never had 1.0.0 to
// alter. What it has to remember is that it is at 3.0.0, so a later migration
// knows where to start.
func creatingTables[R any](
	within sql.Querying,
	plan Plan,
	target string,
) migrating[R, Report] {
	node, err := plan.History.At(target)
	if err != nil {
		return faultFrom[R, Report](
			faultOf("reading the history", plan.History.Name(), target, err))
	}
	statements, err := ddl.Create(plan.Dialect, node)
	if err != nil {
		return faultFrom[R, Report](
			faultOf("projecting the tables", plan.History.Name(), target, err))
	}

	return inOrder[R](within, plan, statements, "creating the tables", target).
		AndThen(recordApplied[R](within, plan, target, true)).
		As(Report{
			Aggregate: plan.History.Name(),
			To:        target,
			Applied:   []string{target},
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
	within sql.Querying,
	plan Plan,
	current string,
	target string,
) migrating[R, Report] {
	path, err := route(plan.History, current, target)
	if err != nil {
		return faultFrom[R, Report](faultOf("planning", plan.History.Name(), target, err))
	}

	stepped := effect.For[R, Fault]().Succeed(effect.Unit{})
	for at := 0; at < len(path)-1; at++ {
		stepped = stepped.AndThen(one[R](within, plan, path[at], path[at+1]))
	}
	return stepped.As(Report{
		Aggregate: plan.History.Name(),
		From:      current,
		To:        target,
		Applied:   path[1:],
	})
}

// one applies a single version's step and records it.
func one[R any](
	within sql.Querying,
	plan Plan,
	from string,
	to string,
) migrating[R, effect.Unit] {
	return effect.For[R, Fault]().
		Suspend(func() migrating[R, effect.Unit] {
			planOfed, err := planOf(plan, from, to)
			if err != nil {
				return faultFrom[R, effect.Unit](
					faultOf("projecting a step", plan.History.Name(), to, err))
			}
			return applyStep[R](within, plan, planOfed, to).
				AndThen(recordApplied[R](within, plan, to, false))
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
		return nil, errUnknownRecorded
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

// inOrder runs statements in order, stopping at the first that fails.
func inOrder[R any](
	within sql.Querying,
	plan Plan,
	statements []string,
	doing string,
	version string,
) migrating[R, effect.Unit] {
	return effect.ForEach(statements, func(statement string) migrating[R, effect.Unit] {
		return runSteps[R](within, plan, statement, nil, doing, version)
	}).As(effect.Unit{})
}

// planOf is everything one step does, in order.
func planOf(plan Plan, from string, to string) ([]Action, error) {
	stages, err := plan.History.Stages(from, to)
	if err != nil {
		return nil, err
	}
	heldValue := []Action{}
	for _, stage := range stages {
		acts, err := actions(plan.Dialect, stage)
		if err != nil {
			return nil, err
		}
		heldValue = append(heldValue, acts...)
	}
	return heldValue, nil
}

// applyStep runs the actions in order, stopping at the first that fails.
func applyStep[R any](
	within sql.Querying,
	plan Plan,
	action []Action,
	version string,
) migrating[R, effect.Unit] {
	return effect.ForEach(action, func(action Action) migrating[R, effect.Unit] {
		return run[R](within, plan, action, version)
	}).As(effect.Unit{})
}

// recordApplied writes the version into the ledger.
//
// An insert the first time and an update after, spelled out rather than done
// with an upsert: the three dialects spell an upsert three ways, and which of
// the two this is is something the caller already knows.
func recordApplied[R any](
	within sql.Querying,
	plan Plan,
	version string,
	first bool,
) migrating[R, effect.Unit] {
	aggregate := plan.History.Name()

	// Said as shapes, so the placeholders are the dialect's. Written as text
	// these carried question marks, which the ledger's own reader did too and
	// which Postgres refuses.
	statement := sql.Compose(plan.Dialect,
		sql.Text("update "+plan.Dialect.Quoted(plan.ledger())+
			" set "+plan.Dialect.Quoted("version")+" = "),
		sql.Bind(dynamic.OfText(version)),
		sql.Text(" where "+plan.Dialect.Quoted("aggregate")+" = "),
		sql.Bind(dynamic.OfText(aggregate)))
	if first {
		statement = sql.Writing{
			Table:   plan.ledger(),
			Columns: []string{"aggregate", "version"},
			Values:  []dynamic.Value{dynamic.OfText(aggregate), dynamic.OfText(version)},
		}.Statement(plan.Dialect)
	}
	return runSteps[R](within, plan,
		statement.Text(), statement.Values(), "recording the version", version)
}
