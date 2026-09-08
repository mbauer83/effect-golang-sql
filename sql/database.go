package sql

// The database/sql adapter.
//
// Parameters are always bound and never interpolated, which is what "prepared
// statements by default" means where it matters: the port has no way to pass a
// value except as an argument, so a statement built by concatenation cannot be
// expressed through it. Whether the driver prepares and caches is the driver's
// business, and a caching adapter is a thing to add when measurement asks for
// one rather than before.

import (
	"context"

	stdsql "database/sql"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// Connected is a database/sql database behind the port.
type Connected struct {
	database *stdsql.DB
}

// Open connects, checks that the connection works, and gives the scope the
// closing.
//
// The check is not ceremony: Open on database/sql is lazy, so a wrong address
// or a missing file would otherwise surface at the first query rather than at
// start-up, which is exactly the wrong end of the program.
func Open[R any](scope effect.Scope, driver string, source string) effect.Effect[R, Fault, *Connected] {
	acquire := effect.Try(
		func(ctx context.Context, _ R) (*Connected, error) {
			database, err := stdsql.Open(driver, source)
			if err != nil {
				return nil, err
			}
			if err := database.PingContext(ctx); err != nil {
				// The handle is useless and would otherwise hold whatever it
				// managed to open.
				_ = database.Close()
				return nil, err
			}
			return &Connected{database: database}, nil
		},
		func(err error) Fault { return faulted("opening "+driver, "", err) },
	).Named("open")

	return scope.AcquireRelease(acquire, disconnecting[R])
}

func disconnecting[R any](connected *Connected) effect.Effect[R, effect.Never, effect.Unit] {
	return effect.Release[R](func(context.Context) error { return connected.database.Close() })
}

// Query runs a statement that returns rows.
func (connected *Connected) Query(
	ctx context.Context,
	statement string,
	arguments []dynamic.Value,
) (Cursor, error) {
	bound, err := bindings(arguments)
	if err != nil {
		return nil, err
	}
	rows, err := connected.database.QueryContext(ctx, statement, bound...)
	if err != nil {
		return nil, err
	}
	return &walking{rows: rows}, nil
}

// Execute runs a statement that returns none.
func (connected *Connected) Execute(
	ctx context.Context,
	statement string,
	arguments []dynamic.Value,
) (Outcome, error) {
	bound, err := bindings(arguments)
	if err != nil {
		return Outcome{}, err
	}
	result, err := connected.database.ExecContext(ctx, statement, bound...)
	if err != nil {
		return Outcome{}, err
	}
	return outcomeOf(result), nil
}

// Begin starts a transaction.
func (connected *Connected) Begin(ctx context.Context) (Transaction, error) {
	transaction, err := connected.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &transacting{transaction: transaction}, nil
}

// outcomeOf reads what a driver will say. A driver that does not know how many
// rows changed says so by refusing to answer, and a count of zero would be a
// different claim.
func outcomeOf(result stdsql.Result) Outcome {
	changed, err := result.RowsAffected()
	if err != nil {
		return Outcome{Changed: -1}
	}
	return Outcome{Changed: changed}
}
