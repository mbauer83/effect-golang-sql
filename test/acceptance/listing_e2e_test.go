package acceptance

// A listing read a page at a time against a real database. What matters is
// that keyset pages neither repeat nor skip a row across ties, that a page
// read backwards is the page read forwards, that a numbered page is the same
// page, that a cursor is refused under another sort or filter, and that a
// listing is only ever of its owner's rows.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type entry struct {
	ID    int64
	Owner string
	Year  int64
}

var entryFields = struct {
	ID    schema.Field[entry, int64]
	Owner schema.Field[entry, string]
	Year  schema.Field[entry, int64]
}{
	ID:    schema.FieldAt("id", schema.Int64(), func(value *entry) *int64 { return &value.ID }).Identity(),
	Owner: schema.FieldAt("owner", schema.Text(), func(value *entry) *string { return &value.Owner }),
	Year:  schema.FieldAt("year", schema.Int64(), func(value *entry) *int64 { return &value.Year }),
}

var entries = sql.Map(schema.Struct[entry]("entry", entryFields.ID, entryFields.Owner, entryFields.Year))

// listed runs work against a database holding twelve of ann's entries -- the
// years repeating, so the sort ties -- and three of bob's.
func listed[A any](t *testing.T, work func(*sql.Database, sql.Listing[entry]) sqlEffect[A]) (A, error) {
	t.Helper()
	tables, err := ddl.Tables(ddl.SQLite, entries.Schema().Structure())
	if err != nil {
		t.Fatal(err)
	}
	made, err := ddl.Create(ddl.SQLite, entries.Schema().Structure())
	if err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 12; id++ {
		made = append(made, fmt.Sprintf(`INSERT INTO "entry" VALUES (%d, 'ann', %d)`, id, 2000+id%4))
	}
	for id := 13; id <= 15; id++ {
		made = append(made, fmt.Sprintf(`INSERT INTO "entry" VALUES (%d, 'bob', 2001)`, id))
	}
	source := tables[0].Source()
	listing := sql.NewListing(entries.Schema(), source, "id").
		Sort("year", sql.Of[int64](source, "year").Ascending()).
		Sort("recent", sql.Of[int64](source, "year").Descending()).
		PageSize(5, 6).
		MaxPage(3).
		IndexedBy(sql.DerivedIndex).
		Within(sql.Equal(sql.Of[string](source, "owner"), sql.Param("ann")))
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[A] {
		return sql.Open[effect.Unit](scope, "sqlite", "file:"+t.TempDir()+"/listed.db").
			FlatMap(func(database *sql.Database) sqlEffect[A] {
				return effect.ForEach(made, func(statement string) sqlEffect[sql.Outcome] {
					return sql.Execute[effect.Unit](database, statement)
				}).FlatMap(func([]sql.Outcome) sqlEffect[A] { return work(database, listing) })
			})
	})
	within, giveUp := context.WithTimeout(context.Background(), 10*time.Second)
	defer giveUp()
	exit := runtime.Run(within, effect.Unit{}, program)
	if value, ok := exit.Value(); ok {
		return value, nil
	}
	cause, _ := exit.Cause()
	fault, _ := cause.Failure()
	return *new(A), fault
}

func identities(items []entry) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func page(database *sql.Database, listing sql.Listing[entry], query sql.PageQuery) sqlEffect[sql.Page[entry]] {
	return listing.Page[effect.Unit](database, ddl.SQLite, query)
}

