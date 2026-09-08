package acceptance

// What a row-moving function gets, and what it is bound by.
//
// The transaction the migration is running in -- so it can read what the
// migration has already done, write, and be rolled back with everything else.
// That is the whole reason it is a function and not a statement list.

import (
	"context"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/evolve"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/migrate"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

func TestARowMoverRunsInTheMigrationsOwnTransaction(t *testing.T) {
	// The reason it gets the transaction rather than a connection: what it
	// writes is committed or rolled back with everything else. So a mover that
	// fails after writing leaves nothing behind -- on a database with
	// transactional DDL, which SQLite has.
	//
	// It also proves the mover can *read*, which a statement list could do and
	// a value function could not: it sees the row the migration just added a
	// column to.
	seen := make(chan int, 4)
	failing := evolve.Rewritten{
		Doing: "counting the pallets and then failing",
		Adding: []evolve.Change{
			evolve.Added{Field: structure.Field{
				Name:    "counted",
				Node:    schema.MaxLength(schema.Text(), 8).Structure(),
				Default: structure.DefaultTo{Value: dynamic.OfText("")},
			}},
		},
		Forward: evolve.Rewrite{
			Value: func(value dynamic.Object) (dynamic.Object, error) { return value, nil },
			Rows: func(ctx context.Context, within sql.Querying) error {
				// Reads what the migration has already done, in the same
				// transaction: the column added a moment ago is there.
				cursor, err := within.Query(ctx,
					`select count(*) as "count" from "Pallet" where "counted" = ''`, nil)
				if err != nil {
					return err
				}
				defer func() { _ = cursor.Close() }()
				for cursor.Next() {
					row, err := cursor.Row()
					if err != nil {
						return err
					}
					held, _ := row.Member("count")
					if number, isNumber := held.(dynamic.Integer); isNumber {
						seen <- int(number.Value)
					}
				}
				if _, err := within.Execute(ctx,
					`update "Pallet" set "counted" = 'yes'`, nil); err != nil {
					return err
				}
				return errCounted
			},
		},
	}

	history := evolve.Of("logistics.Counting").
		Starting("1.0.0", warehouse.PalletSchema.Structure()).
		Then("1.1.0", failing)
	if err := history.Fault(); err != nil {
		t.Fatal(err)
	}
	plan := migrate.Plan{Dialect: ddl.SQLite, History: history, Target: "1.1.0"}

	exit := migrator(t, func(database *sql.Connected) moving[string] {
		return migrate.Apply[effect.Unit](database,
			migrate.Plan{Dialect: ddl.SQLite, History: history, Target: "1.0.0"}).
			FlatMap(func(migrate.Report) moving[sql.Outcome] {
				return sql.Execute[effect.Unit](database,
					`insert into "Pallet" ("reference", "warehouse")
					 values ('P-1', 'Kiel')`).MapError(migrating)
			}).
			FlatMap(func(sql.Outcome) moving[migrate.Report] {
				return migrate.Apply[effect.Unit](database, plan)
			}).
			FlatMap(func(migrate.Report) moving[string] {
				return effect.For[effect.Unit, migrate.Fault]().
					Succeed("the failing mover was allowed to finish")
			}).
			CatchAll(func(migrate.Fault) moving[string] {
				// Refused, as it should be. The ledger is what says whether
				// the writes went with it.
				return migrate.Current[effect.Unit](database, plan)
			})
	})

	current, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	// Rolled back with the rest: the version is still the one it started at.
	if current != "1.0.0" {
		t.Fatalf("expected the transaction to have taken the writes with it, got %q", current)
	}
	// And the mover did read the row, in the transaction, after the column it
	// reads was added -- which is the whole reason it gets the transaction.
	select {
	case counted := <-seen:
		if counted != 1 {
			t.Errorf("expected the mover to see one pallet, got %d", counted)
		}
	default:
		t.Error("the mover never ran, so it never read anything")
	}
}

var errCounted = errors.New("counted, and now failing on purpose")
