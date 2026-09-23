package unit

// Paging, which is the one part of a query whose correctness is not visible in
// the SQL.
//
// A cursor over two columns needs a lexicographic comparison: one on the
// leading column alone repeats the rows tying with the page boundary, and one
// on both columns unconditionally skips the rows past it. Neither shows up
// unless a fixture ties, so these are stated over what the comparison permits
// rather than over what one page happened to return.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

func pageStatement(order []sql.Ordering, at ...dynamic.Value) sql.Statement {
	return sql.SelectQuery{
		Select:  sql.SelectColumns("film_id"),
		From:    sql.From("film_tracking"),
		OrderBy: order,
		After:   at,
		Limit:   40,
	}.Statement(ddl.Postgres)
}

func TestACursorComparesEveryColumnTheListIsOrderedBy(t *testing.T) {
	held := pageStatement(
		[]sql.Ordering{
			sql.Column[string]("watchlisted_at").Descending(),
			sql.Column[int64]("film_id").Descending(),
		},
		sql.At("2026-01-01"), sql.At(int64(1)))
	expected := `select "film_id" from "film_tracking" where ` +
		`"watchlisted_at" < $1 or ("watchlisted_at" = $2 and "film_id" < $3) ` +
		`order by "watchlisted_at" desc, "film_id" desc limit 40`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
	// The boundary's leading value is compared twice, so it is bound twice --
	// which is why the values come out of the same rendering that wrote the
	// text rather than from a list the caller kept beside it.
	if len(held.Values()) != 3 {
		t.Fatalf("expected three bound values, got %d", len(held.Values()))
	}
}

func TestACursorFollowsWhicheverWayTheListIsRead(t *testing.T) {
	for _, expected := range []struct {
		named string
		order sql.Ordering
		sign  string
	}{
		{named: "descending", order: sql.Column[string]("added_at").Descending(), sign: "<"},
		{named: "ascending", order: sql.Column[string]("added_at").Ascending(), sign: ">"},
	} {
		t.Run(expected.named, func(t *testing.T) {
			held := pageStatement([]sql.Ordering{expected.order}, sql.At("2026-01-01")).Text()
			if !strings.Contains(held, `"added_at" `+expected.sign+` $1`) {
				t.Fatalf("expected a %s comparison, got %s", expected.sign, held)
			}
		})
	}
}

func TestAMixedOrderCursorComparesEachColumnItsOwnWay(t *testing.T) {
	// A list sorted one column up and another down has no row-value form at
	// all, which is why the comparison is written out a column at a time.
	held := pageStatement(
		[]sql.Ordering{
			sql.Column[string]("title").Ascending(),
			sql.Column[int64]("year").Descending(),
		},
		sql.At("Heat"), sql.At(int64(1995))).Text()
	if !strings.Contains(held, `"title" > $1`) || !strings.Contains(held, `"year" < $3`) {
		t.Fatalf("expected each column compared its own way, got %s", held)
	}
}

func TestACursorAtTheStartOfAListIsNoCriterionAtAll(t *testing.T) {
	held := pageStatement([]sql.Ordering{sql.Column[string]("watchlisted_at").Descending()})
	if strings.Contains(held.Text(), "where") {
		t.Fatalf("expected no where clause on a first page, got %s", held.Text())
	}
	if len(held.Values()) != 0 {
		t.Fatal("expected a first page to bind nothing")
	}
}

func TestACursorIsAndedIntoWhateverElseTheQueryAsks(t *testing.T) {
	// The clause a page adds and the clause a caller asked for are one
	// criterion, and the page's disjunction is bracketed inside it -- which is
	// where leaving a bracket out would silently widen the page to every row
	// of the table that matched the second disjunct.
	held := sql.SelectQuery{
		Select:  sql.SelectColumns("film_id"),
		From:    sql.From("film_tracking"),
		Where:   sql.ColumnEquals("owner_id", "u"),
		OrderBy: []sql.Ordering{sql.Column[string]("watchlisted_at").Descending()},
		After:   []dynamic.Value{sql.At("2026-01-01")},
		Limit:   40,
	}.Statement(ddl.Postgres).Text()
	expected := `where "owner_id" = $1 and "watchlisted_at" < $2`
	if !strings.Contains(held, expected) {
		t.Fatalf("expected\n\t%s\nin\n\t%s", expected, held)
	}
}
