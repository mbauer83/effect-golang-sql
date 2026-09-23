package migrate

// Taking the lock, and reading whether it was given.

import (
	"context"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// takeLock waits for the lock, if the plan has one.
//
// A lock that answers false rather than waiting -- which is what MySQL's does
// on a timeout -- is another instance holding it, and that is reported rather
// than proceeding beside them.
func takeLock[R any](database sql.Querier, plan Plan) migration[R, effect.Unit] {
	if plan.Lock == nil {
		return effect.For[R, Fault]().Succeed(effect.Unit{})
	}
	statement := plan.Lock.Take(plan.Dialect, plan.History.Name())
	if statement.Text() == "" {
		return effect.For[R, Fault]().Succeed(effect.Unit{})
	}
	return holdLock[R](database, plan, statement)
}

// releaseLock gives the lock back, where the database does not do it on its own.
func releaseLock[R any](database sql.Querier, plan Plan) migration[R, effect.Unit] {
	if plan.Lock == nil {
		return effect.For[R, Fault]().Succeed(effect.Unit{})
	}
	statement := plan.Lock.Release(plan.Dialect, plan.History.Name())
	if statement.Text() == "" {
		// Transaction-scoped: the transaction ending frees it, which is one
		// fewer thing to get wrong than releasing it by hand.
		return effect.For[R, Fault]().Succeed(effect.Unit{})
	}
	return runStatement[R](database, plan,
		statement.Text(), statement.Values(), "freeing the lock", "")
}

// holdLock takes a lock and refuses if it did not get it.
//
// A lock statement is a select, not an execute: both of these return whether
// they got it, and MySQL's returns nought on a timeout rather than failing. So
// the answer is read rather than assumed.
func holdLock[R any](
	database sql.Querier,
	plan Plan,
	statement sql.Statement,
) migration[R, effect.Unit] {
	return effect.Try(
		func(ctx context.Context, _ R) (effect.Unit, error) {
			cursor, err := database.Query(ctx, statement.Text(), statement.Values())
			if err != nil {
				return effect.Unit{}, err
			}
			defer func() { _ = cursor.Close() }()
			if !cursor.Next() {
				return effect.Unit{}, errNotTaken
			}
			row, err := cursor.Row()
			if err != nil {
				return effect.Unit{}, err
			}
			if !isLockGranted(row) {
				return effect.Unit{}, errNotTaken
			}
			return effect.Unit{}, cursor.Err()
		},
		func(err error) Fault {
			return faultOf("taking the lock", plan.History.Name(), "", err)
		},
	)
}

// isLockGranted reads whether a lock statement said yes.
//
// The two of them answer differently -- Postgres's returns nothing useful and
// MySQL's returns one, nought or null -- so anything that is not an explicit
// no counts as yes, and an explicit no is what MySQL says on a timeout.
func isLockGranted(row dynamic.Object) bool {
	if len(row.Fields) == 0 {
		return true
	}
	switch shape := row.Fields[0].Value.(type) {
	case dynamic.Integer:
		return shape.Value != 0
	case dynamic.Boolean:
		return shape.Value
	case dynamic.Absent:
		return false
	default:
		return true
	}
}
