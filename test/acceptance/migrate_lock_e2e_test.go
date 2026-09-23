package acceptance

// Taking the advisory lock on the servers that have one.
//
// The case this exists for already happened: the Postgres lock was composed
// with a question mark, which names the right function and is refused by
// Postgres for its argument. Every unit test passed, because a unit test can
// only compare the statement to what somebody expected it to say -- and the
// expectation had the same mistake in it. Only a server can answer whether a
// lock statement takes a lock.
//
// Gated on an address, like the rest of these: the whole migration is
// exercised elsewhere, and this is the one statement inside it that no
// SQLite run can reach, because SQLite has no advisory lock to take.

import (
	"context"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/migrate"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// takeLock takes the lock, reads what the server answered, and gives it back.
func takeLock(
	t *testing.T,
	dialect ddl.Dialect,
	lock migrate.Lock,
	variable string,
	driver string,
) {
	t.Helper()
	address := os.Getenv(variable)
	if address == "" {
		t.Skipf("set %s to take a real %s lock", variable, dialect.Name())
	}
	taking := lock.Take(dialect, "acceptance.Lock")
	if why := taking.Err(); why != nil {
		t.Fatal(why)
	}

	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[[]grant] {
		return sql.Open[effect.Unit](scope, driver, address).
			FlatMap(func(database *sql.Database) sqlEffect[[]grant] {
				return effect.RunCollect(
					sql.Rows[effect.Unit](database, grantedSchema, taking))
			})
	})

	within, giveUp := context.WithTimeout(context.Background(), 30*time.Second)
	defer giveUp()

	answered, ok := runtime.Run(within, effect.Unit{}, program).Value()
	if !ok {
		t.Fatalf("the lock statement was refused by %s", dialect.Name())
	}
	if len(answered) != 1 {
		t.Fatalf("expected one row back, got %d", len(answered))
	}
	// Freeing is the other half, and for Postgres it is deliberately nothing:
	// the transaction ending frees it.
	freeing := lock.Release(dialect, "acceptance.Lock")
	if dialect.Name() == "postgres" && freeing.Text() != "" {
		t.Fatalf("expected Postgres to free it with the transaction, got %q", freeing.Text())
	}
}

func TestOnARealServerThePostgresAdvisoryLockIsTaken(t *testing.T) {
	takeLock(t, ddl.Postgres, migrate.PostgresAdvisory, "EFFECT_GOLANG_POSTGRES_URL", "pgx")
}

func TestOnARealServerTheMySQLNamedLockIsTaken(t *testing.T) {
	takeLock(t, ddl.MySQL, migrate.MySQLNamed, "EFFECT_GOLANG_MYSQL_URL", "mysql")
}

// grant is whatever the lock statement answered with.
//
// Read as text rather than as a truth value, because the two servers answer
// differently -- Postgres's transaction lock answers an empty row and MySQL's
// answers one -- and what this establishes is that the statement ran, not what
// it said.
type grant struct {
	Answer string
}

var grantedSchema = schema.Struct[grant]("granted",
	schema.OptionalFieldOf("pg_advisory_xact_lock", schema.Text(),
		func(row grant) (string, bool) { return row.Answer, row.Answer != "" },
		func(row *grant, answer string) { row.Answer = answer }),
)
