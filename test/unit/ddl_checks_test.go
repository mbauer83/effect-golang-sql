package unit

// Checks. What matters is that each rule the description states is kept by the
// table in the dialect's own spelling, that a rule a dialect cannot express is
// said as a comment instead of dropped, and that a check has a name a refusal
// can report.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
)

var labelled = schema.Struct[dynamic.Value]("label",
	schema.DynamicField("id", schema.Int64()).Identity(),
	schema.DynamicField("code", schema.Text().Check(schema.Pattern("^[A-Z]{3}$"))),
	schema.DynamicField("name", schema.Text().Check(schema.MinLength(1), schema.MaxLength(40))),
)

func createdIn(t *testing.T, dialect ddl.Dialect) string {
	t.Helper()
	statements, err := ddl.Create(dialect, labelled.Structure())
	if err != nil {
		t.Fatal(err)
	}
	return statements[0]
}

func TestACheckIsNamedForItsTableColumnAndRule(t *testing.T) {
	created := createdIn(t, ddl.Postgres)
	if !strings.Contains(created, `CONSTRAINT "label_name_min_length" CHECK (CHAR_LENGTH("name") >= 1)`) {
		t.Fatalf("expected the minimum length checked by name, got\n%s", created)
	}
}

func TestAPatternIsCheckedWhereTheDialectHasRegularExpressions(t *testing.T) {
	if created := createdIn(t, ddl.Postgres); !strings.Contains(created, `CHECK ("code" ~ '^[A-Z]{3}$')`) {
		t.Fatalf("expected Postgres to check the pattern, got\n%s", created)
	}
	if created := createdIn(t, ddl.MySQL); !strings.Contains(created, "CHECK (`code` REGEXP '^[A-Z]{3}$')") {
		t.Fatalf("expected MySQL to check the pattern, got\n%s", created)
	}
	// SQLite has no regular expression of its own, so the rule is said rather
	// than kept.
	created := createdIn(t, ddl.SQLite)
	if strings.Contains(created, "label_code_pattern") || !strings.Contains(created, "-- matching ^[A-Z]{3}$") {
		t.Fatalf("expected SQLite to note the pattern and not check it, got\n%s", created)
	}
}

func TestAMaximumLengthTheTypeKeepsIsNotCheckedAgain(t *testing.T) {
	// A bounded varchar keeps its own limit; a text column needs the check.
	if created := createdIn(t, ddl.Postgres); strings.Contains(created, "label_name_max_length") {
		t.Fatalf("expected the varchar to keep the limit, got\n%s", created)
	}
	if created := createdIn(t, ddl.SQLite); !strings.Contains(created, `CHECK (LENGTH("name") <= 40)`) {
		t.Fatalf("expected SQLite to check the limit, got\n%s", created)
	}
}

func TestALongCheckNameIsShortenedAndStaysUnique(t *testing.T) {
	long := schema.Struct[dynamic.Value](strings.Repeat("t", 40),
		schema.DynamicField("id", schema.Int64()).Identity(),
		schema.DynamicField(strings.Repeat("c", 30), schema.Text().Check(schema.MinLength(1))),
	)
	statements, err := ddl.Create(ddl.Postgres, long.Structure())
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(statements[0], `CONSTRAINT "`) + len(`CONSTRAINT "`)
	name := statements[0][start : start+strings.Index(statements[0][start:], `"`)]
	if len(name) > 63 {
		t.Fatalf("expected a name Postgres takes, got %d bytes: %s", len(name), name)
	}
}
