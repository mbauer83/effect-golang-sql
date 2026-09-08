package acceptance

// A migration, run.
//
// The claim worth checking is the one a diff cannot make: a declared rename
// moves the column and the data stays in it. So this creates version one's
// table, puts a row in it, runs the statements that carry it to version two,
// and reads the row back under its new name. A drop-and-add -- which is all a
// diff could have written -- would have lost it.

import (
	"context"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// evolved opens a database, creates the table as of one version, and runs the
// work.
func evolved[A any](
	t *testing.T,
	at string,
	work func(*sql.Connected) building[A],
) effect.Exit[sql.Fault, A] {
	t.Helper()
	runtime, err := effect.NewRuntime(effect.WithDebugTracking())
	if err != nil {
		t.Fatal(err)
	}
	node, err := warehouse.Pallets.At(at)
	if err != nil {
		t.Fatal(err)
	}
	create, err := ddl.Create(ddl.SQLite, node)
	if err != nil {
		t.Fatal(err)
	}
	source := "file:" + t.TempDir() + "/evolving.db"

	program := effect.Scoped(func(scope effect.Scope) building[A] {
		return sql.Open[effect.Unit](scope, "sqlite", source).
			FlatMap(func(database *sql.Connected) building[A] {
				return executed(database, create).
					FlatMap(func(effect.Unit) building[A] { return work(database) })
			})
	})

	within, giveUp := context.WithTimeout(context.Background(), 10*time.Second)
	defer giveUp()
	return runtime.Run(within, effect.Unit{}, program)
}

func TestADeclaredRenameMovesTheColumnAndKeepsWhatWasInIt(t *testing.T) {
	statements, err := ddl.Alter(ddl.SQLite, warehouse.Pallets, "1.0.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}

	exit := evolved(t, "1.0.0", func(database *sql.Connected) building[warehouse.Sited] {
		return sql.Execute[effect.Unit](database,
			`insert into "Pallet" ("reference", "warehouse") values ('P-1', 'Kiel')`).
			FlatMap(func(sql.Outcome) building[effect.Unit] {
				// The migration itself.
				return executed(database, statements)
			}).
			FlatMap(func(effect.Unit) building[warehouse.Sited] {
				// Read under the new name. The row was written before the
				// column had this name, so its value being here is the whole
				// claim.
				return sql.QueryRow[effect.Unit](database, warehouse.SitedSchema,
					`select "site", "handling" from "Pallet" where "reference" = ?`,
					warehouse.Text("P-1"))
			})
	})

	sited, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if sited.Site != "Kiel" {
		t.Fatalf("the rename lost the column's contents: %#v", sited)
	}
	// And the added column got its default for the row that predated it, which
	// is what lets a not-null column be added to a table that has rows.
	if sited.Handling != "standard" {
		t.Errorf("expected the default for the existing row, got %q", sited.Handling)
	}
}

func TestTheStatementsGoBackAsWellAsForward(t *testing.T) {
	forward, err := ddl.Alter(ddl.SQLite, warehouse.Pallets, "1.0.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	backward, err := ddl.Alter(ddl.SQLite, warehouse.Pallets, "1.1.0", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	exit := evolved(t, "1.0.0", func(database *sql.Connected) building[warehouse.Stored] {
		return sql.Execute[effect.Unit](database,
			`insert into "Pallet" ("reference", "warehouse") values ('P-2', 'Kiel')`).
			FlatMap(func(sql.Outcome) building[effect.Unit] {
				return executed(database, forward)
			}).
			FlatMap(func(effect.Unit) building[effect.Unit] {
				return executed(database, backward)
			}).
			FlatMap(func(effect.Unit) building[warehouse.Stored] {
				// Back under the original name, with the value still in it:
				// the inverse of a rename is a rename, which is the one
				// inverse in the set that loses nothing.
				return sql.QueryRow[effect.Unit](database, warehouse.StoredSchema,
					`select "id", case when "warehouse" = 'Kiel' then 1 else 0 end as "dated"
					 from "Pallet" where "reference" = ?`,
					warehouse.Text("P-2"))
			})
	})

	stored, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if stored.Dated == 0 {
		t.Fatal("the round trip lost the column's contents")
	}
}

func TestTheMigratedValueAndTheMigratedTableAgree(t *testing.T) {
	// One declaration, two projections: the statements that move the table and
	// the function that moves a value. They come from the same changes, so a
	// value carried forward in memory is a value the migrated table accepts --
	// which is the property that would otherwise need two things kept in step
	// by hand.
	statements, err := ddl.Alter(ddl.SQLite, warehouse.Pallets, "1.0.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var held dynamic.Value = dynamic.Object{Fields: []dynamic.Field{
		{Name: "reference", Value: dynamic.OfText("P-3")},
		{Name: "warehouse", Value: dynamic.OfText("Bremen")},
	}}
	migrated, err := warehouse.Pallets.Migrate("1.0.0", "1.1.0", held)
	if err != nil {
		t.Fatal(err)
	}

	exit := evolved(t, "1.0.0", func(database *sql.Connected) building[warehouse.Sited] {
		return executed(database, statements).
			FlatMap(func(effect.Unit) building[sql.Outcome] {
				// Written with the migrated value's own members, in the
				// migrated table.
				return sql.Execute[effect.Unit](database,
					`insert into "Pallet" ("reference", "site", "handling")
					 values (?, ?, ?)`,
					member(migrated, "reference"),
					member(migrated, "site"),
					member(migrated, "handling"))
			}).
			FlatMap(func(sql.Outcome) building[warehouse.Sited] {
				return sql.QueryRow[effect.Unit](database, warehouse.SitedSchema,
					`select "site", "handling" from "Pallet" where "reference" = ?`,
					warehouse.Text("P-3"))
			})
	})

	sited, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if sited.Site != "Bremen" || sited.Handling != "standard" {
		t.Fatalf("unexpected row: %#v", sited)
	}
}

func member(value dynamic.Value, name string) dynamic.Value {
	held, present := value.(dynamic.Object).Member(name)
	if !present {
		return dynamic.Absent{}
	}
	return held
}