func TestKeysetPagesNeitherRepeatNorSkipARowAcrossTies(t *testing.T) {
	seen, err := listed(t, func(database *sql.Database, listing sql.Listing[entry]) sqlEffect[[]int64] {
		var walk func(sql.PageCursor, []int64) sqlEffect[[]int64]
		walk = func(after sql.PageCursor, held []int64) sqlEffect[[]int64] {
			return page(database, listing, sql.PageQuery{After: after}).
				FlatMap(func(read sql.Page[entry]) sqlEffect[[]int64] {
					held = append(held, identities(read.Items)...)
					if read.Next.IsStart() {
						return effect.Succeed[effect.Unit, sql.Fault](held)
					}
					return walk(read.Next, held)
				})
		}
		return walk("", nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Years 2000..2003 by id%4, ann's twelve only, each year's ids ascending.
	want := []int64{4, 8, 12, 1, 5, 9, 2, 6, 10, 3, 7, 11}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("expected every row once, in order\n\t%v\ngot\n\t%v", want, seen)
	}
}

func TestAPageReadBackwardsIsThePageReadForwards(t *testing.T) {
	pages, err := listed(t, func(database *sql.Database, listing sql.Listing[entry]) sqlEffect[[2][]int64] {
		return page(database, listing, sql.PageQuery{}).
			FlatMap(func(first sql.Page[entry]) sqlEffect[[2][]int64] {
				return page(database, listing, sql.PageQuery{After: first.Next}).
					FlatMap(func(second sql.Page[entry]) sqlEffect[[2][]int64] {
						return page(database, listing, sql.PageQuery{Before: second.Previous}).
							Map(func(again sql.Page[entry]) [2][]int64 {
								return [2][]int64{identities(first.Items), identities(again.Items)}
							})
					})
			})
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(pages[0]) != fmt.Sprint(pages[1]) {
		t.Fatalf("expected the first page back, got %v then %v", pages[0], pages[1])
	}
}

func TestANumberedPageIsTheSamePageAKeysetReadReaches(t *testing.T) {
	third, err := listed(t, func(database *sql.Database, listing sql.Listing[entry]) sqlEffect[sql.Page[entry]] {
		return page(database, listing, sql.PageQuery{Number: 3})
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(identities(third.Items)) != fmt.Sprint([]int64{7, 11}) || !third.Next.IsStart() || third.Previous.IsStart() {
		t.Fatalf("expected the last two rows, last and not first, got %+v", third)
	}
}

func TestACursorIsRefusedUnderAnotherSortOrFilter(t *testing.T) {
	_, err := listed(t, func(database *sql.Database, listing sql.Listing[entry]) sqlEffect[sql.Page[entry]] {
		return page(database, listing, sql.PageQuery{}).
			FlatMap(func(first sql.Page[entry]) sqlEffect[sql.Page[entry]] {
				return page(database, listing, sql.PageQuery{Sort: "recent", After: first.Next})
			})
	})
	if !errors.Is(err, sql.ErrPageCursor) {
		t.Fatalf("expected the cursor refused under another sort, got %v", err)
	}
}

func TestAPageTheListingDoesNotOfferIsRefused(t *testing.T) {
	for name, query := range map[string]sql.PageQuery{
		"an unknown sort":  {Sort: "title"},
		"a page too large": {Size: 7},
		"a page too deep":  {Number: 4},
		"two positions":    {After: "x", Number: 2},
	} {
		_, err := listed(t, func(database *sql.Database, listing sql.Listing[entry]) sqlEffect[sql.Page[entry]] {
			return page(database, listing, query)
		})
		if !errors.Is(err, sql.ErrPageQuery) {
			t.Errorf("%s: expected a refusal, got %v", name, err)
		}
	}
}

func TestAListingCountsItsOwnersRowsAndStopsWhereAsked(t *testing.T) {
	counts, err := listed(t, func(database *sql.Database, listing sql.Listing[entry]) sqlEffect[[2]int64] {
		return listing.Count[effect.Unit](database, ddl.SQLite, sql.Criterion{}).
			FlatMap(func(all int64) sqlEffect[[2]int64] {
				return listing.CountUpTo[effect.Unit](database, ddl.SQLite, sql.Criterion{}, 5).
					Map(func(capped int64) [2]int64 { return [2]int64{all, capped} })
			})
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts != [2]int64{12, 5} {
		t.Fatalf("expected ann's twelve, capped at five, got %v", counts)
	}
}

type planRow struct{ Detail string }

var planSchema = schema.Struct[planRow]("",
	schema.FieldAt("detail", schema.Text(), func(row *planRow) *string { return &row.Detail }))

func TestAListingsPageIsReadByTheIndexItDeclares(t *testing.T) {
	plans, err := listed(t, func(database *sql.Database, listing sql.Listing[entry]) sqlEffect[[2]string] {
		explain := ddl.Explain(ddl.SQLite, listing.Statement(ddl.SQLite, sql.PageQuery{}))
		plan := func() sqlEffect[string] {
			return effect.RunCollect(sql.Rows[effect.Unit](database, planSchema, explain)).
				Map(func(rows []planRow) string {
					joined := ""
					for _, row := range rows {
						joined += row.Detail + "; "
					}
					return joined
				})
		}
		return plan().FlatMap(func(before string) sqlEffect[[2]string] {
			made := ddl.CreateIndexes(ddl.SQLite, "entry", listing.Indexes()...)
			return effect.ForEach(made, func(statement string) sqlEffect[sql.Outcome] {
				return sql.Execute[effect.Unit](database, statement)
			}).FlatMap(func([]sql.Outcome) sqlEffect[[2]string] {
				return plan().Map(func(after string) [2]string { return [2]string{before, after} })
			})
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plans[0], "SCAN") {
		t.Fatalf("expected the page to scan without its index, got %s", plans[0])
	}
	if strings.Contains(plans[1], "SCAN") || !strings.Contains(plans[1], "entry_by_year") {
		t.Fatalf("expected the page read by its index, got %s", plans[1])
	}
}
