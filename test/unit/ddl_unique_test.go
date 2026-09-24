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
	"github.com/mbauer83/effect-golang-sql/sql"
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

func TestFieldsUniqueTogetherAreOneUniqueIndex(t *testing.T) {
	shelved := schema.Struct[dynamic.Value]("shelved",
		schema.DynamicField("id", schema.Int64()).Identity(),
		schema.DynamicField("title", schema.Text().Check(schema.MaxLength(200))).UniqueTogether("title_per_author"),
		schema.DynamicField("author", schema.Text().Check(schema.MaxLength(100))).UniqueTogether("title_per_author"),
	)
	statements, err := ddl.Create(ddl.Postgres, shelved.Structure())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(statements, "\n")
	if !strings.Contains(joined, `CREATE UNIQUE INDEX "shelved_title_per_author" ON "shelved" ("title", "author")`) {
		t.Fatalf("expected one unique index over both, got\n%s", joined)
	}
}

type place struct{ Aisle, Shelf string }
type stocked struct {
	ID    int64
	Place place
}

func TestAUniqueValueObjectIsItsColumnsUniqueTogether(t *testing.T) {
	placeSchema := schema.Struct[place]("place",
		schema.FieldAt("aisle", schema.Text().Check(schema.MaxLength(8)), func(value *place) *string { return &value.Aisle }),
		schema.FieldAt("shelf", schema.Text().Check(schema.MaxLength(8)), func(value *place) *string { return &value.Shelf }))
	mapped := sql.Map(schema.Struct[stocked]("stocked",
		schema.FieldAt("id", schema.Int64(), func(value *stocked) *int64 { return &value.ID }).Identity(),
		schema.FieldAt("place", placeSchema, func(value *stocked) *place { return &value.Place }).Unique()))
	statements, err := ddl.Create(ddl.Postgres, mapped.Schema().Structure())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(statements, "\n")
	if !strings.Contains(joined, `CREATE UNIQUE INDEX "stocked_place" ON "stocked" ("place_aisle", "place_shelf")`) {
		t.Fatalf("expected the value object's columns unique together, got\n%s", joined)
	}
}
