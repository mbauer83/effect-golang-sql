package acceptance

// A listing's searches against real servers. What matters is that each kind
// finds what it says -- a prefix in any case, every word of full text, a
// fragment anywhere -- for rows written before the search was made and after,
// changed and removed; that a kind a dialect has no index for is refused; and,
// on SQLite, that a search is read from its index.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type searchedFilm struct {
	ID       int64
	Name     string
	Overview string
}

var searchedFilms = sql.Map(schema.Struct[searchedFilm]("searched_film",
	schema.FieldAt("id", schema.Int64(), func(film *searchedFilm) *int64 { return &film.ID }).Identity(),
	schema.FieldAt("name", schema.Text(), func(film *searchedFilm) *string { return &film.Name }),
	schema.FieldAt("overview", schema.Text(), func(film *searchedFilm) *string { return &film.Overview })))

var (
	byName     = sql.NewSearch("name", sql.PrefixMatch, "name")
	byWords    = sql.NewSearch("words", sql.FullTextMatch, "name", "overview")
	byFragment = sql.NewSearch("fragment", sql.SubstringMatch, "name")
)

// searchFound is what each search found, by identity.
type searchFound struct {
	prefix, words, fragment []int64
	counted                 int64
	plan                    string
	// restored says the table took a row without its columns named once the
	// searches were dropped: it has no column of theirs left.
	restored bool
}

// searched makes the table and its searches on a dialect, writes films before
// the searches and changes them after, and runs each search.
func searched(t *testing.T, dialect ddl.Dialect, driver string, address string) (searchFound, error) {
	t.Helper()
	tables, err := ddl.Tables(dialect, searchedFilms.Schema().Structure())
	if err != nil {
		t.Fatal(err)
	}
	source := tables[0].Source()
	searches := []sql.Search{byName, byWords}
	if dialect.Name() == ddl.Postgres.Name() {
		searches = append(searches, byFragment)
	}
	listing := sql.NewListing(searchedFilms.Schema(), source, "id").
		Sort("id", sql.Of[int64](source, "id").Ascending()).
		WithSearch(searches...)
	drop, _ := ddl.Drop(dialect, searchedFilms.Schema().Structure())
	create, _ := ddl.Create(dialect, searchedFilms.Schema().Structure())
	made, err := ddl.CreateSearches(dialect, "searched_film", listing.Searches()...)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(id int, name string, overview string) string {
		// The columns named, because a search's generated column is one more.
		return fmt.Sprintf("INSERT INTO %s (%s, %s, %s) VALUES (%d, '%s', '%s')", dialect.QuoteIdentifier("searched_film"),
			dialect.QuoteIdentifier("id"), dialect.QuoteIdentifier("name"), dialect.QuoteIdentifier("overview"), id, name, overview)
	}
	film := dialect.QuoteIdentifier("searched_film")
	statements := append(append(append(drop, create...),
		insert(1, "Alien", "A ship crew meets an alien"),
		insert(2, "Aliens", "Marines return to the alien planet"),
		insert(3, "Blade Runner", "A replicant hunter"),
		insert(4, "ALIEN³", "A prison planet, and an alien")),
		made...)
	statements = append(statements,
		insert(5, "Arrival", "Linguists meet an alien from another planet"),
		"UPDATE "+film+" SET "+dialect.QuoteIdentifier("overview")+" = 'An alien planet, hunted' WHERE "+dialect.QuoteIdentifier("id")+" = 3",
		"DELETE FROM "+film+" WHERE "+dialect.QuoteIdentifier("id")+" = 2")
	found := func(database *sql.Database, criterion sql.Criterion) sqlEffect[[]int64] {
		return listing.Page[effect.Unit](database, dialect, sql.PageQuery{Where: criterion}).
			Map(func(page sql.Page[searchedFilm]) []int64 {
				ids := make([]int64, 0, len(page.Items))
				for _, item := range page.Items {
					ids = append(ids, item.ID)
				}
				return ids
			})
	}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[searchFound] {
		return sql.Open[effect.Unit](scope, driver, address).
			FlatMap(func(database *sql.Database) sqlEffect[searchFound] {
				var result searchFound
				return executeAll(database, statements).
					FlatMap(func(effect.Unit) sqlEffect[[]int64] { return found(database, listing.Match("name", "ali")) }).
					FlatMap(func(ids []int64) sqlEffect[[]int64] {
						result.prefix = ids
						return found(database, listing.Match("words", "Planet, ALIEN!"))
					}).
					FlatMap(func(ids []int64) sqlEffect[[]int64] {
						result.words = ids
						if dialect.Name() != ddl.Postgres.Name() {
							return effect.Succeed[effect.Unit, sql.Fault]([]int64(nil))
						}
						return found(database, listing.Match("fragment", "LIE"))
					}).
					FlatMap(func(ids []int64) sqlEffect[int64] {
						result.fragment = ids
						return listing.Count[effect.Unit](database, dialect, listing.Match("words", "alien"))
					}).
					FlatMap(func(counted int64) sqlEffect[string] {
						result.counted = counted
						if dialect.Name() != ddl.SQLite.Name() {
							return effect.Succeed[effect.Unit, sql.Fault]("")
						}
						explain := func(name string, text string) sqlEffect[string] {
							return planOf(database, ddl.Explain(dialect,
								listing.Statement(dialect, sql.PageQuery{Where: listing.Match(name, text)})))
						}
						return effect.ForEach([][2]string{{"name", "ali"}, {"words", "alien"}},
							func(asked [2]string) sqlEffect[string] { return explain(asked[0], asked[1]) }).
							Map(func(plans []string) string { return strings.Join(plans, "\n") })
					}).
					FlatMap(func(plan string) sqlEffect[effect.Unit] {
						result.plan = plan
						return executeAll(database, append(ddl.DropSearches(dialect, "searched_film", listing.Searches()...),
							fmt.Sprintf("INSERT INTO %s VALUES (6, 'Solaris', 'An ocean')", film)))
					}).
					Map(func(effect.Unit) searchFound { result.restored = true; return result })
			})
	})
	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()
	exit := runtime.Run(within, effect.Unit{}, program)
	if value, ok := exit.Value(); ok {
		return value, nil
	}
	cause, _ := exit.Cause()
	fault, _ := cause.Failure()
	return searchFound{}, fault
}

