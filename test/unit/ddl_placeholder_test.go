package unit

// How each dialect spells the values a statement binds.
//
// One case, because there is one rule per dialect and the rule is the whole of
// what a caller needs: two of the three ignore the ordinal and one does not,
// and a caller that had to know which would be a caller that stopped being
// portable the first time it was moved.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-sql/ddl"
)

func TestEachDialectSpellsABoundValueItsOwnWay(t *testing.T) {
	for _, spelling := range []struct {
		dialect ddl.Dialect
		first   string
		third   string
	}{
		{dialect: ddl.Postgres, first: "$1", third: "$3"},
		{dialect: ddl.SQLite, first: "?", third: "?"},
		{dialect: ddl.MySQL, first: "?", third: "?"},
	} {
		t.Run(spelling.dialect.Name(), func(t *testing.T) {
			if held := spelling.dialect.Placeholder(1); held != spelling.first {
				t.Fatalf("expected the first to be %q, got %q", spelling.first, held)
			}
			if held := spelling.dialect.Placeholder(3); held != spelling.third {
				t.Fatalf("expected the third to be %q, got %q", spelling.third, held)
			}
		})
	}
}

func TestPostgresNumbersEveryValueItBinds(t *testing.T) {
	// The property a caller depends on: a statement binding three values in
	// order gets three distinct spellings, so it can be composed a piece at a
	// time and still bind what it meant.
	said := make([]string, 0, 3)
	for ordinal := 1; ordinal <= 3; ordinal++ {
		said = append(said, ddl.Postgres.Placeholder(ordinal))
	}
	if joined := strings.Join(said, ", "); joined != "$1, $2, $3" {
		t.Fatalf("expected them numbered in order, got %q", joined)
	}
}
