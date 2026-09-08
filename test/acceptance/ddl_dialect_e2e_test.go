package acceptance

// The generated DDL against the two databases it was asked for.
//
// Gated on an address, because neither runs everywhere these tests run: set
// EFFECT_GOLANG_POSTGRES_URL or EFFECT_GOLANG_MYSQL_URL to run them. What only
// a real one can answer is whether the statements are statements it accepts --
// a generated-always identity, a datetime with a fractional default, an engine
// clause, a foreign key on a bounded varchar key. The derivation they share
// with SQLite is established without them.

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// accepted runs the aggregate's DDL against a real database and then uses it.
//
// The tables are dropped first and last: a service container is reused between
// tests in a job, and a schema left behind would make the second run fail for a
// reason that has nothing to do with what it is testing.
func accepted(
	t *testing.T,
	dialect ddl.Dialect,
	variable string,
	driver string,
) {
	t.Helper()
	address := os.Getenv(variable)
	if address == "" {
		t.Skipf("set %s to run this against a real %s", variable, dialect.Name())
	}

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
	program := effect.Scoped(func(scope effect.Scope) building[warehouse.Stored] {
		return sql.Open[effect.Unit](scope, driver, address).
			FlatMap(func(database *sql.Connected) building[warehouse.Stored] {
				return executed(database, drop).
					AndThen(executed(database, create)).
					FlatMap(func(effect.Unit) building[warehouse.Stored] {
						return used(dialect, database)
					})
			})
	})

	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()

	exit := runtime.Run(within, effect.Unit{}, program)
	stored, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	// The identity was assigned and the default applied, which is what the
	// description claimed and what only the database can confirm.
	if stored.ID < 1 {
		t.Errorf("expected a generated key, got %d", stored.ID)
	}
	if stored.Dated == 0 {
		t.Error("expected the default to have applied")
	}
}

// used writes a pallet and an item on it, and reads back what the database
// decided.
func used(dialect ddl.Dialect, database *sql.Connected) building[warehouse.Stored] {
	pallet := dialect.Quoted("Pallet")
	item := dialect.Quoted("PalletItem")
	return sql.Execute[effect.Unit](database,
		`insert into `+pallet+` (`+dialect.Quoted("reference")+`, `+
			dialect.Quoted("warehouse")+`) values ('P-1', 'Kiel')`).
		FlatMap(func(sql.Outcome) building[warehouse.Stored] {
			return sql.QueryRow[effect.Unit](database, warehouse.StoredSchema,
				`select `+dialect.Quoted("id")+`, case when `+dialect.Quoted("storedAt")+
					` is not null then 1 else 0 end as `+dialect.Quoted("dated")+
					` from `+pallet+` where `+dialect.Quoted("reference")+` = 'P-1'`)
		}).
		FlatMap(func(stored warehouse.Stored) building[warehouse.Stored] {
			// The child, on a bounded varchar key with a foreign key to a
			// generated one: the two type choices that most easily disagree.
			return sql.Execute[effect.Unit](database,
				`insert into `+item+` (`+dialect.Quoted("id")+`, `+dialect.Quoted("sku")+`, `+
					dialect.Quoted("quantity")+`, `+dialect.Quoted("Pallet_id")+`, `+
					dialect.Quoted("position")+`) values `+
					`('8f14e45f-ceea-467a-a4fb-1a9c73d0f2b1', 'BOLT-8', 40, `+
					stampedKey(stored)+`, 0)`).
				As(stored)
		})
}

func executed(database *sql.Connected, statements []string) building[effect.Unit] {
	return effect.ForEach(statements, func(statement string) building[sql.Outcome] {
		return sql.Execute[effect.Unit](database, statement)
	}).As(effect.Unit{})
}

func stampedKey(stored warehouse.Stored) string {
	return strconv.FormatInt(stored.ID, 10)
}

// migrated runs the aggregate's DDL, then the statements that carry it to the
// next version, then reads the renamed column back.
//
// The claim only a real database can settle: that these are statements it
// accepts, and that the rename kept what was in the column.
func migrated(t *testing.T, dialect ddl.Dialect, variable string, driver string) {
	t.Helper()
	address := os.Getenv(variable)
	if address == "" {
		t.Skipf("set %s to run this against a real %s", variable, dialect.Name())
	}

	first, err := warehouse.Pallets.At("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	create, err := ddl.Create(dialect, first)
	if err != nil {
		t.Fatal(err)
	}
	drop, err := ddl.Drop(dialect, first)
	if err != nil {
		t.Fatal(err)
	}
	alter, err := ddl.Alter(dialect, warehouse.Pallets, "1.0.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := effect.NewRuntime(effect.WithDebugTracking())
	if err != nil {
		t.Fatal(err)
	}
	program := effect.Scoped(func(scope effect.Scope) building[warehouse.Sited] {
		return sql.Open[effect.Unit](scope, driver, address).
			FlatMap(func(database *sql.Connected) building[warehouse.Sited] {
				return executed(database, drop).
					AndThen(executed(database, create)).
					AndThen(sql.Execute[effect.Unit](database,
						`insert into `+dialect.Quoted("Pallet")+` (`+
							dialect.Quoted("reference")+`, `+dialect.Quoted("warehouse")+
							`) values ('P-9', 'Kiel')`)).
					AndThen(executed(database, alter)).
					FlatMap(func(effect.Unit) building[warehouse.Sited] {
						return sql.QueryRow[effect.Unit](database, warehouse.SitedSchema,
							`select `+dialect.Quoted("site")+`, `+dialect.Quoted("handling")+
								` from `+dialect.Quoted("Pallet")+
								` where `+dialect.Quoted("reference")+` = 'P-9'`)
					})
			})
	})

	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()

	exit := runtime.Run(within, effect.Unit{}, program)
	sited, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	// The row was written before the column had this name, so its value being
	// here is the whole claim -- and a drop-and-add would have lost it.
	if sited.Site != "Kiel" {
		t.Errorf("the rename lost the column's contents: %#v", sited)
	}
	// And the row that predated the added column got its default.
	if sited.Handling != "standard" {
		t.Errorf("expected the default for the existing row, got %q", sited.Handling)
	}
}

func TestThePostgresSchemaIsOnePostgresAccepts(t *testing.T) {
	accepted(t, ddl.Postgres, "EFFECT_GOLANG_POSTGRES_URL", "pgx")
}

func TestThePostgresMigrationIsOnePostgresAccepts(t *testing.T) {
	migrated(t, ddl.Postgres, "EFFECT_GOLANG_POSTGRES_URL", "pgx")
}

func TestTheMySQLMigrationIsOneMySQLAccepts(t *testing.T) {
	migrated(t, ddl.MySQL, "EFFECT_GOLANG_MYSQL_URL", "mysql")
}

func TestTheMySQLSchemaIsOneMySQLAccepts(t *testing.T) {
	accepted(t, ddl.MySQL, "EFFECT_GOLANG_MYSQL_URL", "mysql")
}
