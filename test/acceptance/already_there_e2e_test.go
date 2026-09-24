package acceptance

// Whether every driver's duplicate-key refusal is recognised as one.
//
// Stated per dialect against a real server, because the whole point of the
// classification is that three drivers say it three different ways -- and a
// unit test with a hand-made error would only prove that the shape somebody
// imagined is recognised. The shapes are measured: pgx offers SQLState,
// modernc's sqlite offers Code, and MySQL's driver offers neither and is
// recognised by its text.

import (
	"context"
	"errors"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

func TestOnPostgresADuplicateKeyIsRecognisedAsAlreadyThere(t *testing.T) {
	duplicateKeyIsRecognised(t, "EFFECT_GOLANG_POSTGRES_URL", "pgx")
}

func TestOnMySQLADuplicateKeyIsRecognisedAsAlreadyThere(t *testing.T) {
	duplicateKeyIsRecognised(t, "EFFECT_GOLANG_MYSQL_URL", "mysql")
}

func TestOnSQLiteADuplicateKeyIsRecognisedAsAlreadyThere(t *testing.T) {
	// No variable: an in-memory database needs no server, so this one always
	// runs and the classification is never wholly unproved.
	runDuplicateKey(t, "sqlite", "file:alreadythere?mode=memory&cache=shared")
}

func duplicateKeyIsRecognised(t *testing.T, variable string, driverName string) {
	t.Helper()
	address := os.Getenv(variable)
	if address == "" {
		t.Skipf("set %s to run this against a real %s server", variable, driverName)
	}
	runDuplicateKey(t, driverName, address)
}

// runDuplicateKey inserts the same key twice and asks what the refusal was.
func runDuplicateKey(t *testing.T, driverName string, address string) {
	t.Helper()
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, sql.Fault, error] {
		return sql.Open[effect.Unit](scope, driverName, address).
			FlatMap(func(connected *sql.Database) effect.Effect[effect.Unit, sql.Fault, error] {
				return insertTwice(connected)
			})
	})
	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	refused, ran := exit.Value()
	if !ran {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if refused == nil {
		t.Fatal("expected the second insert refused")
	}
	if !errors.Is(refused, sql.ErrAlreadyThere) {
		t.Fatalf("expected %s's duplicate key recognised as already there, got %T %v",
			driverName, refused, refused)
	}
	// And it is not mistaken for the conditions this package raises itself.
	if errors.Is(refused, sql.ErrNoRows) || errors.Is(refused, sql.ErrSeveralRows) {
		t.Fatalf("expected a duplicate key to be only that, got %v", refused)
	}
}

// insertTwice makes a table, inserts one key twice, and answers with what
// the second attempt was refused with.
//
// The refusal is the value rather than the failure, because it is what the
// test is about: a failure here would have to be unwrapped out of an Exit to
// look at the thing being examined.
func insertTwice(
	connected *sql.Database,
) effect.Effect[effect.Unit, sql.Fault, error] {
	return run(connected, `DROP TABLE IF EXISTS already_there`).
		FlatMap(func(effect.Unit) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
			return run(connected,
				`CREATE TABLE already_there (id VARCHAR(32) NOT NULL PRIMARY KEY)`)
		}).
		FlatMap(func(effect.Unit) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
			return run(connected, `INSERT INTO already_there (id) VALUES ('once')`)
		}).
		FlatMap(func(effect.Unit) effect.Effect[effect.Unit, sql.Fault, error] {
			// The one that must be refused, and its refusal is the answer.
			return effect.Fold(
				run(connected, `INSERT INTO already_there (id) VALUES ('once')`),
				func(cause effect.Cause[sql.Fault]) error { return refusalIn(cause) },
				func(effect.Unit) error { return nil },
			).MapError(func(effect.Never) sql.Fault { return sql.Fault{} })
		})
}

// refusalIn is the fault a cause carries, or the cause itself when it carries
// none -- so a defect is reported rather than mistaken for no refusal.
func refusalIn(cause effect.Cause[sql.Fault]) error {
	if refused, single := cause.Failure(); single {
		return refused
	}
	if failures := cause.Failures(); len(failures) > 0 {
		return failures[0]
	}
	return errors.New(cause.String())
}

// run is one statement, for its effect.
func run(
	connected *sql.Database,
	text string,
) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
	return sql.Execute[effect.Unit](connected, text).
		Map(func(sql.Outcome) effect.Unit { return effect.Unit{} })
}
