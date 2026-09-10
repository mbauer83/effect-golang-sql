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
		sql.Holds("tracking_id", sql.OfText),
		sql.Holds("owner_id", sql.OfText),
		sql.Holds("film_id", sql.OfWhole),
		sql.Holds("watchlisted_at", sql.OfMoment),
	).As("t")
	viewings = sql.From("film_viewing",
		sql.Holds("viewing_id", sql.OfText),
		sql.Holds("tracking_id", sql.OfText),
		sql.Holds("watched_at", sql.OfMoment),
		sql.Holds("minutes", sql.OfWhole),
	).As("v")
)

func TestAJoinRelatesTwoSourcesAndSaysWhichKindItIs(t *testing.T) {
	reading := sql.Reading{
		Select: sql.Selecting(sql.Of[int64](trackings, "film_id").Term()),
		From:   trackings,
		Joining: []sql.Join{
			sql.Including(viewings, sql.Matching(
				sql.Of[string](viewings, "tracking_id"),
				sql.Of[string](trackings, "tracking_id"))),
		},
		Where: sql.Matching(sql.Of[string](trackings, "owner_id"), sql.Bound("u")),
	}
	held := reading.Statement(ddl.Postgres)
	if held.Refused() != nil {
		t.Fatal(held.Refused())
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
	reading := sql.Reading{
		Select: []sql.Selection{
			film.Named("tracking_id"),
			sql.Counted().Named("viewings"),
			sql.Total(sql.Of[int64](viewings, "minutes")).Named("minutes"),
		},
		From:    viewings,
		Grouped: sql.Terms(film),
		Having:  sql.Above(sql.Counted(), sql.Bound(int64(2))),
		Ordered: []sql.Ordering{sql.Counted().Descending()},
	}
	held := reading.Statement(ddl.Postgres)
	if held.Refused() != nil {
		t.Fatal(held.Refused())
	}
	expected := `select "v"."tracking_id" as "tracking_id", count(*) as "viewings", ` +
		`cast(sum("v"."minutes") as bigint) as "minutes" from "film_viewing" "v" ` +
		`group by "v"."tracking_id" having count(*) > $1 order by count(*) desc`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
}

func TestAWindowIsPartitionedAndOrderedWithinEachPartition(t *testing.T) {
	reading := sql.Reading{
		Select: []sql.Selection{
			sql.Of[string](viewings, "viewing_id").Named("viewing_id"),
			sql.Counted().Over(sql.Window{
				Partitioned: sql.Terms(sql.Of[string](viewings, "tracking_id")),
				Ordered: []sql.Ordering{
					sql.Of[timeStub](viewings, "watched_at").Ascending(),
				},
			}).Named("so_far"),
		},
		From: viewings,
	}
	held := reading.Statement(ddl.Postgres)
	if held.Refused() != nil {
		t.Fatal(held.Refused())
	}
	expected := `select "v"."viewing_id" as "viewing_id", ` +
		`count(*) over (partition by "v"."tracking_id" order by "v"."watched_at" asc) ` +
		`as "so_far" from "film_viewing" "v"`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
}

func TestANamedExpressionIsReadByTheQueryThatNamedIt(t *testing.T) {
	counted := sql.Reading{
		Select: []sql.Selection{
			sql.Of[string](viewings, "tracking_id").Named("tracking_id"),
			sql.Counted().Named("viewings"),
		},
		From:    viewings,
		Grouped: sql.Terms(sql.Of[string](viewings, "tracking_id")),
	}
	seen := sql.Naming("seen", counted)
	reading := sql.Reading{
		With:   []sql.Expression{seen},
		Select: sql.Selected("tracking_id", "viewings"),
		From:   seen.Source(),
		// The whole reason a named expression is here: a value a select list
		// computes cannot be filtered in the clause that computes it, so it is
		// computed once and filtered by the reading that reads it.
		Where: sql.Above(sql.Of[int64](seen.Source(), "viewings"), sql.Bound(int64(1))),
	}
	held := reading.Statement(ddl.Postgres)
	if held.Refused() != nil {
		t.Fatal(held.Refused())
	}
	expected := `with "seen" as (select "v"."tracking_id" as "tracking_id", ` +
		`count(*) as "viewings" from "film_viewing" "v" group by "v"."tracking_id") ` +
		`select "tracking_id", "viewings" from "seen" where "viewings" > $1`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
}

func TestAReadingReadAsATableIsADerivedSource(t *testing.T) {
	inner := sql.Reading{
		Select: []sql.Selection{
			sql.Of[string](viewings, "tracking_id").Named("tracking_id"),
			sql.Largest(sql.Of[int64](viewings, "minutes")).Named("longest"),
		},
		From:    viewings,
		Grouped: sql.Terms(sql.Of[string](viewings, "tracking_id")),
	}
	longest := inner.Reads("longest")
	reading := sql.Reading{
		Select: sql.Selecting(sql.Of[int64](longest, "longest").Term()),
		From:   longest,
	}
	held := reading.Statement(ddl.SQLite)
	if held.Refused() != nil {
		t.Fatal(held.Refused())
	}
	if !strings.HasPrefix(held.Text(), `select "longest"."longest" from (select `) {
		t.Fatalf("unexpected statement: %s", held.Text())
	}
}

func TestOneValueAnotherReadingAnswersIsATermOfThisOne(t *testing.T) {
	// A correlated count, which is what two independent counts about one row
	// have to be: joined, the first would multiply the rows the second is
	// counted over.
	reading := sql.Reading{
		Select: []sql.Selection{
			sql.Of[int64](trackings, "film_id").Named("film_id"),
			sql.Answers[int64](sql.Reading{
				Select: sql.Selecting(sql.Counted().Term()),
				From:   viewings,
				Where: sql.Matching(
					sql.Of[string](viewings, "tracking_id"),
					sql.Of[string](trackings, "tracking_id")),
			}).Named("viewings"),
		},
		From: trackings,
	}
	held := reading.Statement(ddl.Postgres)
	if held.Refused() != nil {
		t.Fatal(held.Refused())
	}
	expected := `select "t"."film_id" as "film_id", ` +
		`(select count(*) from "film_viewing" "v" ` +
		`where "v"."tracking_id" = "t"."tracking_id") as "viewings" ` +
		`from "film_tracking" "t"`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
}
