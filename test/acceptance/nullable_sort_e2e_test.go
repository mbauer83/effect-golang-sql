package acceptance

// A listing sorted by a column that may be null, against real servers. What
// matters is that the nulls go where the ordering says on every server, and
// that pages read forwards and backwards across them neither repeat nor skip a
// row -- the nulls tie, so it is the key that orders them.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type reviewed struct {
	ID     int64
	Rating *int64
}

var (
	reviewedID     = schema.FieldAt("id", schema.Int64(), func(value *reviewed) *int64 { return &value.ID }).Identity()
	reviewedRating = schema.FieldAt("rating", schema.Nullable(schema.Int64()), func(value *reviewed) **int64 { return &value.Rating })
	reviews        = sql.NewRepository(sql.Map(schema.Struct[reviewed]("reviewed", reviewedID, reviewedRating)), reviewedID.Shape())
)

// walkPages is every page of a sort, two at a time, forwards and then
// backwards from the last page.
func walkPages(database *sql.Database, dialect ddl.Dialect, listing sql.Listing[reviewed], sort string) sqlEffect[[2][]int64] {
	var forwards func(sql.PageCursor, []int64, sql.PageCursor) sqlEffect[[2][]int64]
	var backwards func(sql.PageCursor, []int64, []int64) sqlEffect[[2][]int64]
	ids := func(page sql.Page[reviewed]) []int64 {
		held := make([]int64, 0, len(page.Items))
		for _, item := range page.Items {
			held = append(held, item.ID)
		}
		return held
	}
	backwards = func(before sql.PageCursor, read []int64, ahead []int64) sqlEffect[[2][]int64] {
		if before.IsStart() {
			return effect.Succeed[effect.Unit, sql.Fault]([2][]int64{ahead, read})
		}
		return listing.Page[effect.Unit](database, dialect, sql.PageQuery{Sort: sort, Before: before, Size: 2}).
			FlatMap(func(page sql.Page[reviewed]) sqlEffect[[2][]int64] {
				return backwards(page.Previous, append(ids(page), read...), ahead)
			})
	}
	forwards = func(after sql.PageCursor, read []int64, last sql.PageCursor) sqlEffect[[2][]int64] {
		return listing.Page[effect.Unit](database, dialect, sql.PageQuery{Sort: sort, After: after, Size: 2}).
			FlatMap(func(page sql.Page[reviewed]) sqlEffect[[2][]int64] {
				read = append(read, ids(page)...)
				if page.Next.IsStart() {
					// Back from the last page: its own rows, then every page before.
					return backwards(page.Previous, ids(page), read)
				}
				return forwards(page.Next, read, page.Previous)
			})
	}
	return forwards("", nil, "")
}

func nullableSortOn(t *testing.T, dialect ddl.Dialect, driver string, address string) {
	t.Helper()
	drop, _ := ddl.Drop(dialect, reviews.Structure())
	create, err := ddl.Create(dialect, reviews.Structure())
	if err != nil {
		t.Fatal(err)
	}
	rating := func(value int64) *int64 { return &value }
	rows := []reviewed{{1, rating(3)}, {2, nil}, {3, rating(5)}, {4, nil}, {5, rating(3)}, {6, rating(1)}, {7, nil}}
	listing := reviews.Listing().
		Sort("best", sql.Of[int64](reviews.Source(), "rating").Descending().NullsLast()).
		Sort("unrated", sql.Of[int64](reviews.Source(), "rating").Ascending().NullsFirst())
	runtime, _ := effect.NewRuntime()
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[[2][2][]int64] {
		return sql.Open[effect.Unit](scope, driver, address).FlatMap(func(database *sql.Database) sqlEffect[[2][2][]int64] {
			return executeAll(database, append(drop, create...)).
				FlatMap(func(effect.Unit) sqlEffect[[]sql.Outcome] {
					return effect.ForEach(rows, func(row reviewed) sqlEffect[sql.Outcome] {
						return reviews.Save[effect.Unit](database, dialect, row).As(sql.Outcome{})
					})
				}).
				FlatMap(func([]sql.Outcome) sqlEffect[[2][]int64] { return walkPages(database, dialect, listing, "best") }).
				FlatMap(func(best [2][]int64) sqlEffect[[2][2][]int64] {
					return walkPages(database, dialect, listing, "unrated").
						Map(func(unrated [2][]int64) [2][2][]int64 { return [2][2][]int64{best, unrated} })
				})
		})
	})
	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()
	walked, ok := runtime.Run(within, effect.Unit{}, program).Value()
	if !ok {
		t.Fatal(runtime.Run(within, effect.Unit{}, program))
	}
	// Best first, the unrated last and, tying, by the key the way the sort
	// ends: descending. Unrated first, then from the lowest, ties by key
	// ascending.
	for at, want := range []string{"[3 5 1 6 7 4 2]", "[2 4 7 6 1 5 3]"} {
		if fmt.Sprint(walked[at][0]) != want || fmt.Sprint(walked[at][1]) != want {
			t.Errorf("sort %d: expected %s forwards and backwards, got %v and %v", at, want, walked[at][0], walked[at][1])
		}
	}
}

func TestANullableSortPagesOnSQLite(t *testing.T) {
	nullableSortOn(t, ddl.SQLite, "sqlite", "file:"+t.TempDir()+"/reviewed.db")
}

func TestANullableSortPagesOnPostgres(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_POSTGRES_URL") == "" {
		t.Skip("set EFFECT_GOLANG_POSTGRES_URL to run against a real postgres")
	}
	nullableSortOn(t, ddl.Postgres, "pgx", os.Getenv("EFFECT_GOLANG_POSTGRES_URL"))
}

func TestANullableSortPagesOnMySQL(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_MYSQL_URL") == "" {
		t.Skip("set EFFECT_GOLANG_MYSQL_URL to run against a real mysql")
	}
	nullableSortOn(t, ddl.MySQL, "mysql", os.Getenv("EFFECT_GOLANG_MYSQL_URL"))
}
