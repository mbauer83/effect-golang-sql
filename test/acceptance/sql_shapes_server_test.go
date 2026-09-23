package acceptance

// The statement shapes, run by the servers they were spelled for.
//
// A unit test can say what each dialect writes; only a server can say that it
// accepts it. That matters most for the upsert clause, which is the one
// shape the three spell three ways -- Postgres and SQLite with a conflict
// target and an excluded row, MySQL with a duplicate-key clause and an alias
// for the row it was offered -- and next most for a cursor, whose comparison a
// server has to parse and plan.
//
// SQLite runs everywhere these tests run, so the shapes are established with
// it and the other two are gated on an address. What that division cannot
// cover is a dialect's own spelling, which is why the gated cases are named in
// CI rather than left to a variable somebody might forget to set.

import (
	"context"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// keyRow is the identity a database assigned, read back by the reference the
// caller does know.
type keyRow struct {
	ID int64
}

var keyedSchema = schema.Struct[keyRow]("Keyed",
	schema.FieldOf("id", schema.Int64(),
		func(held keyRow) int64 { return held.ID },
		func(held *keyRow, value int64) { held.ID = value }),
)

// palletLine is one line of a pallet, as the shapes read it back.
type palletLine struct {
	SKU      string
	Quantity int32
	Position int32
}

var linedSchema = schema.Struct[palletLine]("Lined",
	schema.FieldOf("sku", schema.Text(),
		func(held palletLine) string { return held.SKU },
		func(held *palletLine, value string) { held.SKU = value }),
	schema.FieldOf("quantity", schema.Int32(),
		func(held palletLine) int32 { return held.Quantity },
		func(held *palletLine, value int32) { held.Quantity = value }),
	schema.FieldOf("position", schema.Int32(),
		func(held palletLine) int32 { return held.Position },
		func(held *palletLine, value int32) { held.Position = value }),
)

// onATable runs the aggregate's DDL on one server and then the work.
//
// Dropped before it is created, because a service container is reused between
// tests in a job and a table left behind would fail the next run for a reason
// that has nothing to do with what it tests.
func onATable[A any](
	t *testing.T,
	dialect ddl.Dialect,
	driver string,
	source string,
	work func(*sql.Database) sqlEffect[A],
) effect.Exit[sql.Fault, A] {
	t.Helper()
	create, err := ddl.Create(dialect, warehouse.PalletSchema.Structure())
	if err != nil {
		t.Fatal(err)
	}
	drop, err := ddl.Drop(dialect, warehouse.PalletSchema.Structure())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := effect.NewRuntime(effect.WithDebugTracking())
	if err != nil {
		t.Fatal(err)
	}
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[A] {
		return sql.Open[effect.Unit](scope, driver, source).
			FlatMap(func(database *sql.Database) sqlEffect[A] {
				return executeAll(database, drop).
					AndThen(executeAll(database, create)).
					FlatMap(func(effect.Unit) sqlEffect[A] { return work(database) })
			})
	})
	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()
	return runtime.Run(within, effect.Unit{}, program)
}

func TestTheStatementShapesRunOnSQLite(t *testing.T) {
	checkRemaining(t, onATable(t, ddl.SQLite, "sqlite",
		"file:"+t.TempDir()+"/shapes.db",
		func(database *sql.Database) sqlEffect[[]palletLine] {
			return exercise(ddl.SQLite, database)
		}))
}

func TestTheStatementShapesRunOnPostgres(t *testing.T) {
	checkRemaining(t, onServer(t, ddl.Postgres, "EFFECT_GOLANG_POSTGRES_URL", "pgx"))
}

func TestTheStatementShapesRunOnMySQL(t *testing.T) {
	checkRemaining(t, onServer(t, ddl.MySQL, "EFFECT_GOLANG_MYSQL_URL", "mysql"))
}

func onServer(
	t *testing.T,
	dialect ddl.Dialect,
	variable string,
	driver string,
) effect.Exit[sql.Fault, []palletLine] {
	t.Helper()
	address := os.Getenv(variable)
	if address == "" {
		t.Skipf("set %s to run the shapes against a real %s", variable, dialect.Name())
	}
	return onATable(t, dialect, driver, address,
		func(database *sql.Database) sqlEffect[[]palletLine] {
			return exercise(dialect, database)
		})
}

// runQueries is the whole query specification against one gated server.
func runQueries(
	t *testing.T,
	dialect ddl.Dialect,
	variable string,
	driver string,
) effect.Exit[sql.Fault, palletSummary] {
	t.Helper()
	address := os.Getenv(variable)
	if address == "" {
		t.Skipf("set %s to run the query against a real %s", variable, dialect.Name())
	}
	return onATable(t, dialect, driver, address, summarise(dialect))
}
