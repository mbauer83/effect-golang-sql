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

// migrating names a database fault at this boundary, so a test reads one type.
func migrating(fault sql.Fault) migrate.Fault {
	return migrate.Fault{Doing: "using the schema", Err: fault}
}

func contains(held string, wanted string) bool {
	return strings.Contains(held, wanted)
}

func TestASplitMovesTheRowsAndTheValueTheSameWay(t *testing.T) {
	exit := migrator(t, func(database *sql.Connected) moving[Split] {
		return migrate.Apply[effect.Unit](database, planFor("3.0.0")).
			FlatMap(func(migrate.Report) moving[sql.Outcome] {
				return sql.Execute[effect.Unit](database,
					`insert into "Pallet" ("reference", "site", "handling")
					 values ('KI-0001', 'Kiel', 'standard')`).
					MapError(migrating)
			}).
			FlatMap(func(sql.Outcome) moving[migrate.Report] {
				// The split, applied by the migrator: the structural statements
				// first, then the one that moves the rows.
				return migrate.Apply[effect.Unit](database, planFor("3.1.0"))
			}).
			FlatMap(func(migrate.Report) moving[Split] {
				return sql.QueryRow[effect.Unit](database, splitSchema,
					`select "prefix", "serial" from "Pallet"`).
					MapError(migrating)
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

func TestTheStatementsThatMoveTheRowsDifferByDialect(t *testing.T) {
	// Which is why they are declared per dialect: there is no dialect-neutral
	// way to say "the part before the dash", and inventing one would mean
	// picking a dialect's spelling and calling it neutral.
	for name, expected := range map[string]struct {
		dialect ddl.Dialect
		says    string
	}{
		"postgres": {dialect: ddl.Postgres, says: "split_part"},
		"mysql":    {dialect: ddl.MySQL, says: "substring_index"},
		"sqlite":   {dialect: ddl.SQLite, says: "instr"},
	} {
		statements, err := ddl.Alter(expected.dialect, warehouse.Pallets, "3.0.0", "3.1.0")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(statements) != 4 {
			t.Errorf("%s: unexpected statements: %v", name, statements)
			continue
		}
		// The ordering is the point, and it is the shape of the change rather
		// than something an author had to remember: the columns arrive, the
		// values move, and the one they came from goes last. A split that
		// dropped it first would be reading a column that is not there, which
		// is exactly what the first version of this did.
		at := func(wanted string) int {
			for index, statement := range statements {
				if contains(statement, wanted) {
					return index
				}
			}
			return -1
		}
		added, moved, dropped := at(`"serial"`), at(expected.says), at("drop column")
		if name == "mysql" {
			added = at("`serial`")
		}
		switch {
		case added < 0 || moved < 0 || dropped < 0:
			t.Errorf("%s: unexpected statements: %v", name, statements)
		case !(added < moved && moved < dropped):
			t.Errorf("%s: expected add %d before move %d before drop %d: %v",
				name, added, moved, dropped, statements)
		}
	}
}
