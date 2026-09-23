package acceptance

// The migrator against a real database.
//
// This is the part nothing else could establish: that what has been applied is
// remembered, that running twice runs once, and that a failure leaves the
// ledger saying something true. So it runs, runs again, and then fails on
// purpose.

import (
	"context"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/migrate"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type migrateEffect[A any] = effect.Effect[effect.Unit, migrate.Fault, A]

// migrator opens one database and runs the work against it, keeping the file so
// that two migrations in one test see the same schema.
func migrator[A any](t *testing.T, work func(*sql.Database) migrateEffect[A]) effect.Exit[migrate.Fault, A] {
	t.Helper()
	runtime, err := effect.NewRuntime(effect.WithDebugTracking())
	if err != nil {
		t.Fatal(err)
	}
	source := "file:" + t.TempDir() + "/migrating.db"

	program := effect.Scoped(func(scope effect.Scope) migrateEffect[A] {
		return sql.Open[effect.Unit](scope, "sqlite", source).
			MapError(func(fault sql.Fault) migrate.Fault {
				return migrate.Fault{Op: "open", Err: fault}
			}).
			FlatMap(work)
	})

	within, giveUp := context.WithTimeout(context.Background(), 20*time.Second)
	defer giveUp()
	return runtime.Run(within, effect.Unit{}, program)
}

func planFor(target string) migrate.Plan {
	return migrate.Plan{
		Dialect: ddl.SQLite,
		History: warehouse.Pallets,
		Target:  target,
	}
}

func TestTheFirstMigrationCreatesTheTablesAndRecordsWhereItGotTo(t *testing.T) {
	// As of the target, not as of version one then stepping forward: a
	// database that starts at 2.0.0 has not skipped anything, it simply never
	// had 1.0.0 to alter.
	exit := migrator(t, func(database *sql.Database) migrateEffect[migrate.Report] {
		return migrate.Apply[effect.Unit](database, planFor("2.0.0"))
	})

	report, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if !report.Created {
		t.Error("expected the tables to have been created")
	}
	if report.From != "" || report.To != "2.0.0" {
		t.Errorf("unexpected report: %#v", report)
	}
}

func TestRunningTheSameMigrationTwiceRunsItOnce(t *testing.T) {
	// The property every migrator has to have. The second run reads the ledger,
	// finds it is already there, and does nothing at all.
	exit := migrator(t, func(database *sql.Database) migrateEffect[migrate.Report] {
		return apply(func(do *binder) migrate.Report {
			do.Await(migrate.Apply[effect.Unit](database, planFor("2.0.0")))
			return do.Await(migrate.Apply[effect.Unit](database, planFor("2.0.0")))
		})
	})

	report, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if !report.IsEmpty() {
		t.Fatalf("the second run did something: %#v", report)
	}
	if report.Created {
		t.Error("the second run created the tables again")
	}
}

func TestASecondMigrationStepsFromWhereTheFirstGotTo(t *testing.T) {
	// Across two evolutions, so the path matters: 2.0.0 to 3.0.0 passes through
	// 2.1.0, and each is recorded as it completes.
	exit := migrator(t, func(database *sql.Database) migrateEffect[migrate.Report] {
		return apply(func(do *binder) migrate.Report {
			do.Await(migrate.Apply[effect.Unit](database, planFor("2.0.0")))
			return do.Await(migrate.Apply[effect.Unit](database, planFor("3.0.0")))
		})
	})

	report, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if report.From != "2.0.0" || report.To != "3.0.0" {
		t.Errorf("unexpected report: %#v", report)
	}
	// Every version passed through, because each one is recorded and a ledger
	// that jumped would not say where a failure left things.
	if len(report.Versions) != 2 || report.Versions[0] != "2.1.0" || report.Versions[1] != "3.0.0" {
		t.Errorf("unexpected path: %v", report.Versions)
	}
	if report.Created {
		t.Error("expected an alteration rather than a creation")
	}
}

func TestTheLedgerIsReadableOnItsOwn(t *testing.T) {
	// A deployment wants to know where a database is without migrating it, and
	// asking should not fail merely because nothing has ever been migrated in.
	exit := migrator(t, func(database *sql.Database) migrateEffect[string] {
		return applyString(func(do *binder) string {
			before := do.Await(migrate.Current[effect.Unit](database, planFor("2.0.0")))
			if before != "" {
				t.Errorf("expected an empty database to hold nothing, got %q", before)
			}
			do.Await(migrate.Apply[effect.Unit](database, planFor("2.0.0")))
			return do.Await(migrate.Current[effect.Unit](database, planFor("2.0.0")))
		})
	})

	current, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if current != "2.0.0" {
		t.Errorf("unexpected version: %q", current)
	}
}

func TestAFailedStepLeavesTheLedgerSayingSomethingTrue(t *testing.T) {
	// 4.0.0 changes a column's type, which SQLite cannot do -- so the step is
	// refused. SQLite has transactional DDL, so the whole migration rolls back
	// and the ledger still says where the database really is. That is the
	// property a second run depends on.
	exit := migrator(t, func(database *sql.Database) migrateEffect[string] {
		return migrate.Apply[effect.Unit](database, planFor("3.0.0")).
			FlatMap(func(migrate.Report) migrateEffect[string] {
				return migrate.Apply[effect.Unit](database, planFor("4.0.0")).
					FlatMap(func(migrate.Report) migrateEffect[string] {
						return effect.For[effect.Unit, migrate.Fault]().
							Succeed("the refused step was applied")
					}).
					CatchAll(func(migrate.Fault) migrateEffect[string] {
						// Refused, as it should be. What matters is what the
						// ledger says now.
						return migrate.Current[effect.Unit](database, planFor("3.0.0"))
					})
			})
	})

	current, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if current != "3.0.0" {
		t.Fatalf("expected the ledger to still say 3.0.0, got %q", current)
	}
}

func TestMigratingBackwardsIsAllowedAndSaysSo(t *testing.T) {
	// Knowingly: some inverses cannot restore what they dropped. What the
	// migrator promises is that it runs them and records where it ended up.
	exit := migrator(t, func(database *sql.Database) migrateEffect[migrate.Report] {
		return apply(func(do *binder) migrate.Report {
			do.Await(migrate.Apply[effect.Unit](database, planFor("3.0.0")))
			return do.Await(migrate.Apply[effect.Unit](database, planFor("1.1.0")))
		})
	})

	report, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if report.From != "3.0.0" || report.To != "1.1.0" {
		t.Errorf("unexpected report: %#v", report)
	}
	// Three steps, not two: 3.0.0 to 1.1.0 passes through 2.1.0 and 2.0.0, and
	// each is recorded as it completes.
	if got := strings.Join(report.Versions, ","); got != "2.1.0,2.0.0,1.1.0" {
		t.Errorf("unexpected path: %s", got)
	}
}

func TestTheLedgerTableIsConfigurable(t *testing.T) {
	// Because a database may already have a table of that name, or a
	// convention of its own, or two applications sharing one schema.
	named := planFor("2.0.0")
	named.Ledger = "warehouse_versions"

	exit := migrator(t, func(database *sql.Database) migrateEffect[int64] {
		return applyCount(func(do *binder) int64 {
			do.Await(migrate.Apply[effect.Unit](database, named))
			held := do.Await(countRows(database, `warehouse_versions`))
			return held.Count
		})
	})

	rows, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if rows != 1 {
		t.Fatalf("expected the named ledger to hold one row, got %d", rows)
	}
}
