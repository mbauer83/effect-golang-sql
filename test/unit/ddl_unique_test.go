package unit

// Uniqueness the description states. What matters is that it becomes an index
// the database keeps, and that a column a dialect cannot index is refused when
// the table is derived rather than when the statement runs.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
)

func TestAUniqueFieldIsAUniqueIndex(t *testing.T) {
	account := schema.Struct[dynamic.Value]("account",
		schema.DynamicField("id", schema.Int64()).Identity(),
		schema.DynamicField("email", schema.Text().Check(schema.MaxLength(254))).Unique(),
	)
	statements, err := ddl.Create(ddl.Postgres, account.Structure())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(statements, "\n")
	if !strings.Contains(joined, `CREATE UNIQUE INDEX "account_email_unique" ON "account" ("email")`) {
		t.Fatalf("expected a unique index on the email, got\n%s", joined)
	}
}

func TestAUniqueFieldADialectCannotIndexIsRefused(t *testing.T) {
	unbounded := schema.Struct[dynamic.Value]("account",
		schema.DynamicField("id", schema.Int64()).Identity(),
		schema.DynamicField("email", schema.Text()).Unique(),
	)
	if _, err := ddl.Create(ddl.MySQL, unbounded.Structure()); err == nil ||
		!strings.Contains(err.Error(), "email") {
		t.Fatalf("expected MySQL to refuse an unbounded unique text, naming it, got %v", err)
	}
	if _, err := ddl.Create(ddl.Postgres, unbounded.Structure()); err != nil {
		t.Fatalf("expected Postgres to index unbounded text, got %v", err)
	}
}
