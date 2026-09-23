package acceptance

// A query with every part of the specification in it, run by a real server.
//
// A unit test can say what each dialect writes. Only a server can say that it
// accepts it -- and a query with a common table expression, a left join, a
// group, a criterion over what the group aggregates, a window and a correlated
// subquery is where a specification that composed something almost right would
// show it.
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

// palletSummary is what the whole query answers with: one pallet, what is on it, and
// where the biggest line on it sits.
type palletSummary struct {
	Reference string
	Lines     int64
	Quantity  int64
	Longest   int64
	Ranked    int64
}

var summedSchema = schema.Struct[palletSummary]("Summed",
	schema.FieldOf("reference", schema.Text(),
		func(held palletSummary) string { return held.Reference },
		func(held *palletSummary, value string) { held.Reference = value }),
	schema.FieldOf("lines", schema.Int64(),
		func(held palletSummary) int64 { return held.Lines },
		func(held *palletSummary, value int64) { held.Lines = value }),
	schema.FieldOf("quantity", schema.Int64(),
		func(held palletSummary) int64 { return held.Quantity },
		func(held *palletSummary, value int64) { held.Quantity = value }),
	schema.FieldOf("longest", schema.Int64(),
		func(held palletSummary) int64 { return held.Longest },
		func(held *palletSummary, value int64) { held.Longest = value }),
	schema.FieldOf("ranked", schema.Int64(),
		func(held palletSummary) int64 { return held.Ranked },
		func(held *palletSummary, value int64) { held.Ranked = value }),
)

// summaryQuery is the query, built from the projection of the description the
// tables came from -- so every column in it is checked against what the
// description says, by name and by kind.
func summaryQuery(dialect ddl.Dialect, pallet int64) sql.SelectQuery {
	tables, err := ddl.Tables(dialect, warehouse.PalletSchema.Structure())
	if err != nil {
		return sql.SelectQuery{}
	}
	pallets := tables[0].Source().As("p")
	items := tables[1].Source().As("i")

	// A common table expression, because the totals it computes are filtered by
	// the query that reads them -- which the clause computing them cannot do.
	totals := sql.With("totals", sql.SelectQuery{
		Select: []sql.Selection{
			sql.Of[int64](items, "Pallet_id").As("pallet"),
			sql.Count().As("lines"),
			sql.Sum(sql.Of[int32](items, "quantity")).As("quantity"),
			sql.Max(sql.Of[int32](items, "quantity")).As("longest"),
		},
		From:    items,
		GroupBy: sql.Terms(sql.Of[int64](items, "Pallet_id")),
		// Over what it aggregates, not over a column: a pallet with one line
		// is not a pallet worth summarising.
		Having: sql.AtLeast(sql.Count(), sql.Param(int64(2))),
	})
	summary := totals.Source().As("s")

	return sql.SelectQuery{
		With:   []sql.CTE{totals},
		Select: summarySelection(pallets, summary),
		From:   pallets,
		Joins: []sql.Join{
			sql.LeftJoin(summary,
				sql.Equal(sql.Of[int64](summary, "pallet"), sql.Of[int64](pallets, "id"))),
		},
		Where:   sql.Equal(sql.Of[int64](pallets, "id"), sql.Param(pallet)),
		OrderBy: []sql.Ordering{sql.Of[string](pallets, "reference").Ascending()},
	}
}

func summarySelection(pallets sql.Source, summary sql.Source) []sql.Selection {
	return []sql.Selection{
		sql.Of[string](pallets, "reference").As("reference"),
		sql.Coalesce(sql.Of[int64](summary, "lines"), sql.Param(int64(0))).As("lines"),
		sql.Coalesce(sql.Of[int64](summary, "quantity"), sql.Param(int64(0))).As("quantity"),
		sql.Coalesce(sql.Of[int64](summary, "longest"), sql.Param(int64(0))).As("longest"),
		// A window, which keeps the row the aggregate would have collapsed:
		// where this pallet stands among the ones read, by how much is on it.
		sql.Count().Over(sql.Window{
			OrderBy: []sql.Ordering{sql.Of[int64](summary, "quantity").Descending()},
		}).As("ranked"),
	}
}

func TestTheWholeQuerySpecificationRunsOnSQLite(t *testing.T) {
	checkSummary(t, ddl.SQLite, onATable(t, ddl.SQLite, "sqlite",
		"file:"+t.TempDir()+"/query.db", summarise(ddl.SQLite)))
}

func TestTheWholeQuerySpecificationRunsOnPostgres(t *testing.T) {
	checkSummary(t, ddl.Postgres, runQueries(t, ddl.Postgres, "EFFECT_GOLANG_POSTGRES_URL", "pgx"))
}

func TestTheWholeQuerySpecificationRunsOnMySQL(t *testing.T) {
	checkSummary(t, ddl.MySQL, runQueries(t, ddl.MySQL, "EFFECT_GOLANG_MYSQL_URL", "mysql"))
}

// summarise writes a pallet with three lines on it and then asks the whole
// query about it.
func summarise(dialect ddl.Dialect) func(*sql.Database) sqlEffect[palletSummary] {
	return func(database *sql.Database) sqlEffect[palletSummary] {
		pallet := sql.InsertQuery{
			Table:   "Pallet",
			Columns: []string{"reference", "warehouse"},
			Values:  []dynamic.Value{sql.At("P-2"), sql.At("Kiel")},
		}
		return sql.Run[effect.Unit](database, pallet.Statement(dialect)).
			FlatMap(func(sql.Outcome) sqlEffect[keyRow] {
				return sql.Row[effect.Unit](database, keyedSchema, sql.SelectQuery{
					Select: sql.SelectColumns("id"),
					From:   sql.From("Pallet"),
					Where:  sql.ColumnEquals("reference", "P-2"),
				}.Statement(dialect))
			}).
			FlatMap(func(held keyRow) sqlEffect[palletSummary] {
				return stock(dialect, database, held.ID).
					FlatMap(func(effect.Unit) sqlEffect[palletSummary] {
						return sql.Row[effect.Unit](database, summedSchema,
							summaryQuery(dialect, held.ID).Statement(dialect))
					})
			})
	}
}

// stock puts three lines on the pallet, which is what makes the group large
// enough for the criterion over what it aggregates to keep it.
func stock(dialect ddl.Dialect, database *sql.Database, pallet int64) sqlEffect[effect.Unit] {
	return effect.ForEach([]palletLine{
		{SKU: "P2-BOLT", Quantity: 40, Position: 0},
		{SKU: "P2-NUT", Quantity: 80, Position: 1},
		{SKU: "P2-WASHER", Quantity: 120, Position: 2},
	}, func(line palletLine) sqlEffect[sql.Outcome] {
		return sql.Run[effect.Unit](database, lineInsert(pallet, line).Statement(dialect))
	}).As(effect.Unit{})
}

func checkSummary(t *testing.T, dialect ddl.Dialect, exit effect.Exit[sql.Fault, palletSummary]) {
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
