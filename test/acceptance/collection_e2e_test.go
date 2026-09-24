package acceptance

// A lazy collection against a real database. What matters is that the order a
// person puts it in is the order it reads in, whatever is inserted where and
// moved; that its keys are rewritten when they grow and the order survives;
// that one owner's collection is not another's; and that its keys are the
// database's to keep: a film twice is refused, a watchlist deleted takes its
// entries with it, and a film somebody lists cannot be deleted.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type listedFilm struct{ ID int64 }
type watchlist struct{ ID int64 }

var (
	listedFilmID  = schema.FieldAt("id", schema.Int64(), func(value *listedFilm) *int64 { return &value.ID }).Identity()
	listedFilms   = schema.Struct[listedFilm]("film", listedFilmID)
	watchlistID   = schema.FieldAt("id", schema.Int64(), func(value *watchlist) *int64 { return &value.ID }).Identity()
	watchlists    = schema.Struct[watchlist]("watchlist", watchlistID)
	filmsTable    = sql.Map(listedFilms)
	listsTable    = sql.Map(watchlists)
	watchedFilms  = schema.Lazy(watchlists, watchlistID, "films", schema.Ref(listedFilms, listedFilmID)).Ordered()
	watchlistRows = sql.CollectionOf(listsTable, watchedFilms, filmsTable)
)

type listEffect[A any] = effect.Effect[effect.Unit, sql.Fault, A]

// onCollection runs work against a database holding films 1 to 300 and two
// watchlists, 1 and 2, with foreign keys enforced.
func onCollection[A any](t *testing.T, work func(*sql.Database) listEffect[A]) (A, error) {
	t.Helper()
	var made []string
	for _, node := range []structure.Node{
		filmsTable.Schema().Structure(), listsTable.Schema().Structure(), watchlistRows.Structure(),
	} {
		statements, err := ddl.Create(ddl.SQLite, node)
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, statements...)
	}
	for id := 1; id <= 300; id++ {
		made = append(made, fmt.Sprintf(`INSERT INTO "film" VALUES (%d)`, id))
	}
	made = append(made, `INSERT INTO "watchlist" VALUES (1)`, `INSERT INTO "watchlist" VALUES (2)`)
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	program := effect.Scoped(func(scope effect.Scope) listEffect[A] {
		return sql.Open[effect.Unit](scope, "sqlite", "file:"+t.TempDir()+"/collected.db?_pragma=foreign_keys(1)&_pragma=synchronous(0)&_pragma=journal_mode(memory)").
			FlatMap(func(database *sql.Database) listEffect[A] {
				return effect.ForEach(made, func(statement string) listEffect[sql.Outcome] {
					return sql.Execute[effect.Unit](database, statement)
				}).FlatMap(func([]sql.Outcome) listEffect[A] { return work(database) })
			})
	})
	within, giveUp := context.WithTimeout(context.Background(), 30*time.Second)
	defer giveUp()
	exit := runtime.Run(within, effect.Unit{}, program)
	if value, ok := exit.Value(); ok {
		return value, nil
	}
	cause, _ := exit.Cause()
	fault, _ := cause.Failure()
	return *new(A), fault
}

type change = func(*sql.Database) listEffect[effect.Unit]

// steps runs changes in order and then reads owner's collection in its order.
func steps(database *sql.Database, owner int64, changes ...change) listEffect[[]int64] {
	return effect.ForEach(changes, func(each change) listEffect[effect.Unit] { return each(database) }).
		FlatMap(func([]effect.Unit) listEffect[[]int64] { return everyPage(database, owner, "", nil) })
}

// everyPage is owner's whole collection, read a page at a time.
func everyPage(database *sql.Database, owner int64, after sql.PageCursor, held []int64) listEffect[[]int64] {
	return watchlistRows.Listing(owner).Page[effect.Unit](database, ddl.SQLite, sql.PageQuery{After: after, Size: 100}).
		FlatMap(func(page sql.Page[int64]) listEffect[[]int64] {
			held = append(held, page.Items...)
			if page.Next.IsStart() {
				return effect.Succeed[effect.Unit, sql.Fault](held)
			}
			return everyPage(database, owner, page.Next, held)
		})
}

func insert(owner int64, film int64, at sql.Placement[int64]) change {
	return func(database *sql.Database) listEffect[effect.Unit] {
		return watchlistRows.Insert[effect.Unit](database, ddl.SQLite, owner, film, at)
	}
}

func move(owner int64, film int64, to sql.Placement[int64]) change {
	return func(database *sql.Database) listEffect[effect.Unit] {
		return watchlistRows.Move[effect.Unit](database, ddl.SQLite, owner, film, to)
	}
}

