package unit

// Where an ordering puts its nulls. What matters is that every dialect is made
// to agree, and that a position that is a null in an ordering that does not
// say where nulls go is refused rather than answered three ways.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

func TestEveryDialectPutsNullsWhereTheOrderingSays(t *testing.T) {
	source := sql.From("reviewed", sql.ColumnOf("id", sql.OfWhole), sql.ColumnOf("rating", sql.OfWhole))
	query := sql.SelectQuery{
		Select: sql.SelectColumns("id"), From: source,
		OrderBy: []sql.Ordering{sql.Of[int64](source, "rating").Descending().NullsLast()},
	}
	cases := map[ddl.Dialect]string{
		ddl.Postgres: `ORDER BY "rating" DESC NULLS LAST`,
		ddl.SQLite:   `ORDER BY "rating" DESC NULLS LAST`,
		ddl.MySQL:    "ORDER BY (`rating` IS NULL) ASC, `rating` DESC",
	}
	for dialect, want := range cases {
		if text := query.Statement(dialect).Text(); !strings.Contains(text, want) {
			t.Errorf("%s: expected %s in\n%s", dialect.Name(), want, text)
		}
	}
}

func TestANullPositionInAnOrderingThatDoesNotSayIsRefused(t *testing.T) {
	source := sql.From("reviewed", sql.ColumnOf("rating", sql.OfWhole))
	unstated := []sql.Ordering{sql.Of[int64](source, "rating").Ascending()}
	statement := sql.SelectQuery{Select: sql.SelectColumns("rating"), From: source, OrderBy: unstated,
		After: []dynamic.Value{dynamic.Absent{}}}.Statement(ddl.Postgres)
	if err := statement.Err(); err == nil || !strings.Contains(err.Error(), "NullsFirst or NullsLast") {
		t.Errorf("expected the null position refused, naming what to say, got %v", err)
	}
	stated := []sql.Ordering{sql.Of[int64](source, "rating").Ascending().NullsFirst()}
	past := sql.SelectQuery{Select: sql.SelectColumns("rating"), From: source, OrderBy: stated,
		After: []dynamic.Value{dynamic.Absent{}}}.Statement(ddl.Postgres)
	if err := past.Err(); err != nil || !strings.Contains(past.Text(), "IS NOT NULL") {
		t.Errorf("expected past a null, with nulls first, to be the values, got %v\n%s", err, past.Text())
	}
}
