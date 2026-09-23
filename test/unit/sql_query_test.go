package unit

// A query with more than one source: joined, grouped, filtered on its groups,
// read over a window, and read from expressions it named itself.
//
// These are the parts a store could not say before and had to write as text,
// which is where a placeholder or an upsert clause used to get chosen. So what
// each case establishes is that the part is expressible *and* still spelled by
// the dialect.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// The two tables these cases read, as a projection would hand them over:
// columns, and what each of them holds.
var (
	trackings = sql.From("film_tracking",
		sql.ColumnOf("tracking_id", sql.OfText),
		sql.ColumnOf("owner_id", sql.OfText),
		sql.ColumnOf("film_id", sql.OfWhole),
		sql.ColumnOf("watchlisted_at", sql.OfMoment),
	).As("t")
	viewings = sql.From("film_viewing",
		sql.ColumnOf("viewing_id", sql.OfText),
		sql.ColumnOf("tracking_id", sql.OfText),
		sql.ColumnOf("watched_at", sql.OfMoment),
		sql.ColumnOf("minutes", sql.OfWhole),
	).As("v")
)

func TestAJoinRelatesTwoSourcesAndSaysWhichKindItIs(t *testing.T) {
	reading := sql.SelectQuery{
		Select: sql.SelectTerms(sql.Of[int64](trackings, "film_id").Term()),
		From:   trackings,
		Joins: []sql.Join{
			sql.LeftJoin(viewings, sql.Equal(
				sql.Of[string](viewings, "tracking_id"),
				sql.Of[string](trackings, "tracking_id"))),
		},
		Where: sql.Equal(sql.Of[string](trackings, "owner_id"), sql.Param("u")),
	}
	held := reading.Statement(ddl.Postgres)
	if held.Err() != nil {
		t.Fatal(held.Err())
	}
	expected := `select "t"."film_id" from "film_tracking" "t" ` +
		`left join "film_viewing" "v" on "v"."tracking_id" = "t"."tracking_id" ` +
		`where "t"."owner_id" = $1`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
	// A left join and not an inner one, and the difference is the whole
	// answer: an inner join loses every tracking with no viewing, silently.
	if strings.Contains(held.Text(), " join ") && !strings.Contains(held.Text(), " left join ") {
		t.Fatal("expected the join to be kept as a left join")
	}
}

func TestAGroupIsCollapsedAndFilteredOnWhatItAggregates(t *testing.T) {
	film := sql.Of[string](viewings, "tracking_id")
	reading := sql.SelectQuery{
		Select: []sql.Selection{
			film.As("tracking_id"),
			sql.Count().As("viewings"),
			sql.Sum(sql.Of[int64](viewings, "minutes")).As("minutes"),
		},
		From:    viewings,
		GroupBy: sql.Terms(film),
		Having:  sql.Above(sql.Count(), sql.Param(int64(2))),
		OrderBy: []sql.Ordering{sql.Count().Descending()},
	}
	held := reading.Statement(ddl.Postgres)
	if held.Err() != nil {
		t.Fatal(held.Err())
	}
	expected := `select "v"."tracking_id" as "tracking_id", count(*) as "viewings", ` +
		`cast(sum("v"."minutes") as bigint) as "minutes" from "film_viewing" "v" ` +
		`group by "v"."tracking_id" having count(*) > $1 order by count(*) desc`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
}

func TestAWindowIsPartitionedAndOrderedWithinEachPartition(t *testing.T) {
	reading := sql.SelectQuery{
		Select: []sql.Selection{
			sql.Of[string](viewings, "viewing_id").As("viewing_id"),
			sql.Count().Over(sql.Window{
				PartitionBy: sql.Terms(sql.Of[string](viewings, "tracking_id")),
				OrderBy: []sql.Ordering{
					sql.Of[timeStub](viewings, "watched_at").Ascending(),
				},
			}).As("so_far"),
		},
		From: viewings,
	}
	held := reading.Statement(ddl.Postgres)
	if held.Err() != nil {
		t.Fatal(held.Err())
	}
	expected := `select "v"."viewing_id" as "viewing_id", ` +
		`count(*) over (partition by "v"."tracking_id" order by "v"."watched_at" asc) ` +
		`as "so_far" from "film_viewing" "v"`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
}

func TestANamedExpressionIsReadByTheQueryThatNamedIt(t *testing.T) {
	counted := sql.SelectQuery{
		Select: []sql.Selection{
			sql.Of[string](viewings, "tracking_id").As("tracking_id"),
			sql.Count().As("viewings"),
		},
		From:    viewings,
		GroupBy: sql.Terms(sql.Of[string](viewings, "tracking_id")),
	}
	seen := sql.With("seen", counted)
	reading := sql.SelectQuery{
		With:   []sql.CTE{seen},
		Select: sql.SelectColumns("tracking_id", "viewings"),
		From:   seen.Source(),
		// The whole reason a common table expression is here: a value a select
		// list computes cannot be filtered in the clause that computes it, so
		// it is computed once and filtered by the query that reads it.
		Where: sql.Above(sql.Of[int64](seen.Source(), "viewings"), sql.Param(int64(1))),
	}
	held := reading.Statement(ddl.Postgres)
	if held.Err() != nil {
		t.Fatal(held.Err())
	}
	expected := `with "seen" as (select "v"."tracking_id" as "tracking_id", ` +
		`count(*) as "viewings" from "film_viewing" "v" group by "v"."tracking_id") ` +
		`select "tracking_id", "viewings" from "seen" where "viewings" > $1`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
}

func TestAReadingReadAsATableIsADerivedSource(t *testing.T) {
	inner := sql.SelectQuery{
		Select: []sql.Selection{
			sql.Of[string](viewings, "tracking_id").As("tracking_id"),
			sql.Max(sql.Of[int64](viewings, "minutes")).As("longest"),
		},
		From:    viewings,
		GroupBy: sql.Terms(sql.Of[string](viewings, "tracking_id")),
	}
	longest := inner.As("longest")
	reading := sql.SelectQuery{
		Select: sql.SelectTerms(sql.Of[int64](longest, "longest").Term()),
		From:   longest,
	}
	held := reading.Statement(ddl.SQLite)
	if held.Err() != nil {
		t.Fatal(held.Err())
	}
	if !strings.HasPrefix(held.Text(), `select "longest"."longest" from (select `) {
		t.Fatalf("unexpected statement: %s", held.Text())
	}
}

func TestOneValueAnotherReadingAnswersIsATermOfThisOne(t *testing.T) {
	// A correlated count, which is what two independent counts about one row
	// have to be: joined, the first would multiply the rows the second is
	// counted over.
	reading := sql.SelectQuery{
		Select: []sql.Selection{
			sql.Of[int64](trackings, "film_id").As("film_id"),
			sql.Answers[int64](sql.SelectQuery{
				Select: sql.SelectTerms(sql.Count().Term()),
				From:   viewings,
				Where: sql.Equal(
					sql.Of[string](viewings, "tracking_id"),
					sql.Of[string](trackings, "tracking_id")),
			}).As("viewings"),
		},
		From: trackings,
	}
	held := reading.Statement(ddl.Postgres)
	if held.Err() != nil {
		t.Fatal(held.Err())
	}
	expected := `select "t"."film_id" as "film_id", ` +
		`(select count(*) from "film_viewing" "v" ` +
		`where "v"."tracking_id" = "t"."tracking_id") as "viewings" ` +
		`from "film_tracking" "t"`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
}
