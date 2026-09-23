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
	"time"

	stdsql "database/sql"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// Database is a database/sql database behind the port.
type Database struct {
	database *stdsql.DB
	// instants is how this driver is given a moment, decided from its name at
	// Open: see Instants.
	instants Instants
}

// Connections is how many of them this program will hold.
//
// Stated, because database/sql does not bound a pool unless it is told to:
// its default is as many connections as the program asks for at once, and a
// server has a limit. MEASURED against Postgres with forty simulated people:
// the pool grew past a hundred and the server answered "sorry, too many
// clients already", which reached a person as a 502 on an ordinary page.
//
// A limit is not a performance setting here. It is the difference between
// queueing inside this program, where a wait is a wait, and queueing at the
// server, where the wait is a refusal.
type Connections struct {
	// MaxOpen is how many may be open at once. Zero means as many as are asked
	// for, which is what exhausted a server.
	MaxOpen int
	// MaxIdle is how many are kept when nothing is using them, so a burst after
	// a quiet minute does not pay for a handshake per query.
	MaxIdle int
	// MaxIdleTime is how long an unused one is kept before it is closed, which is
	// what stops a pool sized for a peak from holding the peak all night.
	MaxIdleTime time.Duration
}

// ModestConnections are what a program that has not thought about it should
// hold.
//
// Twenty-five, which is a quarter of the hundred Postgres allows by default
// and leaves room for the other things a deployment runs -- a worker, a
// migration, somebody with a psql open. Five kept idle, closed after five
// minutes.
func ModestConnections() Connections {
	return Connections{MaxOpen: 25, MaxIdle: 5, MaxIdleTime: 5 * time.Minute}
}

// Open connects, checks that the connection works, and gives the scope the
// closing.
//
// The check is not ceremony: Open on database/sql is lazy, so a wrong address
// or a missing file would otherwise surface at the first query rather than at
// start-up, which is exactly the wrong end of the program.
//
// The pool is bounded, because an unbounded one is not a default anybody
// wants: a program that has not thought about how many connections it holds
// should hold few rather than all of them. OpenWith is for one that has.
func Open[R any](scope effect.Scope, driver string, source string) effect.Effect[R, Fault, *Database] {
	return OpenWith[R](scope, driver, source, ModestConnections())
}

// OpenWith connects, holding this many connections.
func OpenWith[R any](
	scope effect.Scope,
	driver string,
	source string,
	pool Connections,
) effect.Effect[R, Fault, *Database] {
	acquire := effect.Try(
		func(ctx context.Context, _ R) (*Database, error) {
			database, err := stdsql.Open(driver, source)
			if err != nil {
				return nil, err
			}
			pool.applyTo(database)
			if err := database.PingContext(ctx); err != nil {
				// The handle is useless and would otherwise hold whatever it
				// managed to open.
				_ = database.Close()
				return nil, err
			}
			return &Database{database: database, instants: instantsFor(driver)}, nil
		},
		func(err error) Fault { return faultOf("open "+driver, "", err) },
	).WithName("open")

	return scope.AcquireRelease(acquire, disconnect[R])
}

func disconnect[R any](db *Database) effect.Effect[R, effect.Never, effect.Unit] {
	return effect.AddFinalizer[R](func(context.Context) error { return db.database.Close() })
}

// Query runs a statement that returns rows.
func (db *Database) Query(
	ctx context.Context,
	statement string,
	arguments []dynamic.Value,
) (Cursor, error) {
	values, err := bindings(arguments, db.instants)
	if err != nil {
		return nil, err
	}
	rows, err := db.database.QueryContext(ctx, statement, values...)
	if err != nil {
		return nil, err
	}
	return &stdCursor{rows: rows}, nil
}

// Execute runs a statement that returns none.
func (db *Database) Execute(
	ctx context.Context,
	statement string,
	arguments []dynamic.Value,
) (Outcome, error) {
	values, err := bindings(arguments, db.instants)
	if err != nil {
		return Outcome{}, err
	}
	result, err := db.database.ExecContext(ctx, statement, values...)
	if err != nil {
		return Outcome{}, err
	}
	return outcomeOf(result), nil
}

// Begin starts a transaction.
func (db *Database) Begin(ctx context.Context) (Transaction, error) {
	transaction, err := db.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &stdTransaction{transaction: transaction, instants: db.instants}, nil
}

// outcomeOf reads what a driver will say. A driver that does not know how many
// rows changed says so by refusing to answer, and a count of zero would be a
// different claim.
func outcomeOf(result stdsql.Result) Outcome {
	rows, err := result.RowsAffected()
	if err != nil {
		return Outcome{RowsAffected: -1}
	}
	return Outcome{RowsAffected: rows}
}

// applyTo bounds a pool, leaving anything unstated as database/sql has it.
//
// Unstated rather than defaulted here, because a caller that said MaxOpen and
// nothing else meant MaxOpen: filling in the rest would be this deciding what
// they left out.
func (pool Connections) applyTo(database *stdsql.DB) {
	if pool.MaxOpen > 0 {
		database.SetMaxOpenConns(pool.MaxOpen)
	}
	if pool.MaxIdle > 0 {
		database.SetMaxIdleConns(pool.MaxIdle)
	}
	if pool.MaxIdleTime > 0 {
		database.SetConnMaxIdleTime(pool.MaxIdleTime)
	}
}