type planLine struct{ Detail string }

var planLineSchema = schema.Struct[planLine]("",
	schema.FieldAt("detail", schema.Text(), func(line *planLine) *string { return &line.Detail }))

// planOf is SQLite's query plan, one line per step.
func planOf(database *sql.Database, statement sql.Statement) sqlEffect[string] {
	return effect.RunCollect(sql.Query[effect.Unit](database, planLineSchema, statement.Text(), statement.Values()...)).
		Map(func(lines []planLine) string {
			details := make([]string, 0, len(lines))
			for _, line := range lines {
				details = append(details, line.Detail)
			}
			return strings.Join(details, "\n")
		})
}

func checkSearches(t *testing.T, dialect ddl.Dialect, result searchFound, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	// 2 was deleted; 3 now mentions an alien planet and 5 was written after the
	// search was made.
	if fmt.Sprint(result.prefix) != "[1 4]" {
		t.Errorf("a prefix in any case: expected [1 4], got %v", result.prefix)
	}
	if fmt.Sprint(result.words) != "[3 4 5]" {
		t.Errorf("every word: expected [3 4 5], got %v", result.words)
	}
	if result.counted != 4 {
		t.Errorf("a count of the rows a search finds: expected 4, got %d", result.counted)
	}
	if !result.restored {
		t.Error("expected the table as it was once its searches were dropped")
	}
	if dialect.Name() == ddl.Postgres.Name() && fmt.Sprint(result.fragment) != "[1 4]" {
		t.Errorf("a fragment anywhere: expected [1 4], got %v", result.fragment)
	}
}

func TestSearchesFindWhatTheySayOnSQLite(t *testing.T) {
	result, err := searched(t, ddl.SQLite, "sqlite", "file:"+t.TempDir()+"/searched.db")
	checkSearches(t, ddl.SQLite, result, err)
	if !strings.Contains(result.plan, "USING INDEX searched_film_name_search") || !strings.Contains(result.plan, "VIRTUAL TABLE") {
		t.Errorf("expected the prefix read from its index and the words from the FTS5 table, the plan is\n%s", result.plan)
	}
}

func TestSearchesFindWhatTheySayOnPostgres(t *testing.T) {
	address := os.Getenv("EFFECT_GOLANG_POSTGRES_URL")
	if address == "" {
		t.Skip("set EFFECT_GOLANG_POSTGRES_URL to run the searches against a real postgres")
	}
	result, err := searched(t, ddl.Postgres, "pgx", address)
	checkSearches(t, ddl.Postgres, result, err)
}

func TestSearchesFindWhatTheySayOnMySQL(t *testing.T) {
	address := os.Getenv("EFFECT_GOLANG_MYSQL_URL")
	if address == "" {
		t.Skip("set EFFECT_GOLANG_MYSQL_URL to run the searches against a real mysql")
	}
	result, err := searched(t, ddl.MySQL, "mysql", address)
	checkSearches(t, ddl.MySQL, result, err)
}

func TestASearchNoIndexServesIsRefused(t *testing.T) {
	for _, dialect := range []ddl.Dialect{ddl.SQLite, ddl.MySQL} {
		if _, err := ddl.CreateSearches(dialect, "searched_film", byFragment); err == nil {
			t.Errorf("%s: expected a substring search refused", dialect.Name())
		}
	}
	tables, _ := ddl.Tables(ddl.SQLite, searchedFilms.Schema().Structure())
	source := tables[0].Source()
	listing := sql.NewListing(searchedFilms.Schema(), source, "id").
		Sort("id", sql.Of[int64](source, "id").Ascending()).
		WithSearch(byFragment)
	statement := listing.Statement(ddl.SQLite, sql.PageQuery{Where: listing.Match("fragment", "lie")})
	if err := statement.Err(); err == nil || !strings.Contains(err.Error(), "no index for a substring search") {
		t.Errorf("expected the query refused for want of an index, got %v", err)
	}
	unknown := listing.Statement(ddl.SQLite, sql.PageQuery{Where: listing.Match("title", "x")})
	if err := unknown.Err(); !errors.Is(err, sql.ErrPageQuery) {
		t.Errorf("expected a search nobody declared refused as the client's mistake, got %v", err)
	}
}
