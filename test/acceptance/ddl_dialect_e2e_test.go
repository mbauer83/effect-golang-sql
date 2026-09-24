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
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// checkAccepted runs the aggregate's DDL against a real database and then uses it.
//
// The tables are dropped first and last: a service container is reused between
// tests in a job, and a schema left behind would make the second run fail for a
// reason that has nothing to do with what it is testing.
func checkAccepted(
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
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[warehouse.Receipt] {
		return sql.Open[effect.Unit](scope, driver, address).
			FlatMap(func(database *sql.Database) sqlEffect[warehouse.Receipt] {
				return executeAll(database, drop).
					AndThen(executeAll(database, create)).
					FlatMap(func(effect.Unit) sqlEffect[warehouse.Receipt] {
						return writePallet(dialect, database)
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
	if stored.HasDate == 0 {
		t.Error("expected the default to have applied")
	}
}

// writePallet writes a pallet and an item on it, and reads back what the database
// decided.
func writePallet(dialect ddl.Dialect, database *sql.Database) sqlEffect[warehouse.Receipt] {
	pallet := dialect.QuoteIdentifier("Pallet")
	item := dialect.QuoteIdentifier("PalletItem")
	return sql.Execute[effect.Unit](database,
		`INSERT INTO `+pallet+` (`+dialect.QuoteIdentifier("reference")+`, `+
			dialect.QuoteIdentifier("warehouse")+`) values ('P-1', 'Kiel')`).
		FlatMap(func(sql.Outcome) sqlEffect[warehouse.Receipt] {
			return sql.QueryRow[effect.Unit](database, warehouse.ReceiptSchema,
				`SELECT `+dialect.QuoteIdentifier("id")+`, case when `+dialect.QuoteIdentifier("storedAt")+
					` is not null then 1 else 0 end as `+dialect.QuoteIdentifier("hasDate")+
					` FROM `+pallet+` WHERE `+dialect.QuoteIdentifier("reference")+` = 'P-1'`)
		}).
		FlatMap(func(stored warehouse.Receipt) sqlEffect[warehouse.Receipt] {
			// The child, on a bounded varchar key with a foreign key to a
			// generated one: the two type choices that most easily disagree.
			// And bound rather than written into the text, which is the other
			// thing only a real database settles -- how it spells the values a
			// statement binds is the dialect's, and a statement composed with
			// the wrong spelling is refused by the server and by nothing else.
			return sql.Execute[effect.Unit](database,
				`INSERT INTO `+item+` (`+dialect.QuoteIdentifier("id")+`, `+dialect.QuoteIdentifier("sku")+`, `+
					dialect.QuoteIdentifier("quantity")+`, `+dialect.QuoteIdentifier("Pallet_id")+`, `+
					dialect.QuoteIdentifier("position")+`) values (`+
					bound(dialect, 5)+`)`,
				dynamic.OfText("8f14e45f-ceea-467a-a4fb-1a9c73d0f2b1"),
				dynamic.OfText("BOLT-8"),
				dynamic.OfInteger(40),
				dynamic.OfInteger(stored.ID),
				dynamic.OfInteger(0)).
				FlatMap(func(sql.Outcome) sqlEffect[warehouse.Receipt] {
					return readBack(dialect, database, item).As(stored)
				})
		})
}

// readBack reads the item by a bound key, so a placeholder is on both sides of
// what this establishes: one statement writing five values and one reading by
// one.
func readBack(
	dialect ddl.Dialect,
	database *sql.Database,
	item string,
) sqlEffect[warehouse.Item] {
	return sql.QueryRow[effect.Unit](database, warehouse.ItemSchema,
		`SELECT `+dialect.QuoteIdentifier("id")+`, `+dialect.QuoteIdentifier("sku")+`, `+
			dialect.QuoteIdentifier("quantity")+` FROM `+item+
			` WHERE `+dialect.QuoteIdentifier("sku")+` = `+dialect.Placeholder(1),
		dynamic.OfText("BOLT-8"))
}

// bound is the first count values a statement binds, spelled as this dialect
// spells them.
func bound(dialect ddl.Dialect, count int) string {
	said := make([]string, 0, count)
	for ordinal := 1; ordinal <= count; ordinal++ {
		said = append(said, dialect.Placeholder(ordinal))
	}
	return strings.Join(said, ", ")
}

func executeAll(database *sql.Database, statements []string) sqlEffect[effect.Unit] {
	return effect.ForEach(statements, func(statement string) sqlEffect[sql.Outcome] {
		return sql.Execute[effect.Unit](database, statement)
	}).As(effect.Unit{})
}

func stampedKey(stored warehouse.Receipt) string {
	return strconv.FormatInt(stored.ID, 10)
}

// checkMigration runs the aggregate's DDL, then the statements that carry it to the
// next version, then reads the renamed column back.
//
// The claim only a real database can settle: that these are statements it
// accepts, and that the rename kept what was in the column.
func checkMigration(t *testing.T, dialect ddl.Dialect, variable string, driver string) {
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
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[warehouse.SiteHandling] {
		return sql.Open[effect.Unit](scope, driver, address).
			FlatMap(func(database *sql.Database) sqlEffect[warehouse.SiteHandling] {
				return executeAll(database, drop).
					AndThen(executeAll(database, create)).
					AndThen(sql.Execute[effect.Unit](database,
						`INSERT INTO `+dialect.QuoteIdentifier("Pallet")+` (`+
							dialect.QuoteIdentifier("reference")+`, `+dialect.QuoteIdentifier("warehouse")+
							`) values ('P-9', 'Kiel')`)).
					AndThen(executeAll(database, alter)).
					FlatMap(func(effect.Unit) sqlEffect[warehouse.SiteHandling] {
						return sql.QueryRow[effect.Unit](database, warehouse.SiteHandlingSchema,
							`SELECT `+dialect.QuoteIdentifier("site")+`, `+dialect.QuoteIdentifier("handling")+
								` FROM `+dialect.QuoteIdentifier("Pallet")+
								` WHERE `+dialect.QuoteIdentifier("reference")+` = 'P-9'`)
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
	checkAccepted(t, ddl.Postgres, "EFFECT_GOLANG_POSTGRES_URL", "pgx")
}

func TestThePostgresMigrationIsOnePostgresAccepts(t *testing.T) {
	checkMigration(t, ddl.Postgres, "EFFECT_GOLANG_POSTGRES_URL", "pgx")
}

func TestTheMySQLMigrationIsOneMySQLAccepts(t *testing.T) {
	checkMigration(t, ddl.MySQL, "EFFECT_GOLANG_MYSQL_URL", "mysql")
}

func TestTheMySQLSchemaIsOneMySQLAccepts(t *testing.T) {
	checkAccepted(t, ddl.MySQL, "EFFECT_GOLANG_MYSQL_URL", "mysql")
}
