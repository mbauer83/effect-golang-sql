package unit

// References between aggregates, stored. What matters is that a reference
// becomes a foreign key to where its target is stored, restricting unless the
// domain says otherwise, that a reference whose target the mapping was not
// told about is refused, and that a join on it needs no condition written by
// hand.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

type title struct {
	ID   int64
	Name string
}

var titleFields = struct {
	ID   schema.Field[title, int64]
	Name schema.Field[title, string]
}{
	ID:   schema.FieldAt("id", schema.Int64(), func(value *title) *int64 { return &value.ID }).Identity(),
	Name: schema.FieldAt("name", schema.Text(), func(value *title) *string { return &value.Name }),
}

var titleSchema = schema.Struct[title]("title", titleFields.ID, titleFields.Name)

type showing struct {
	ID    int64
	Title int64
}

func showingSchema(onDelete ...schema.Deletion) schema.Schema[showing] {
	return schema.Struct[showing]("showing",
		schema.FieldAt("id", schema.Int64(), func(value *showing) *int64 { return &value.ID }).Identity(),
		schema.FieldAt("title", schema.Ref(titleSchema, titleFields.ID, onDelete...),
			func(value *showing) *int64 { return &value.Title }))
}

var titles = sql.Map(titleSchema).Column(titleFields.ID, "title_id")

func TestAReferenceIsAForeignKeyToWhereItsTargetIsStored(t *testing.T) {
	stored := sql.Map(showingSchema()).Referring(titles).Schema()
	statements, err := ddl.Create(ddl.Postgres, stored.Structure())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(statements, "\n")
	if !strings.Contains(joined, `FOREIGN KEY ("title") REFERENCES "title" ("title_id")`) {
		t.Fatalf("expected a key to the title's own column, got\n%s", joined)
	}
	// Restricting: no ON DELETE clause, so deleting a title something shows is
	// refused.
	if strings.Contains(joined, "ON DELETE") {
		t.Fatalf("expected the reference to restrict, got\n%s", joined)
	}
	if !strings.Contains(joined, `CREATE INDEX "showing_title" ON "showing" ("title")`) {
		t.Fatalf("expected an index to join on, got\n%s", joined)
	}
}

func TestADeletionTheDomainStatesIsTheKeys(t *testing.T) {
	stored := sql.Map(showingSchema(schema.Cascade)).Referring(titles).Schema()
	statements, _ := ddl.Create(ddl.Postgres, stored.Structure())
	if !strings.Contains(strings.Join(statements, "\n"), "ON DELETE CASCADE") {
		t.Fatalf("expected the showing to go with its title, got\n%s", statements)
	}
	required := sql.Map(showingSchema(schema.SetNull)).Referring(titles).Schema()
	if _, err := ddl.Create(ddl.Postgres, required.Structure()); err == nil ||
		!strings.Contains(err.Error(), "required") {
		t.Fatalf("expected emptying a required reference refused, got %v", err)
	}
}

func TestAReferenceWhoseTargetTheMappingWasNotToldAboutIsRefused(t *testing.T) {
	stored := sql.Map(showingSchema()).Schema()
	if _, err := ddl.Create(ddl.Postgres, stored.Structure()); err == nil ||
		!strings.Contains(err.Error(), "Referring") {
		t.Fatalf("expected the unmapped target refused, saying how to map it, got %v", err)
	}
}

func TestAJoinOnAReferenceNeedsNoConditionWrittenByHand(t *testing.T) {
	showings, err := ddl.Tables(ddl.Postgres, sql.Map(showingSchema()).Referring(titles).Schema().Structure())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ddl.Tables(ddl.Postgres, titles.Schema().Structure())
	if err != nil {
		t.Fatal(err)
	}
	join, err := ddl.Join(showings[0], stored[0])
	if err != nil {
		t.Fatal(err)
	}
	reading := sql.SelectQuery{
		Select: sql.SelectTerms(sql.Of[string](stored[0].Source(), "name").Term()),
		From:   showings[0].Source(),
		Joins:  []sql.Join{join},
	}
	held := reading.Statement(ddl.Postgres)
	if held.Err() != nil {
		t.Fatal(held.Err())
	}
	if !strings.Contains(held.Text(), `JOIN "title" ON "showing"."title" = "title"."title_id"`) {
		t.Fatalf("expected the join on the key, got\n\t%s", held.Text())
	}
	// Either way round.
	if _, err := ddl.Join(stored[0], showings[0]); err != nil {
		t.Fatalf("expected the join from the other end too, got %v", err)
	}
}
