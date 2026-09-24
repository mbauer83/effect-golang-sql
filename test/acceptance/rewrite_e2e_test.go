package acceptance

// The change the closed four cannot express: a field split into two.
//
// Four changes derive their own value migration, because moving a member needs
// no function. Computing one does, so this one says how -- in both directions,
// and per dialect for the rows a database already holds. What is checked here
// is that the two halves agree: a row split by the statements and a value split
// by the function come out the same.

import (
	"testing"

	_ "modernc.org/sqlite"

	"strings"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/migrate"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// Split is what a pallet's reference became.
type Split struct {
	Prefix string
	Serial string
}

var splitSchema = schema.Struct[Split]("Split",
	schema.FieldOf("prefix", schema.Text(),
		func(held Split) string { return held.Prefix },
		func(held *Split, value string) { held.Prefix = value }),
	schema.FieldOf("serial", schema.Text(),
		func(held Split) string { return held.Serial },
		func(held *Split, value string) { held.Serial = value }),
)

// migrateFault names a database fault at this boundary, so a test reads one type.
func migrateFault(fault sql.Fault) migrate.Fault {
	return migrate.Fault{Op: "using the schema", Err: fault}
}

func contains(held string, wanted string) bool {
	return strings.Contains(held, wanted)
}

func TestASplitMovesTheRowsAndTheValueTheSameWay(t *testing.T) {
	exit := migrator(t, func(database *sql.Database) migrateEffect[Split] {
		return migrate.Apply[effect.Unit](database, planFor("3.0.0")).
			FlatMap(func(migrate.Report) migrateEffect[sql.Outcome] {
				return sql.Execute[effect.Unit](database,
					`INSERT INTO "Pallet" ("reference", "site", "handling")
					 VALUES ('KI-0001', 'Kiel', 'standard')`).
					MapError(migrateFault)
			}).
			FlatMap(func(sql.Outcome) migrateEffect[migrate.Report] {
				// The split, applied by the migrator: the structural statements
				// first, then the one that moves the rows.
				return migrate.Apply[effect.Unit](database, planFor("3.1.0"))
			}).
			FlatMap(func(migrate.Report) migrateEffect[Split] {
				return sql.QueryRow[effect.Unit](database, splitSchema,
					`SELECT "prefix", "serial" FROM "Pallet"`).
					MapError(migrateFault)
			})
	})

	row, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	// The statements moved the row that already existed, which is the half a
	// structural change could not have done.
	if row.Prefix != "KI" || row.Serial != "0001" {
		t.Fatalf("the rows were not split: %#v", row)
	}

	// And the function moves a value the same way, which is what makes the two
	// halves of one declaration agree.
	var held dynamic.Value = dynamic.Object{Fields: []dynamic.Field{
		{Name: "reference", Value: dynamic.OfText("KI-0001")},
		{Name: "site", Value: dynamic.OfText("Kiel")},
		{Name: "handling", Value: dynamic.OfText("standard")},
	}}
	moved, err := warehouse.Pallets.Migrate("3.0.0", "3.1.0", held)
	if err != nil {
		t.Fatal(err)
	}
	object := moved.(dynamic.Object)
	prefix, _ := object.Member("prefix")
	serial, _ := object.Member("serial")
	if prefix != dynamic.OfText("KI") || serial != dynamic.OfText("0001") {
		t.Fatalf("the value was not split the same way: %#v", object)
	}
	if _, gone := object.Member("reference"); gone {
		t.Error("expected the field that was split to be gone")
	}
}

func TestASplitGoesBackTheWayItSaysItDoes(t *testing.T) {
	// Back is a separate declaration and not a derivation, because joining is
	// not the inverse of splitting for every input -- a reference with no dash
	// went in as all serial, and comes back as all serial.
	var held dynamic.Value = dynamic.Object{Fields: []dynamic.Field{
		{Name: "prefix", Value: dynamic.OfText("KI")},
		{Name: "serial", Value: dynamic.OfText("0001")},
	}}
	back, err := warehouse.Pallets.Migrate("3.1.0", "3.0.0", held)
	if err != nil {
		t.Fatal(err)
	}
	object := back.(dynamic.Object)
	reference, present := object.Member("reference")
	if !present || reference != dynamic.OfText("KI-0001") {
		t.Fatalf("unexpected value: %#v", object)
	}

	// A reference that never had a prefix comes back as it went in, which is
	// the case a derived inverse would have got wrong.
	var plain dynamic.Value = dynamic.Object{Fields: []dynamic.Field{
		{Name: "prefix", Value: dynamic.OfText("")},
		{Name: "serial", Value: dynamic.OfText("0002")},
	}}
	rejoined, err := warehouse.Pallets.Migrate("3.1.0", "3.0.0", plain)
	if err != nil {
		t.Fatal(err)
	}
	held2, _ := rejoined.(dynamic.Object).Member("reference")
	if held2 != dynamic.OfText("0002") {
		t.Fatalf("unexpected value: %#v", held2)
	}
}

func TestARewritingIsNotAStatementListAndSaysSo(t *testing.T) {
	// Asking ddl for the SQL of a recomputation is asking for something that
	// does not exist: its rows are moved by a Go function, so there is no
	// statement list that is the whole of it. The refusal points at the thing
	// that can plan it.
	for name, dialect := range map[string]ddl.Dialect{
		"postgres": ddl.Postgres, "mysql": ddl.MySQL, "sqlite": ddl.SQLite,
	} {
		_, err := ddl.Alter(dialect, warehouse.Pallets, "3.0.0", "3.1.0")
		if err == nil {
			t.Errorf("%s: expected the projection to refuse a rewriting", name)
			continue
		}
		if !contains(err.Error(), "plan this with migrate") {
			t.Errorf("%s: expected the reason to say what to use, got %v", name, err)
		}
	}
}

func TestThePlanIsTheSameOnEveryDialectBecauseTheMoveIsGo(t *testing.T) {
	// The point of a row-mover being a function. As statements this change
	// would have been three declarations -- split_part, substring_index, substr
	// with instr -- each to be got right separately. As a function it is one,
	// and the only thing that differs between the dialects is how the columns
	// are added and dropped around it.
	var shape []string
	for name, dialect := range map[string]ddl.Dialect{
		"postgres": ddl.Postgres, "mysql": ddl.MySQL, "sqlite": ddl.SQLite,
	} {
		held, err := migrate.Actions(migrate.Plan{
			Dialect: dialect, History: warehouse.Pallets, Target: "3.1.0",
		}, "3.0.0", "3.1.0")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}

		// The shape of the plan: which actions are statements and which move
		// rows, in order. The columns arrive, the function moves the values,
		// and the column they came from goes -- and a plan in any other order
		// would be reading a column that is not there.
		of := make([]string, 0, len(held))
		for _, action := range held {
			if action.Rows != nil {
				of = append(of, "rows")
				continue
			}
			of = append(of, "sql")
		}
		if got := strings.Join(of, ","); got != "sql,sql,rows,sql" {
			t.Errorf("%s: unexpected plan: %s", name, got)
		}
		if shape == nil {
			shape = of
			continue
		}
		if strings.Join(of, ",") != strings.Join(shape, ",") {
			t.Errorf("%s: the plan differs by dialect: %v against %v", name, of, shape)
		}
	}
}
