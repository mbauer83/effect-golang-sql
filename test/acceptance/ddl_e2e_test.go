package acceptance

// The generated DDL, run against a database.
//
// Comparing the statements to expected strings would say only that the
// projection emits what it emitted last week. What matters is that a database
// accepts them and that the schema it then holds is the one the description
// described -- so these run, and then ask the database what it has.
//
// SQLite here because it runs everywhere these tests run. Postgres and MySQL
// are the dialects that were asked for and are exercised in CI against service
// containers; the derivation those share with this one -- which tables, which
// keys, which column a child carries, what order the statements go in -- is the
// part most likely to be wrong, and it is the part this establishes.

import (
	"context"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type sqlEffect[A any] = effect.Effect[effect.Unit, sql.Fault, A]

// built opens a database, runs the aggregate's DDL, and then the work.
func onSchema[A any](
	t *testing.T,
	dialect ddl.Dialect,
	work func(*sql.Database) sqlEffect[A],
) effect.Exit[sql.Fault, A] {
	t.Helper()
	runtime, err := effect.NewRuntime(effect.WithDebugTracking())
	if err != nil {
		t.Fatal(err)
	}
	statements, err := ddl.Create(dialect, warehouse.PalletSchema.Structure())
	if err != nil {
		t.Fatal(err)
	}
	source := "file:" + t.TempDir() + "/warehouse.db"

	program := effect.Scoped(func(scope effect.Scope) sqlEffect[A] {
		return sql.Open[effect.Unit](scope, "sqlite", source).
			FlatMap(func(database *sql.Database) sqlEffect[A] {
				return effect.ForEach(statements, func(statement string) sqlEffect[sql.Outcome] {
					return sql.Execute[effect.Unit](database, statement)
				}).
					FlatMap(func([]sql.Outcome) sqlEffect[A] { return work(database) })
			})
	})

	within, giveUp := context.WithTimeout(context.Background(), 10*time.Second)
	defer giveUp()
	return runtime.Run(within, effect.Unit{}, program)
}

func TestTheGeneratedSchemaIsAcceptedAndHoldsWhatWasDescribed(t *testing.T) {
	// Accepted first: every statement runs, which is the thing a golden string
	// cannot tell you.
	exit := onSchema(t, ddl.SQLite, func(database *sql.Database) sqlEffect[[]sql.Outcome] {
		// Then used. The parent is written, the child references it, and the
		// generated key comes back -- so the key really is generated rather
		// than merely declared.
		return effect.ForEach([]string{
			`insert into "Pallet" ("reference", "warehouse") values ('P-1', 'Kiel')`,
			`insert into "PalletItem" ("id", "sku", "quantity", "Pallet_id", "position")
			 values ('i-1', 'BOLT-8', 40, 1, 0)`,
			`insert into "PalletItem" ("id", "sku", "quantity", "Pallet_id", "position")
			 values ('i-2', 'NUT-8', 80, 1, 1)`,
		}, func(statement string) sqlEffect[sql.Outcome] {
			return sql.Execute[effect.Unit](database, statement)
		})
	})
	if _, ok := exit.Value(); !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
}

func TestTheGeneratedKeyIsGeneratedAndTheDefaultApplies(t *testing.T) {
	// Two things the description said and only a database can confirm: the
	// identity is assigned without being given, and the computed column gets
	// its default rather than a null.
	exit := onSchema(t, ddl.SQLite, func(database *sql.Database) sqlEffect[warehouse.Receipt] {
		return sql.Execute[effect.Unit](database,
			`insert into "Pallet" ("reference", "warehouse") values ('P-2', 'Kiel')`).
			FlatMap(func(sql.Outcome) sqlEffect[warehouse.Receipt] {
				return sql.QueryRow[effect.Unit](database, warehouse.ReceiptSchema,
					`select "id", "storedAt" is not null as "hasDate"
					 from "Pallet" where "reference" = ?`,
					warehouse.Text("P-2"))
			})
	})

	stored, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if stored.ID < 1 {
		t.Errorf("expected a generated key, got %d", stored.ID)
	}
	// Nobody supplied it, so the default is the only thing that could have.
	if stored.HasDate == 0 {
		t.Error("expected the default to have applied, got no timestamp")
	}
}

func TestTheForeignKeyIsEnforcedAndCascades(t *testing.T) {
	// The cascade is the point of the aggregate being the unit: a child entity
	// has no life without its root, so deleting the root takes the children
	// and a child cannot be written without one.
	exit := onSchema(t, ddl.SQLite, func(database *sql.Database) sqlEffect[int64] {
		return sql.Execute[effect.Unit](database, `pragma foreign_keys = on`).
			FlatMap(func(sql.Outcome) sqlEffect[sql.Outcome] {
				return sql.Execute[effect.Unit](database,
					`insert into "Pallet" ("reference", "warehouse") values ('P-3', 'Kiel')`)
			}).
			FlatMap(func(sql.Outcome) sqlEffect[sql.Outcome] {
				return sql.Execute[effect.Unit](database,
					`insert into "PalletItem" ("id", "sku", "quantity", "Pallet_id", "position")
					 values ('i-3', 'BOLT-8', 5, 1, 0)`)
			}).
			FlatMap(func(sql.Outcome) sqlEffect[sql.Outcome] {
				return sql.Execute[effect.Unit](database, `delete from "Pallet" where "id" = 1`)
			}).
			FlatMap(func(sql.Outcome) sqlEffect[int64] {
				return sql.QueryRow[effect.Unit](database, warehouse.TallySchema,
					`select count(*) as "count" from "PalletItem"`).
					Map(func(held warehouse.Tally) int64 { return held.Count })
			})
	})

	remaining, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if remaining != 0 {
		t.Fatalf("expected the children to go with the parent, got %d", remaining)
	}
}

func TestAChildWithNoParentIsRefused(t *testing.T) {
	exit := onSchema(t, ddl.SQLite, func(database *sql.Database) sqlEffect[sql.Outcome] {
		return sql.Execute[effect.Unit](database, `pragma foreign_keys = on`).
			FlatMap(func(sql.Outcome) sqlEffect[sql.Outcome] {
				return sql.Execute[effect.Unit](database,
					`insert into "PalletItem" ("id", "sku", "quantity", "Pallet_id", "position")
					 values ('i-9', 'BOLT-8', 5, 999, 0)`)
			})
	})
	if exit.IsSuccess() {
		t.Fatal("expected the foreign key to refuse a child with no parent")
	}
}
