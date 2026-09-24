package unit

// Where the two dialects differ in what they can *hold* rather than in how they
// spell it -- which is the only kind of difference worth a refusal.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
)

func TestPostgresRefusesAnUnsignedSixtyFourAndMySQLDoesNot(t *testing.T) {
	// The one place the two dialects differ in what they can *hold* rather
	// than in how they spell it. Postgres has no unsigned integers: a decimal
	// would hold the values and would not be an integer, so a key that was
	// fast would quietly stop being one -- which is the protobuf-width mistake
	// under another name.
	// An integer identity, so the key rule below does not fire first and turn
	// this into a test of something else.
	wide := schema.Struct[dynamic.Value]("Wide",
		schema.DynamicField("id", schema.Int64()).Identity().Computed(),
		schema.DynamicField("counter", schema.Uint64()),
	)

	_, err := ddl.Tables(ddl.Postgres, wide.Structure())
	if err == nil {
		t.Fatal("expected postgres to refuse an unsigned 64-bit column")
	}
	if !strings.Contains(err.Error(), "would not be an integer") {
		t.Errorf("expected the reason to say why, got %v", err)
	}

	// MySQL has them, so widening there would throw away a range it offers.
	tables, err := ddl.Tables(ddl.MySQL, wide.Structure())
	if err != nil {
		t.Fatalf("expected mysql to accept it, got %v", err)
	}
	counter, held := columnIn(tables[0], "counter")
	if !held || counter.Type != "BIGINT UNSIGNED" {
		t.Errorf("unexpected column: %#v", counter)
	}
}

func TestTheSmallUnsignedTypesAreWidenedRatherThanRefused(t *testing.T) {
	// Widening is not approximating: every value of a uint32 is a value of a
	// bigint, so nothing is lost and the column is still an integer. Refusing
	// these would be refusing something Postgres can do perfectly well.
	narrow := schema.Struct[dynamic.Value]("Narrow",
		schema.DynamicField("id", schema.UUID()).Identity(),
		schema.DynamicField("small", schema.Uint8()),
		schema.DynamicField("medium", schema.Uint16()),
		schema.DynamicField("wide", schema.Uint32()),
	)

	tables, err := ddl.Tables(ddl.Postgres, narrow.Structure())
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{
		"small": "SMALLINT", "medium": "INTEGER", "wide": "BIGINT",
	} {
		column, held := columnIn(tables[0], name)
		if !held || column.Type != expected {
			t.Errorf("%s: expected %q, got %#v", name, expected, column)
		}
	}
}

func TestADerivedColumnThatCollidesWithADeclaredOneIsRefused(t *testing.T) {
	// The reference to the parent and the position in the list are both
	// derived, so a description that already has a column of that name would
	// otherwise get two -- or one silently overwritten.
	colliding := schema.Struct[dynamic.Value]("Crate",
		schema.DynamicField("id", schema.Int64()).Identity().Computed(),
		schema.DynamicField("what", schema.Text()),
		schema.DynamicField("lines", schema.List(schema.Struct[dynamic.Value]("CrateLine",
			schema.DynamicField("id", schema.UUID()).Identity(),
			schema.DynamicField("position", schema.Text()),
		))),
	)

	_, err := ddl.Tables(ddl.Postgres, colliding.Structure())
	if err == nil {
		t.Fatal("expected the collision to be refused")
	}
	if !strings.Contains(err.Error(), "already there") {
		t.Errorf("expected the reason to name the collision, got %v", err)
	}
}

func TestAMapOfEntitiesIsStoredAsADocumentRatherThanGuessedAt(t *testing.T) {
	// A map of entities would need a column for the key, and the description
	// does not say what to call it. Inventing a name would put it in the schema
	// forever, so the map goes in one column and the entities stay in it --
	// which is what the description literally says: a map of these.
	mapped := schema.Struct[dynamic.Value]("Bay",
		schema.DynamicField("id", schema.Int64()).Identity().Computed(),
		schema.DynamicField("slots", schema.Map(entityShape)),
	)

	tables, err := ddl.Tables(ddl.Postgres, mapped.Structure())
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatalf("expected one table, got %d", len(tables))
	}
	slots, held := columnIn(tables[0], "slots")
	if !held || slots.Type != "JSONB" {
		t.Errorf("unexpected column: %#v", slots)
	}
}

