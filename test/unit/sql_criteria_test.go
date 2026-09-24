package unit

// Which rows a statement is about.
//
// The closed part of the vocabulary, and the three things a renderer of it can
// get wrong: an empty set spelled as a list no dialect accepts, a bracket left
// out where precedence would then decide the meaning, and a criterion the
// caller never supplied written as an empty clause.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

func whereStatement(where sql.Criterion) sql.Statement {
	return sql.SelectQuery{
		Select: sql.SelectColumns("film_id"),
		From:   sql.From("film_tracking"),
		Where:  where,
	}.Statement(ddl.Postgres)
}

func TestASetWithNothingInItMatchesNoRows(t *testing.T) {
	// Not "IN ()", which none of the three accept: a caller whose filter
	// turned out empty gets the empty answer instead of a syntax error.
	held := whereStatement(sql.AmongValues(sql.Column[string]("copy_id")))
	if !strings.HasSuffix(held.Text(), "WHERE 1 = 0") {
		t.Fatalf("expected a criterion nothing satisfies, got %s", held.Text())
	}
	if len(held.Values()) != 0 {
		t.Fatal("expected nothing bound")
	}
}

func TestSeveralCriteriaBindInTheOrderTheyAreRead(t *testing.T) {
	held := whereStatement(sql.Both(
		sql.ColumnEquals("user_id", "u"),
		sql.AmongValues(sql.Column[string]("film_id"), "f", "g"),
		sql.Present(sql.Column[string]("shelved_at")),
	))
	expected := `"user_id" = $1 AND "film_id" IN ($2, $3) AND "shelved_at" IS NOT NULL`
	if !strings.HasSuffix(held.Text(), "WHERE "+expected) {
		t.Fatalf("expected it to end with\n\t%s\ngot\n\t%s", expected, held.Text())
	}
	if len(held.Values()) != 3 {
		t.Fatalf("expected three bound values, got %d", len(held.Values()))
	}
}

func TestACriterionThatExcludesNothingIsNotWritten(t *testing.T) {
	// What lets a query and its own criterion into whatever the caller gave
	// without asking whether the caller gave one.
	held := whereStatement(sql.Both(sql.All(), sql.ColumnEquals("user_id", "u")))
	if !strings.HasSuffix(held.Text(), `WHERE "user_id" = $1`) {
		t.Fatalf("unexpected statement: %s", held.Text())
	}
	if strings.Contains(held.Text(), "()") {
		t.Fatalf("expected no empty brackets, got %s", held.Text())
	}
}

func TestACriterionInsideAnotherIsBracketedSoPrecedenceNeverDecides(t *testing.T) {
	// "AND" binds tighter than "OR", so a disjunction inside a conjunction is
	// a different criterion without brackets. Every junction member gets them
	// rather than the ones that would change meaning without them, because
	// working out which those are is arithmetic on precedence levels in aid of
	// removing a character.
	for _, expected := range []struct {
		named string
		where sql.Criterion
		said  string
	}{
		{
			named: "a disjunction inside a conjunction",
			where: sql.Both(
				sql.ColumnEquals("user_id", "u"),
				sql.Either(
					sql.Present(sql.Column[string]("watched_at")),
					sql.Present(sql.Column[string]("watchlisted_at")),
				),
			),
			said: `"user_id" = $1 AND ("watched_at" IS NOT NULL OR "watchlisted_at" IS NOT NULL)`,
		},
		{
			named: "a conjunction inside a disjunction",
			where: sql.Either(
				sql.Both(
					sql.ColumnEquals("user_id", "u"),
					sql.Present(sql.Column[string]("watched_at")),
				),
				sql.Present(sql.Column[string]("watchlisted_at")),
			),
			said: `("user_id" = $1 AND "watched_at" IS NOT NULL) OR "watchlisted_at" IS NOT NULL`,
		},
	} {
		t.Run(expected.named, func(t *testing.T) {
			held := whereStatement(expected.where).Text()
			if !strings.HasSuffix(held, "WHERE "+expected.said) {
				t.Fatalf("expected it to end with\n\t%s\ngot\n\t%s", expected.said, held)
			}
		})
	}
}

func TestReversingACriterionSaysNotOfIt(t *testing.T) {
	held := whereStatement(sql.Not(sql.Present(sql.Column[string]("watched_at")))).Text()
	if !strings.HasSuffix(held, `WHERE NOT ("watched_at" IS NOT NULL)`) {
		t.Fatalf("unexpected statement: %s", held)
	}
	// Reversing "every row" is no row, which is the one case where the answer
	// is not the word not: a clause that was never written has nothing to
	// negate.
	if reversed := whereStatement(sql.Not(sql.All())).Text(); !strings.HasSuffix(reversed, "WHERE 1 = 0") {
		t.Fatalf("expected no rows, got %s", reversed)
	}
}

func TestAWildcardPatternIsUniversalAndARegularExpressionIsNot(t *testing.T) {
	pattern := sql.Like(sql.Column[string]("title"), sql.Param("Heat%"))
	for _, dialect := range []ddl.Dialect{ddl.Postgres, ddl.MySQL, ddl.SQLite} {
		held := whereStatement(pattern)
		if held.Err() != nil {
			t.Fatalf("%s refused a wildcard pattern: %v", dialect.Name(), held.Err())
		}
	}

	regular := sql.Regexp(sql.Column[string]("title"), sql.Param("^Heat"))
	for _, expected := range []struct {
		dialect ddl.Dialect
		said    string
	}{
		{dialect: ddl.Postgres, said: `"title" ~ $1`},
		{dialect: ddl.MySQL, said: "`title` REGEXP ?"},
	} {
		held := sql.SelectQuery{
			Select: sql.SelectColumns("film_id"),
			From:   sql.From("film_tracking"),
			Where:  regular,
		}.Statement(expected.dialect)
		if held.Err() != nil {
			t.Fatalf("%s refused: %v", expected.dialect.Name(), held.Err())
		}
		if !strings.HasSuffix(held.Text(), expected.said) {
			t.Fatalf("expected %s to end with %s, got %s",
				expected.dialect.Name(), expected.said, held.Text())
		}
	}

	// SQLite has none without an extension, so it says nothing and the
	// statement carries which dialect could not do what -- rather than being
	// composed and refused by the server at the first request.
	held := sql.SelectQuery{
		Select: sql.SelectColumns("film_id"),
		From:   sql.From("film_tracking"),
		Where:  regular,
	}.Statement(ddl.SQLite)
	if held.Err() == nil {
		t.Fatal("expected SQLite to refuse a regular expression")
	}
	if !strings.Contains(held.Err().Error(), "sqlite") {
		t.Fatalf("expected the refusal to name the dialect, got %v", held.Err())
	}
}
