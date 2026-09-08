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
// failing is how a fault of this package becomes the work's own failure. It is
// a parameter rather than a fixed type because a repository's refusals are the
// application's -- no such customer, the order is already paid -- and a
// transaction that forced them into a database fault would have the layering
// backwards.
func Transact[R, E, A any](
	database Beginning,
	failing func(Fault) E,
	work func(Querying) effect.Effect[R, E, A],
) effect.Effect[R, E, A] {
	return effect.Scoped(func(scope effect.Scope) effect.Effect[R, E, A] {
		return scope.AcquireRelease(beginning[R](database).MapError(failing), rollingBack[R]).
			FlatMap(func(transaction Transaction) effect.Effect[R, E, A] {
				return work(transaction).
					FlatMap(func(value A) effect.Effect[R, E, A] {
						return committing[R](transaction).MapError(failing).As(value)
					})
			}).
			Named("transaction")
	})
}

func beginning[R any](database Beginning) effect.Effect[R, Fault, Transaction] {
	return effect.Try(
		func(ctx context.Context, _ R) (Transaction, error) { return database.Begin(ctx) },
		func(err error) Fault { return faulted("beginning a transaction", "", err) },
	).Named("begin")
}

func committing[R any](transaction Transaction) effect.Effect[R, Fault, effect.Unit] {
	return effect.Try(
		func(context.Context, R) (effect.Unit, error) {
			return effect.Unit{}, transaction.Commit()
		},
		func(err error) Fault { return faulted("committing", "", err) },
	).Named("commit")
}

// rollingBack ends the transaction if it has not ended.
//
// A rollback after a commit is the driver saying the transaction is already
// over, which is the outcome that was wanted. Anything else is a rollback that
// did not happen, and a transaction left open is worth a defect: it holds locks
// until something else notices.
func rollingBack[R any](transaction Transaction) effect.Effect[R, effect.Never, effect.Unit] {
	return effect.Release[R](func(context.Context) error {
		if err := transaction.Rollback(); err != nil && !errors.Is(err, stdsql.ErrTxDone) {
			return err
		}
		return nil
	})
}
