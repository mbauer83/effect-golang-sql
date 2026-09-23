package unit

// What a query refuses, and why refusing beats composing.
//
// Everything here is a statement that would have reached a server and been
// rejected there -- at the first request, in production, with a message about
// syntax rather than about the mistake. A refusal carries the query's own
// words instead, and the runners will not send a statement that carries one.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

func TestAColumnASourceDoesNotHaveIsRefusedWhereItIsNamed(t *testing.T) {
	// The rename case: a member the domain no longer has, in a query written
	// when it did. Nothing about the statement is wrong except the name, so a
	// server's answer would be a syntax error and this is the column.
	held := sql.SelectQuery{
		Select: sql.SelectTerms(sql.Of[string](trackings, "watchlisted").Term()),
		From:   trackings,
	}.Statement(ddl.Postgres)
	why := held.Err()
	if why == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(why.Error(), "watchlisted") || !strings.Contains(why.Error(), "watchlisted_at") {
		t.Fatalf("expected the refusal to name what was asked and what is there, got %v", why)
	}
}

func TestAColumnReadAsTheWrongTypeIsRefused(t *testing.T) {
	// The retype case, and the one a name check alone would let through: the
	// column is there and holds a moment, and this reads it as a number. A
	// server would compare them by coercing one side, or refuse, depending on
	// which server.
	held := sql.SelectQuery{
		Select: sql.SelectTerms(sql.Of[int64](viewings, "watched_at").Term()),
		From:   viewings,
	}.Statement(ddl.Postgres)
	why := held.Err()
	if why == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(why.Error(), "moment") {
		t.Fatalf("expected the refusal to say what the column holds, got %v", why)
	}
}

func TestASourceToldNothingChecksNothing(t *testing.T) {
	// The other half of being schema-driven: a query over a table this module
	// has no description of is still a query. Refusing it would send the
	// caller back to writing SQL, which is the thing this exists to stop.
	held := sql.SelectQuery{
		Select: sql.SelectTerms(sql.Of[int64](sql.From("whatever"), "anything").Term()),
		From:   sql.From("whatever"),
	}.Statement(ddl.Postgres)
	if held.Err() != nil {
		t.Fatalf("expected no refusal from a source that was told nothing: %v", held.Err())
	}
}

func TestAQueryMissingWhatMakesItAQuerySaysSo(t *testing.T) {
	for _, expected := range []struct {
		named   string
		reading sql.SelectQuery
		said    string
	}{
		{
			named:   "nothing selected",
			reading: sql.SelectQuery{From: sql.From("film_tracking")},
			said:    "what it answers with",
		},
		{
			named:   "nowhere to read from",
			reading: sql.SelectQuery{Select: sql.SelectColumns("film_id")},
			said:    "where its rows come from",
		},
	} {
		t.Run(expected.named, func(t *testing.T) {
			why := expected.reading.Statement(ddl.SQLite).Err()
			if why == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(why.Error(), expected.said) {
				t.Fatalf("expected the refusal to mention %q, got %v", expected.said, why)
			}
		})
	}
}

func TestARefusedStatementIsNeverSent(t *testing.T) {
	// The claim that makes a refusal safe to carry rather than return: there
	// is no path by which a statement carrying one reaches a database. The
	// fake here would record any statement it was given, and is given none.
	unasked := &unaskable{}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	refused := sql.SelectQuery{
		Select: sql.SelectTerms(sql.Of[string](trackings, "nonesuch").Term()),
		From:   trackings,
	}.Statement(ddl.SQLite)

	exit := runtime.Run(context.Background(), effect.Unit{},
		sql.Run[effect.Unit](unasked, refused))
	if _, ok := exit.Value(); ok {
		t.Fatal("expected running a refused statement to fail")
	}
	if len(unasked.statements) != 0 {
		t.Fatalf("a refused statement reached the database: %v", unasked.statements)
	}
	cause, stopped := exit.Cause()
	if !stopped {
		t.Fatal("expected a cause")
	}
	if len(cause.Failures()) != 1 {
		t.Fatalf("expected one typed failure rather than a defect, got %+v", cause)
	}
}

func TestAnOperationADialectCannotDoNamesBoth(t *testing.T) {
	held := sql.SelectQuery{
		Select: sql.SelectTerms(
			sql.Regexp(sql.Column[string]("title"), sql.Param("^Heat")).Term()),
		From: sql.From("film"),
	}.Statement(ddl.SQLite)
	why := held.Err()
	if why == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(why.Error(), "sqlite") ||
		!strings.Contains(why.Error(), "regular expression") {
		t.Fatalf("expected the refusal to name the dialect and the operation, got %v", why)
	}
}

func TestADialectCanBeTaughtAnOperationItDoesNotHave(t *testing.T) {
	// The extension seam. SQLite has no regular expression unless the program
	// that opened the database registered one -- so a program that did says
	// so, and nothing in this module changes.
	taught := sql.Also(ddl.SQLite, map[sql.Operation]sql.Syntax{
		sql.ExpressionMatch: sql.Infix(" regexp "),
	})
	held := sql.SelectQuery{
		Select: sql.SelectColumns("film_id"),
		From:   sql.From("film"),
		Where:  sql.Regexp(sql.Column[string]("title"), sql.Param("^Heat")),
	}.Statement(taught)
	if held.Err() != nil {
		t.Fatal(held.Err())
	}
	if !strings.HasSuffix(held.Text(), `where "title" regexp ?`) {
		t.Fatalf("unexpected statement: %s", held.Text())
	}
}

// unaskable is a database that remembers what it was asked and refuses to
// answer, so a case can establish that it was asked nothing at all.
type unaskable struct {
	statements []string
}

func (held *unaskable) Query(_ context.Context, statement string, _ []dynamic.Value) (sql.Cursor, error) {
	held.statements = append(held.statements, statement)
	return nil, errNeverAsked
}

func (held *unaskable) Execute(_ context.Context, statement string, _ []dynamic.Value) (sql.Outcome, error) {
	held.statements = append(held.statements, statement)
	return sql.Outcome{}, errNeverAsked
}

// errNeverAsked is what this fake answers with, so a case that reached it
// fails for reaching it rather than for what came back.
var errNeverAsked = errors.New("this database should not have been asked anything")
