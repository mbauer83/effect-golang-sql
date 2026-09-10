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

// computed is one expression, spelled by one dialect, with the select and the
// source stripped off so a case reads as the expression it is about.
func computed(dialect ddl.Dialect, term sql.Term) (string, error) {
	held := sql.Compose(dialect, sql.Computed(dialect, term)...)
	return held.Text(), held.Refused()
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
			term:     sql.Concatenated(title, sql.Bound(" (rewatch)")).Term(),
			postgres: `("title" || $1)`,
			mysql:    "concat(`title`, ?)",
			sqlite:   `("title" || ?)`,
		},
		{
			named:    "taking a substring",
			term:     sql.Substring(title, sql.Bound(int64(1)), sql.Bound(int64(3))).Term(),
			postgres: `substring("title" from $1 for $2)`,
			mysql:    "substring(`title`, ?, ?)",
			sqlite:   `substr("title", ?, ?)`,
		},
		{
			named:    "counting characters",
			term:     sql.Length(title).Term(),
			postgres: `length("title")`,
			// Not length, which counts bytes: right on two servers and
			// quietly wrong on this one for every string that is not ASCII.
			mysql:  "char_length(`title`)",
			sqlite: `length("title")`,
		},
		{
			named:    "joining a group's values",
			term:     sql.Joined(title, ", ").Term(),
			postgres: `string_agg("title", ', ')`,
			mysql:    "group_concat(`title` separator ', ')",
			sqlite:   `group_concat("title", ', ')`,
		},
		{
			named: "taking a difference in seconds",
			term:  sql.Seconds(watched, added).Term(),
			// Three shapes, and MySQL's takes the earlier moment first -- so
			// the arguments are read the other way round, once, here.
			postgres: `extract(epoch from ("watched_at" - "added_at"))`,
			mysql:    "timestampdiff(second, `added_at`, `watched_at`)",
			sqlite:   `((julianday("watched_at") - julianday("added_at")) * 86400)`,
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
				held, why := computed(spelled.dialect, expected.term)
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
	term := sql.Lowered(sql.Trimmed(sql.Column[string]("title"))).Term()
	for _, dialect := range []ddl.Dialect{ddl.Postgres, ddl.MySQL, ddl.SQLite} {
		if _, known := dialect.Writes(sql.LowerCase); known {
			t.Fatalf("%s answers about lowering case, so this case proves nothing",
				dialect.Name())
		}
		held, why := computed(dialect, term)
		if why != nil {
			t.Fatalf("%s: %v", dialect.Name(), why)
		}
		if !containsAll(held, "lower(", "trim(", "title") {
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
	// per dialect, so the type a reading claims is the type it gets.
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
			term:     sql.Total(minutes).Term(),
			postgres: `cast(sum("minutes") as bigint)`,
			mysql:    "cast(sum(`minutes`) as signed)",
			sqlite:   `cast(sum("minutes") as integer)`,
		},
		{
			named:    "an average",
			term:     sql.Mean(minutes).Term(),
			postgres: `cast(avg("minutes") as double precision)`,
			mysql:    "cast(avg(`minutes`) as double)",
			sqlite:   `cast(avg("minutes") as real)`,
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
				held, why := computed(spelled.dialect, expected.term)
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
	held, why := computed(ddl.Postgres, sql.Total(sql.Column[float64]("score")).Term())
	if why != nil {
		t.Fatal(why)
	}
	if held != `sum("score")` {
		t.Fatalf("expected a plain sum, got %s", held)
	}
}