func TestACollectionReadsInTheOrderAPersonPutItIn(t *testing.T) {
	rows := watchlistRows
	order, err := onCollection(t, func(database *sql.Database) listEffect[[]int64] {
		return steps(database, 1,
			insert(1, 1, rows.Last()), insert(1, 2, rows.Last()), insert(1, 3, rows.Last()),
			insert(1, 4, rows.First()),
			insert(1, 5, rows.After(2)),
			insert(1, 6, rows.Before(1)),
			move(1, 3, rows.First()),
			func(database *sql.Database) listEffect[effect.Unit] {
				return rows.Remove[effect.Unit](database, ddl.SQLite, 1, 2)
			},
			insert(2, 9, rows.Last()),
		)
	})
	if err != nil {
		t.Fatal(err)
	}
	// 1 2 3; 4 first; 5 after 2; 6 before 1; 3 moved first; 2 removed. The
	// other owner's 9 is not here.
	if fmt.Sprint(order) != fmt.Sprint([]int64{3, 4, 6, 1, 5}) {
		t.Fatalf("expected [3 4 6 1 5], got %v", order)
	}
}

func TestAnOrderSurvivesItsKeysBeingRewritten(t *testing.T) {
	rows := watchlistRows
	type outcome struct {
		order   []int64
		longest int64
	}
	read, err := onCollection(t, func(database *sql.Database) listEffect[outcome] {
		changes := []change{insert(1, 1, rows.Last()), insert(1, 3, rows.Last())}
		// Each new film straight after the first, so every key halves the gap
		// the one before left, and the keys there grow until the owner's are
		// rewritten.
		for film := int64(10); film < 250; film++ {
			changes = append(changes, insert(1, film, rows.After(1)))
		}
		return steps(database, 1, changes...).
			FlatMap(func(order []int64) listEffect[outcome] {
				return sql.QueryRow[effect.Unit](database, longestKeySchema,
					`SELECT MAX(LENGTH("position")) AS "longest" FROM "watchlist_films"`).
					Map(func(longest longestKey) outcome { return outcome{order: order, longest: longest.Longest} })
			})
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{1}
	for film := int64(249); film >= 10; film-- {
		want = append(want, film)
	}
	want = append(want, 3)
	if fmt.Sprint(read.order) != fmt.Sprint(want) {
		t.Fatalf("expected the first, the rest newest first, then the last; got %v", read.order)
	}
	// Two hundred and forty halvings of one gap are some forty digits; the
	// keys are shorter only because they were rewritten.
	if read.longest > 32 {
		t.Fatalf("expected the keys rewritten, the longest is %d characters", read.longest)
	}
}

type longestKey struct{ Longest int64 }

var longestKeySchema = schema.Struct[longestKey]("",
	schema.FieldAt("longest", schema.Int64(), func(row *longestKey) *int64 { return &row.Longest }))

func TestACollectionsKeysAreTheDatabasesToKeep(t *testing.T) {
	rows := watchlistRows
	twice := func(database *sql.Database) listEffect[effect.Unit] {
		return insert(1, 1, rows.Last())(database).
			FlatMap(func(effect.Unit) listEffect[effect.Unit] { return insert(1, 1, rows.Last())(database) })
	}
	if _, err := onCollection(t, twice); !errors.Is(err, sql.ErrAlreadyThere) {
		t.Errorf("a film twice: expected it refused, got %v", err)
	}
	if _, err := onCollection(t, insert(1, 1, rows.After(8))); !errors.Is(err, sql.ErrNotInCollection) {
		t.Errorf("a place beside a film not listed: expected it refused, got %v", err)
	}
	kept, err := onCollection(t, func(database *sql.Database) listEffect[[2]bool] {
		return insert(1, 1, rows.Last())(database).
			FlatMap(func(effect.Unit) listEffect[sql.Outcome] {
				return sql.Execute[effect.Unit](database, `DELETE FROM "watchlist" WHERE "id" = 1`)
			}).
			FlatMap(func(sql.Outcome) listEffect[bool] { return rows.Has[effect.Unit](database, ddl.SQLite, 1, 1) }).
			FlatMap(func(stillListed bool) listEffect[[2]bool] {
				return insert(2, 5, rows.Last())(database).
					FlatMap(func(effect.Unit) listEffect[[2]bool] {
						return sql.Execute[effect.Unit](database, `DELETE FROM "film" WHERE "id" = 5`).
							Map(func(sql.Outcome) [2]bool { return [2]bool{stillListed, true} }).
							CatchAll(func(sql.Fault) listEffect[[2]bool] {
								return effect.Succeed[effect.Unit, sql.Fault]([2]bool{stillListed, false})
							})
					})
			})
	})
	if err != nil {
		t.Fatal(err)
	}
	if kept[0] {
		t.Error("expected a deleted watchlist's entries to go with it")
	}
	if kept[1] {
		t.Error("expected deleting a listed film to be refused")
	}
}
