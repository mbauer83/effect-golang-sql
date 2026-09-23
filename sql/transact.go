package sql

// A transaction is a scoped resource.
//
// It commits when the work succeeds and rolls back when it fails or is
// interrupted, which is AcquireRelease with an explicit commit on the way out
// and nothing new. The release rolls back unconditionally: after a commit that
// is a no-op the driver reports and this ignores, and before one it is the only
// thing that keeps a cancelled transaction from being left open.

import (
	"context"
	"errors"

	stdsql "database/sql"

	"github.com/mbauer83/effect-golang/effect"
)

// Transact runs work in a transaction.
//
// The transaction's lifetime is the work, not the caller's scope, so it owns
// one of its own: a caller cannot hold a transaction open past the effect that
// asked for it, and cannot forget to end it.
//
// mapFault is how a fault of this package becomes the work's own failure. It is
// a parameter rather than a fixed type because a repository's refusals are the
// application's -- no such customer, the order is already paid -- and a
// transaction that forced them into a database fault would have the layering
// backwards.
func Transact[R, E, A any](
	database Beginner,
	mapFault func(Fault) E,
	work func(Querier) effect.Effect[R, E, A],
) effect.Effect[R, E, A] {
	return effect.Scoped(func(scope effect.Scope) effect.Effect[R, E, A] {
		return effect.Gen(func(do *effect.Do[R, E]) A {
			begin := beginTransaction[R](database).MapError(mapFault)
			transaction := do.Await(scope.AcquireRelease(begin, rollbackTransaction[R]))
			value := do.Await(work(transaction))
			do.Await(commitTransaction[R](transaction).MapError(mapFault))
			return value
		}).WithName("transaction")
	})
}

func beginTransaction[R any](database Beginner) effect.Effect[R, Fault, Transaction] {
	return effect.Try(
		func(ctx context.Context, _ R) (Transaction, error) { return database.Begin(ctx) },
		func(err error) Fault { return faultOf("beginning a transaction", "", err) },
	).WithName("begin")
}

func commitTransaction[R any](transaction Transaction) effect.Effect[R, Fault, effect.Unit] {
	return effect.Try(
		func(context.Context, R) (effect.Unit, error) {
			return effect.Unit{}, transaction.Commit()
		},
		func(err error) Fault { return faultOf("committing", "", err) },
	).WithName("commit")
}

// rollbackTransaction ends the transaction if it has not ended.
//
// Three errors mean it has already ended, and all three are the outcome that
// was wanted. ErrTxDone is the driver saying so after a commit. A context
// error is the driver saying the transaction's own context was cancelled --
// which is not a rollback that failed to happen but a rollback that happened
// without being asked: cancelling a transaction's context is how a database
// ends one, the connection goes and the server aborts what was uncommitted.
//
// Anything else is a rollback that did not happen, and a transaction left open
// is worth a defect: it holds locks until something else notices.
//
// The context error matters more than it looks. A fiber's context is cancelled
// when the fiber completes, so it is cancelled by the time this runs for any
// work that failed -- which made every typed refusal raised inside a
// transaction arrive as a cause carrying a defect. A boundary answers a cause
// with a defect in it as a five hundred, so a refusal a caller was meant to
// act on became "this system broke". MEASURED against Postgres: the rollback
// answers *pgconn.errTimeout, "timeout: context already done: context
// canceled", which errors.Is matches to context.Canceled and not to
// ErrTxDone.
func rollbackTransaction[R any](transaction Transaction) effect.Effect[R, effect.Never, effect.Unit] {
	return effect.AddFinalizer[R](func(context.Context) error {
		if err := transaction.Rollback(); err != nil && !isTransactionDone(err) {
			return err
		}
		return nil
	})
}

// isTransactionDone reports whether a rollback's error says the transaction is no
// longer open.
func isTransactionDone(err error) bool {
	return errors.Is(err, stdsql.ErrTxDone) || isContextDone(err)
}
