package unit

// What a store says, and what each dialect makes of it.
//
// The claim under test is that a query is stated once and gets the spelling of
// the server it is talking to -- so these cases are mostly one query rendered
// three ways, and the interesting ones are where the three answers differ.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

func TestAReadingIsSpelledForTheServerItRunsOn(t *testing.T) {
	reading := sql.SelectQuery{
		Select: sql.SelectColumns("film_id", "title"),
		From:   sql.From("film_catalog"),
		Where:  sql.ColumnEquals("film_id", "tt0111161"),
		Limit:  1,
	}
	for _, expected := range []struct {
		dialect ddl.Dialect
		said    string
	}{
		{
			dialect: ddl.Postgres,
			said:    `select "film_id", "title" from "film_catalog" where "film_id" = $1 limit 1`,
		},
		{
			dialect: ddl.SQLite,
			said:    `select "film_id", "title" from "film_catalog" where "film_id" = ? limit 1`,
		},
		{
			dialect: ddl.MySQL,
			said:    "select `film_id`, `title` from `film_catalog` where `film_id` = ? limit 1",
		},
	} {
		t.Run(expected.dialect.Name(), func(t *testing.T) {
			held := reading.Statement(expected.dialect)
			if held.Err() != nil {
				t.Fatalf("unexpected refusal: %v", held.Err())
			}
			if held.Text() != expected.said {
				t.Fatalf("expected\n\t%s\ngot\n\t%s", expected.said, held.Text())
			}
			if len(held.Values()) != 1 {
				t.Fatalf("expected one bound value, got %d", len(held.Values()))
			}
		})
	}
}

func TestAReplacementIsTheOneWriteTheThreeSpellThreeWays(t *testing.T) {
	replacement := sql.UpsertQuery{
		Table:   "film_tracking",
		Columns: []string{"user_id", "film_id", "watchlisted_at"},
		Key:     []string{"user_id", "film_id"},
		Values:  values("u", "f", "now"),
	}
	for _, expected := range []struct {
		dialect ddl.Dialect
		tail    string
	}{
		{
			dialect: ddl.Postgres,
			tail:    `on conflict ("user_id", "film_id") do update set "watchlisted_at" = excluded."watchlisted_at"`,
		},
		{
			dialect: ddl.SQLite,
			tail:    `on conflict ("user_id", "film_id") do update set "watchlisted_at" = excluded."watchlisted_at"`,
		},
		{
			dialect: ddl.MySQL,
			tail:    "as offered on duplicate key update `watchlisted_at` = offered.`watchlisted_at`",
		},
	} {
		t.Run(expected.dialect.Name(), func(t *testing.T) {
			held := replacement.Statement(expected.dialect)
			if !strings.HasSuffix(held.Text(), expected.tail) {
				t.Fatalf("expected it to end with\n\t%s\ngot\n\t%s", expected.tail, held.Text())
			}
			// Three columns, three values, once: an upsert offers the row
			// and does not bind it again for the update.
			if len(held.Values()) != 3 {
				t.Fatalf("expected three bound values, got %d", len(held.Values()))
			}
		})
	}
}

func TestAKeyThatIsTheWholeRowLeavesNothingToAssign(t *testing.T) {
	replacement := sql.UpsertQuery{
		Table:   "film_viewing_tag",
		Columns: []string{"viewing_id", "tag"},
		Key:     []string{"viewing_id", "tag"},
		Values:  values("v", "rewatch"),
	}
	if held := replacement.Statement(ddl.Postgres).Text(); !strings.HasSuffix(held, "do nothing") {
		t.Fatalf("expected Postgres to do nothing, got %s", held)
	}
	// MySQL has no "do nothing", so it is given the assignment that changes
	// least rather than a clause it would refuse.
	held := replacement.Statement(ddl.MySQL).Text()
	if !strings.HasSuffix(held, "as offered on duplicate key update `viewing_id` = offered.`viewing_id`") {
		t.Fatalf("expected MySQL to assign a key column to itself, got %s", held)
	}
}

func TestAWritingBindsItsColumnsInOrder(t *testing.T) {
	writing := sql.InsertQuery{
		Table:   "film_copy",
		Columns: []string{"copy_id", "user_id", "film_id"},
		Values:  values("c", "u", "f"),
	}
	held := writing.Statement(ddl.Postgres)
	expected := `insert into "film_copy" ("copy_id", "user_id", "film_id") values ($1, $2, $3)`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
	if held.Values()[1] != text("u") {
		t.Fatalf("expected the second value to be the user, got %v", held.Values()[1])
	}
}

func TestARemovalSaysWhichRowsItIsAbout(t *testing.T) {
	removal := sql.DeleteQuery{
		Table: "film_viewing",
		Where: sql.ColumnEquals("tracking_id", "u:1"),
	}
	held := removal.Statement(ddl.SQLite)
	if held.Text() != `delete from "film_viewing" where "tracking_id" = ?` {
		t.Fatalf("unexpected statement: %s", held.Text())
	}
}

func TestAStatementWrittenByHandStillSpellsNoPlaceholder(t *testing.T) {
	// The lower way of saying a statement, for the one shape the query
	// specification leaves out. What it still does not do is choose a
	// spelling, and the criterion in it is the specification's own.
	held := sql.Compose(ddl.Postgres,
		append(
			[]sql.Part{sql.Text(`select count(*) from "film_viewing" where `)},
			sql.Condition(ddl.Postgres, sql.Both(
				sql.ColumnEquals("user_id", "u"),
				sql.Above(sql.Column[int64]("watched_at"), sql.Param(int64(17))),
			))...,
		)...,
	)
	expected := `select count(*) from "film_viewing" where "user_id" = $1 and "watched_at" > $2`
	if held.Text() != expected {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", expected, held.Text())
	}
}
