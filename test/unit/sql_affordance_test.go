package unit

// What each dialect makes of the operations the three do not share.
//
// One case per operation over all three dialects, because the answers are the
// whole point: a store that had written any of these by hand would have been
// right on one server and wrong on the others, and only one of the wrongs --
// MySQL counting bytes where the others count characters -- fails quietly
// rather than loudly.

import (
	"testing"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// spellTerm is one expression, spelled by one dialect, with the select and the
// source stripped off so a case reads as the expression it is about.
func spellTerm(dialect ddl.Dialect, term sql.Term) (string, error) {
	held := sql.Compose(dialect, sql.Computation(dialect, term)...)
	return held.Text(), held.Err()
}

func TestTheThreeDialectsSpellTheOperationsTheyDoNotShare(t *testing.T) {
	title := sql.Column[string]("title")
	watched := sql.Column[timeStub]("watched_at")
	added := sql.Column[timeStub]("added_at")

	for _, expected := range []struct {
		named    string
		term     sql.Term
		postgres string
		mysql    string
		sqlite   string
	}{
		{
			named:    "concatenating",
			term:     sql.Concat(title, sql.Param(" (rewatch)")).Term(),
			postgres: `("title" || $1)`,
			mysql:    "CONCAT(`title`, ?)",
			sqlite:   `("title" || ?)`,
		},
		{
			named:    "taking a substring",
			term:     sql.Substring(title, sql.Param(int64(1)), sql.Param(int64(3))).Term(),
			postgres: `SUBSTRING("title" FROM $1 FOR $2)`,
			mysql:    "SUBSTRING(`title`, ?, ?)",
			sqlite:   `SUBSTR("title", ?, ?)`,
		},
		{
			named:    "counting characters",
			term:     sql.Length(title).Term(),
			postgres: `LENGTH("title")`,
			// Not length, which counts bytes: right on two servers and
			// quietly wrong on this one for every string that is not ASCII.
			mysql:  "CHAR_LENGTH(`title`)",
			sqlite: `LENGTH("title")`,
		},
		{
			named:    "joining a group's values",
			term:     sql.StringAgg(title, ", ").Term(),
			postgres: `STRING_AGG("title", ', ')`,
			mysql:    "GROUP_CONCAT(`title` SEPARATOR ', ')",
			sqlite:   `GROUP_CONCAT("title", ', ')`,
		},
		{
			named: "taking a difference in seconds",
			term:  sql.Seconds(watched, added).Term(),
			// Three shapes, and MySQL's takes the earlier moment first -- so
			// the arguments are read the other way round, once, here.
			postgres: `EXTRACT(EPOCH FROM ("watched_at" - "added_at"))`,
			mysql:    "TIMESTAMPDIFF(SECOND, `added_at`, `watched_at`)",
			sqlite:   `((JULIANDAY("watched_at") - JULIANDAY("added_at")) * 86400)`,
		},
	} {
		t.Run(expected.named, func(t *testing.T) {
			for _, spelled := range []struct {
				dialect ddl.Dialect
				said    string
			}{
				{dialect: ddl.Postgres, said: expected.postgres},
				{dialect: ddl.MySQL, said: expected.mysql},
				{dialect: ddl.SQLite, said: expected.sqlite},
			} {
				held, why := spellTerm(spelled.dialect, expected.term)
				if why != nil {
					t.Fatalf("%s: %v", spelled.dialect.Name(), why)
				}
				if held != spelled.said {
					t.Errorf("%s: expected\n\t%s\ngot\n\t%s",
						spelled.dialect.Name(), spelled.said, held)
				}
			}
		})
	}
}

func TestAnOrdinaryOperationNeedsNoAnswerFromAnyDialect(t *testing.T) {
	// The other half of the arrangement: most operations are the same
	// everywhere, so a dialect answers nothing about them and gets the
	// ordinary spelling. A dialect that had to answer thirty questions in
	// order to disagree about five would be a dialect nobody would write.
	term := sql.Lower(sql.Trim(sql.Column[string]("title"))).Term()
	for _, dialect := range []ddl.Dialect{ddl.Postgres, ddl.MySQL, ddl.SQLite} {
		if _, known := dialect.Syntax(sql.LowerCase); known {
			t.Fatalf("%s answers about lowering case, so this case proves nothing",
				dialect.Name())
		}
		held, why := spellTerm(dialect, term)
		if why != nil {
			t.Fatalf("%s: %v", dialect.Name(), why)
		}
		if !containsAll(held, "LOWER(", "TRIM(", "title") {
			t.Errorf("%s: unexpected spelling %s", dialect.Name(), held)
		}
	}
}

func containsAll(held string, wanted ...string) bool {
	for _, one := range wanted {
		if !contains(held, one) {
			return false
		}
	}
	return true
}

func TestAnAggregateComesBackAsTheTypeTheQueryClaims(t *testing.T) {
	// The one thing the type parameter cannot check: two of the three servers
	// answer an aggregate with a wider type than the values it was over, and
	// hand it back as text. A total of whole numbers and an average are cast,
	// per dialect, so the type a query claims is the type it gets.
	minutes := sql.Column[int64]("minutes")
	for _, expected := range []struct {
		named    string
		term     sql.Term
		postgres string
		mysql    string
		sqlite   string
	}{
		{
			named:    "a total of whole numbers",
			term:     sql.Sum(minutes).Term(),
			postgres: `CAST(SUM("minutes") AS BIGINT)`,
			mysql:    "CAST(SUM(`minutes`) AS SIGNED)",
			sqlite:   `CAST(SUM("minutes") AS INTEGER)`,
		},
		{
			named:    "an average",
			term:     sql.Avg(minutes).Term(),
			postgres: `CAST(AVG("minutes") AS DOUBLE PRECISION)`,
			mysql:    "CAST(AVG(`minutes`) AS DOUBLE)",
			sqlite:   `CAST(AVG("minutes") AS REAL)`,
		},
	} {
		t.Run(expected.named, func(t *testing.T) {
			for _, spelled := range []struct {
				dialect ddl.Dialect
				said    string
			}{
				{dialect: ddl.Postgres, said: expected.postgres},
				{dialect: ddl.MySQL, said: expected.mysql},
				{dialect: ddl.SQLite, said: expected.sqlite},
			} {
				held, why := spellTerm(spelled.dialect, expected.term)
				if why != nil {
					t.Fatalf("%s: %v", spelled.dialect.Name(), why)
				}
				if held != spelled.said {
					t.Errorf("%s: expected\n\t%s\ngot\n\t%s",
						spelled.dialect.Name(), spelled.said, held)
				}
			}
		})
	}
}

func TestATotalOfNumbersNeedsNoCast(t *testing.T) {
	// Only the widening cases are asked for differently: a sum of floats
	// answers a float on every server, so it is the ordinary sum.
	held, why := spellTerm(ddl.Postgres, sql.Sum(sql.Column[float64]("score")).Term())
	if why != nil {
		t.Fatal(why)
	}
	if held != `SUM("score")` {
		t.Fatalf("expected a plain sum, got %s", held)
	}
}
