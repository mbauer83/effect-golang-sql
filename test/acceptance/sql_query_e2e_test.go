package acceptance

// A query with every part of the specification in it, run by a real server.
//
// A unit test can say what each dialect writes. Only a server can say that it
// accepts it -- and a query with a named expression, a left join, a group, a
// criterion over what the group aggregates, a window and a correlated
// subquery is where a specification that composed something almost right
// would show it.
//
// SQLite runs everywhere these tests run, so the parts are established with
// it; the other two are gated on an address and named in CI.

import (
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// summed is what the whole query answers with: one pallet, what is on it, and
// where the biggest line on it sits.
type summed struct {
	Reference string
	Lines     int64
	Quantity  int64
	Longest   int64
	Ranked    int64
}

var summedSchema = schema.Struct[summed]("Summed",
	schema.FieldOf("reference", schema.Text(),
		func(held summed) string { return held.Reference },
		func(held *summed, value string) { held.Reference = value }),
	schema.FieldOf("lines", schema.Int64(),
		func(held summed) int64 { return held.Lines },
		func(held *summed, value int64) { held.Lines = value }),
	schema.FieldOf("quantity", schema.Int64(),
		func(held summed) int64 { return held.Quantity },
		func(held *summed, value int64) { held.Quantity = value }),
	schema.FieldOf("longest", schema.Int64(),
		func(held summed) int64 { return held.Longest },
		func(held *summed, value int64) { held.Longest = value }),
	schema.FieldOf("ranked", schema.Int64(),
		func(held summed) int64 { return held.Ranked },
		func(held *summed, value int64) { held.Ranked = value }),
)

// summarised is the query, built from the projection of the description the
// tables came from -- so every column in it is checked against what the
// description says, by name and by kind.
func summarised(dialect ddl.Dialect, pallet int64) sql.Reading {
	tables, err := ddl.Tables(dialect, warehouse.PalletSchema.Structure())
	if err != nil {
		return sql.Reading{}
	}
	pallets := tables[0].Source().As("p")
	items := tables[1].Source().As("i")

	// A named expression, because the totals it computes are filtered by the
	// reading that reads them -- which the clause computing them cannot do.
	totals := sql.Naming("totals", sql.Reading{
		Select: []sql.Selection{
			sql.Of[int64](items, "Pallet_id").Named("pallet"),
			sql.Counted().Named("lines"),
			sql.Total(sql.Of[int32](items, "quantity")).Named("quantity"),
			sql.Largest(sql.Of[int32](items, "quantity")).Named("longest"),
		},
		From:    items,
		Grouped: sql.Terms(sql.Of[int64](items, "Pallet_id")),
		// Over what it aggregates, not over a column: a pallet with one line
		// is not a pallet worth summarising.
		Having: sql.AtLeast(sql.Counted(), sql.Bound(int64(2))),
	})
	summary := totals.Source().As("s")

	return sql.Reading{
		With:   []sql.Expression{totals},
		Select: chosen(pallets, summary),
		From:   pallets,
		Joining: []sql.Join{
			sql.Including(summary,
				sql.Matching(sql.Of[int64](summary, "pallet"), sql.Of[int64](pallets, "id"))),
		},
		Where:   sql.Matching(sql.Of[int64](pallets, "id"), sql.Bound(pallet)),
		Ordered: []sql.Ordering{sql.Of[string](pallets, "reference").Ascending()},
	}
}

func chosen(pallets sql.Source, summary sql.Source) []sql.Selection {
	return []sql.Selection{
		sql.Of[string](pallets, "reference").Named("reference"),
		sql.Coalesced(sql.Of[int64](summary, "lines"), sql.Bound(int64(0))).Named("lines"),
		sql.Coalesced(sql.Of[int64](summary, "quantity"), sql.Bound(int64(0))).Named("quantity"),
		sql.Coalesced(sql.Of[int64](summary, "longest"), sql.Bound(int64(0))).Named("longest"),
		// A window, which keeps the row the aggregate would have collapsed:
		// where this pallet stands among the ones read, by how much is on it.
		sql.Counted().Over(sql.Window{
			Ordered: []sql.Ordering{sql.Of[int64](summary, "quantity").Descending()},
		}).Named("ranked"),
	}
}

func TestTheWholeQuerySpecificationRunsOnSQLite(t *testing.T) {
	summarisedIs(t, ddl.SQLite, onATable(t, ddl.SQLite, "sqlite",
		"file:"+t.TempDir()+"/query.db", summarising(ddl.SQLite)))
}

func TestTheWholeQuerySpecificationRunsOnPostgres(t *testing.T) {
	summarisedIs(t, ddl.Postgres, queried(t, ddl.Postgres, "EFFECT_GOLANG_POSTGRES_URL", "pgx"))
}

func TestTheWholeQuerySpecificationRunsOnMySQL(t *testing.T) {
	summarisedIs(t, ddl.MySQL, queried(t, ddl.MySQL, "EFFECT_GOLANG_MYSQL_URL", "mysql"))
}

// summarising writes a pallet with three lines on it and then asks the whole
// query about it.
func summarising(dialect ddl.Dialect) func(*sql.Connected) building[summed] {
	return func(database *sql.Connected) building[summed] {
		pallet := sql.Writing{
			Table:   "Pallet",
			Columns: []string{"reference", "warehouse"},
			Values:  []dynamic.Value{sql.At("P-2"), sql.At("Kiel")},
		}
		return sql.Run[effect.Unit](database, pallet.Statement(dialect)).
			FlatMap(func(sql.Outcome) building[keyed] {
				return sql.Row[effect.Unit](database, keyedSchema, sql.Reading{
					Select: sql.Selected("id"),
					From:   sql.From("Pallet"),
					Where:  sql.Equals("reference", "P-2"),
				}.Statement(dialect))
			}).
			FlatMap(func(held keyed) building[summed] {
				return stocked(dialect, database, held.ID).
					FlatMap(func(effect.Unit) building[summed] {
						return sql.Row[effect.Unit](database, summedSchema,
							summarised(dialect, held.ID).Statement(dialect))
					})
			})
	}
}

// stocked puts three lines on the pallet, which is what makes the group large
// enough for the criterion over what it aggregates to keep it.
func stocked(dialect ddl.Dialect, database *sql.Connected, pallet int64) building[effect.Unit] {
	return effect.ForEach([]lined{
		{SKU: "P2-BOLT", Quantity: 40, Position: 0},
		{SKU: "P2-NUT", Quantity: 80, Position: 1},
		{SKU: "P2-WASHER", Quantity: 120, Position: 2},
	}, func(line lined) building[sql.Outcome] {
		return sql.Run[effect.Unit](database, writingLine(pallet, line).Statement(dialect))
	}).As(effect.Unit{})
}

func summarisedIs(t *testing.T, dialect ddl.Dialect, exit effect.Exit[sql.Fault, summed]) {
	t.Helper()
	if exit.IsSuccess() {
		read, _ := exit.Value()
		// Three lines, so the group survives the criterion over what it
		// aggregates; the totals are the group's own; and the window ranked
		// the one row that was read rather than collapsing it.
		if read.Reference != "P-2" {
			t.Fatalf("expected the pallet, got %+v", read)
		}
		if read.Lines != 3 {
			t.Fatalf("expected its three lines, got %+v", read)
		}
		if read.Quantity != 240 || read.Longest != 120 {
			t.Fatalf("expected the group's own totals, got %+v", read)
		}
		if read.Ranked != 1 {
			t.Fatalf("expected the one row read to rank first, got %+v", read)
		}
		return
	}
	t.Fatalf("unexpected exit: %+v", exit)
}