func TestARootThatIsOnlyAnIdentityWithChildrenIsFine(t *testing.T) {
	// Not a refusal, and it was one until this test said otherwise: a basket
	// is its lines and nothing else, which is a legitimate aggregate rather
	// than a description with something missing.
	basket := schema.Struct[dynamic.Value]("Basket",
		schema.DynamicField("id", schema.Int64()).Identity().Computed(),
		schema.DynamicField("lines", schema.List(entityShape)),
	)

	tables, err := ddl.Tables(ddl.Postgres, basket.Structure())
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 {
		t.Fatalf("expected the root and its lines, got %d", len(tables))
	}
	if len(tables[0].Columns) != 1 || tables[0].Columns[0].Name != "id" {
		t.Fatalf("expected a root of just its key, got %#v", tables[0].Columns)
	}
}

func TestMySQLRefusesAnUnboundedStringKeyAndPostgresDoesNot(t *testing.T) {
	// MySQL cannot put a TEXT column in a key specification without a prefix
	// length, so a table declaring one is rejected outright -- and a prefix
	// length invented here would make two different keys equal whenever they
	// agreed for that many characters, which is a correctness bug rather than a
	// limitation. So the description has to bound it.
	unbounded := schema.Struct[dynamic.Value]("Unbounded",
		schema.DynamicField("id", schema.UUID()).Identity(),
		schema.DynamicField("what", schema.Text()),
	)

	_, err := ddl.Tables(ddl.MySQL, unbounded.Structure())
	if err == nil {
		t.Fatal("expected mysql to refuse an unbounded string key")
	}
	if !strings.Contains(err.Error(), "maximum length") {
		t.Errorf("expected the reason to say what to do, got %v", err)
	}

	// Postgres indexes text of any length, so refusing there would be
	// refusing something it does perfectly well.
	tables, err := ddl.Tables(ddl.Postgres, unbounded.Structure())
	if err != nil {
		t.Fatalf("expected postgres to accept it, got %v", err)
	}
	identity, held := columnIn(tables[0], "id")
	if !held || identity.Type != "TEXT" {
		t.Errorf("unexpected column: %#v", identity)
	}
}

func TestMySQLRefusesADefaultItWouldReject(t *testing.T) {
	// MySQL takes no default on a TEXT or BLOB column and rejects the
	// statement -- there is no expression form that changes it. So a
	// description asking for both is refused here rather than producing a
	// statement the database throws out, and the remedy is the same one a key
	// needs: bound the text, which makes it a varchar.
	defaulted := schema.Struct[dynamic.Value]("Noted",
		schema.DynamicField("id", schema.Int64()).Identity().Computed(),
		schema.DynamicField("note", schema.Text()).
			WithDefault(dynamic.OfText("none")),
	)

	_, err := ddl.Tables(ddl.MySQL, defaulted.Structure())
	if err == nil {
		t.Fatal("expected mysql to refuse a default on an unbounded text column")
	}
	if !strings.Contains(err.Error(), "maximum length") {
		t.Errorf("expected the reason to say what to do, got %v", err)
	}

	// Bounded, and it goes through: a varchar takes one.
	bounded := schema.Struct[dynamic.Value]("Noted",
		schema.DynamicField("id", schema.Int64()).Identity().Computed(),
		schema.DynamicField("note", schema.Text().Check(schema.MaxLength(64))).
			WithDefault(dynamic.OfText("none")),
	)
	tables, err := ddl.Tables(ddl.MySQL, bounded.Structure())
	if err != nil {
		t.Fatalf("expected a bounded text default to be accepted, got %v", err)
	}
	note, held := columnIn(tables[0], "note")
	if !held || note.Type != "VARCHAR(64)" || note.Default != "'none'" {
		t.Errorf("unexpected column: %#v", note)
	}

	// Postgres puts a default on anything, so refusing there would be
	// refusing something it does perfectly well.
	if _, err := ddl.Tables(ddl.Postgres, defaulted.Structure()); err != nil {
		t.Errorf("expected postgres to accept it, got %v", err)
	}
}
