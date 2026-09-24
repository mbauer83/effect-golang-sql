package unit

// Which tables an aggregate is, and what is in them.
//
// The derivation is the same in every dialect, so it is checked once here on
// the data rather than three times on strings. What the dialects spell
// differently is checked separately, and that the statements actually work is
// checked by running them.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/ddl"
)

// tablesByName is the tables the order aggregate becomes, by name.
func tablesByName(t *testing.T, dialect ddl.Dialect, node structure.Node) map[string]ddl.Table {
	t.Helper()
	tables, err := ddl.Tables(dialect, node)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ddl.Table{}
	for _, table := range tables {
		byName[table.Name] = table
	}
	return byName
}

func TestAnAggregateBecomesATablePerEntityAndNotOne(t *testing.T) {
	// An object with an identity is a thing and gets a table; an object
	// without one is a value belonging to whatever holds it and lives in that
	// thing's row. So the order and its lines are two tables and the address
	// is a column.
	tables, err := ddl.Tables(ddl.Postgres, order.Structure())
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 {
		t.Fatalf("expected two tables, got %d", len(tables))
	}
	// The parent first, because a child cannot reference a table that is not
	// there yet -- so this is the order the statements have to run in.
	if tables[0].Name != "Order" || tables[1].Name != "OrderLine" {
		t.Fatalf("unexpected tables: %s, %s", tables[0].Name, tables[1].Name)
	}

	byName := map[string]ddl.Table{tables[0].Name: tables[0], tables[1].Name: tables[1]}
	// The value object is one column holding a document, not a table.
	shipTo, held := columnIn(byName["Order"], "shipTo")
	if !held {
		t.Fatal("the value object is not a column")
	}
	if shipTo.Type != "JSONB" {
		t.Errorf("expected the value object in a document column, got %q", shipTo.Type)
	}
	if _, wrong := byName["Address"]; wrong {
		t.Error("the value object got a table of its own")
	}
}

func TestTheChildCarriesTheReferenceAndTheOrderItHad(t *testing.T) {
	byName := tablesByName(t, ddl.Postgres, order.Structure())
	line := byName["OrderLine"]

	// The reference to the parent, derived and named for it.
	reference, held := columnIn(line, "Order_id")
	if !held {
		t.Fatalf("the child has no reference to its parent: %#v", line.Columns)
	}
	if reference.Type != "BIGINT" {
		t.Errorf("expected the parent's key type, got %q", reference.Type)
	}
	if len(line.ForeignKeys) != 1 {
		t.Fatalf("expected one foreign key, got %d", len(line.ForeignKeys))
	}
	key := line.ForeignKeys[0]
	if key.Table != "Order" || len(key.Targets) != 1 || key.Targets[0] != "id" {
		t.Errorf("unexpected foreign key: %#v", key)
	}
	// Cascading, because a child entity has no life without its root: a row
	// that outlived its parent would be unreachable.
	if !key.Cascade {
		t.Error("expected the child to go when the parent does")
	}
	// And an index on it, because looking a parent's children up is a question
	// the schema itself asks.
	if len(line.Indexes) != 1 || line.Indexes[0].Columns[0] != "Order_id" {
		t.Errorf("unexpected indexes: %#v", line.Indexes)
	}

	// The lines were a list, and a list is ordered where a table is not -- so
	// without this the list read back would not be the list written.
	if _, ordered := columnIn(line, "position"); !ordered {
		t.Errorf("expected an ordered child to keep its order: %#v", line.Columns)
	}
}

func TestTheKeysAreWhatTheDescriptionSaidTheyWere(t *testing.T) {
	byName := tablesByName(t, ddl.Postgres, order.Structure())

	root := byName["Order"]
	if len(root.PrimaryKey) != 1 || root.PrimaryKey[0] != "id" {
		t.Fatalf("unexpected key: %v", root.PrimaryKey)
	}
	// Generated, because the description says the identity is computed: the
	// one computed column this projection knows how to write.
	identity, held := columnIn(root, "id")
	if !held || !identity.Identity {
		t.Fatalf("expected a generated key, got %#v", identity)
	}

	// The line's identity is the application's -- Identity without Computed --
	// so it is an ordinary not-null column that happens to be the key.
	line := byName["OrderLine"]
	lineKey, held := columnIn(line, "id")
	if !held || lineKey.Identity {
		t.Fatalf("expected an application-supplied key, got %#v", lineKey)
	}
	if lineKey.Nullable {
		t.Error("a key that may be absent identifies nothing")
	}
	// And it is the parent and the identity together, because a line's
	// identity distinguishes it among its order's lines rather than among
	// every order's. A single-column key there makes a client-chosen identity
	// global: two people who pick the same one collide, and depending on how
	// the row is written one is refused or one silently replaces the other.
	if len(line.PrimaryKey) != 2 ||
		line.PrimaryKey[0] != "Order_id" || line.PrimaryKey[1] != "id" {
		t.Fatalf("expected a child keyed by its parent and its identity, got %v",
			line.PrimaryKey)
	}
}

func TestWhatTheDescriptionSaysAndDDLCannotStateBecomesAComment(t *testing.T) {
	// Two enforcements would be two rules to keep in step, and every dialect
	// spells a CHECK differently. The schema layer already enforces these on
	// the way in and out, so the table says what it cannot keep.
	byName := tablesByName(t, ddl.Postgres, order.Structure())
	reference, _ := columnIn(byName["Order"], "reference")

	if len(reference.Notes) == 0 {
		t.Fatalf("expected the format noted: %#v", reference)
	}
	if !strings.Contains(strings.Join(reference.Notes, " "), "uuid") {
		t.Errorf("unexpected notes: %v", reference.Notes)
	}
	quantity, _ := columnIn(byName["OrderLine"], "quantity")
	if !strings.Contains(strings.Join(quantity.Notes, " "), "at least 1") {
		t.Errorf("unexpected notes: %v", quantity.Notes)
	}
}

func columnIn(table ddl.Table, name string) (ddl.Column, bool) {
	for _, column := range table.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return ddl.Column{}, false
}

func TestARangeTheColumnTypeAlreadyKeepsIsNotRestatedAsProse(t *testing.T) {
	// A description states the range its width implies, because JSON Schema
	// has no integer widths and no other way to say it. A column typed
	// "INTEGER" says it in the type, so restating it would be noise in a file
	// other people read -- and noise that looked like a rule somebody chose.
	byName := tablesByName(t, ddl.Postgres, order.Structure())
	quantity, _ := columnIn(byName["OrderLine"], "quantity")

	notes := strings.Join(quantity.Notes, "; ")
	if strings.Contains(notes, "2147483647") || strings.Contains(notes, "e+") {
		t.Errorf("the width's own range was restated: %q", notes)
	}
	// The narrower bound the author asked for survives, because that is not
	// something the type keeps.
	if notes != "at least 1" {
		t.Errorf("expected only the author's bound, got %q", notes)
	}
}

func TestAnIdentityOfSeveralFieldsIsTheWholeKey(t *testing.T) {
	// An identity is often only unique within something else. A disc somebody
	// owns is identified by whose it is and which of theirs: the identity its
	// owner chose is theirs to choose, so two people may choose the same one
	// and neither is the other's. A description that named only the second
	// half would say that identity is unique across everybody, and a store
	// built on it lets one person's write find, change or replace another's.
	owned := schema.Struct[shelfEntry]("ShelvedThing",
		schema.FieldOf("owner", schema.Text().Check(schema.MinLength(1)),
			func(item shelfEntry) string { return item.Owner },
			func(item *shelfEntry, owner string) { item.Owner = owner }).Identity(),
		schema.FieldOf("item", schema.Text().Check(schema.MinLength(1)),
			func(item shelfEntry) string { return item.Item },
			func(item *shelfEntry, named string) { item.Item = named }).Identity(),
		schema.FieldOf("note", schema.Text(),
			func(item shelfEntry) string { return item.Note },
			func(item *shelfEntry, note string) { item.Note = note }),
	)

	byName := tablesByName(t, ddl.Postgres, owned.Structure())
	table := byName["ShelvedThing"]

	if len(table.PrimaryKey) != 2 ||
		table.PrimaryKey[0] != "owner" || table.PrimaryKey[1] != "item" {
		t.Fatalf("expected both fields in the key, in the order declared, got %v",
			table.PrimaryKey)
	}
	// Both are keys, so neither is nullable and an update shape leaves both
	// out: a key selects the row rather than being changed by it.
	for _, named := range []string{"owner", "item"} {
		column, held := columnIn(table, named)
		if !held {
			t.Fatalf("expected a %q column, got %#v", named, table.Columns)
		}
		if column.Nullable {
			t.Errorf("a key that may be absent identifies nothing: %q", named)
		}
	}
}

// shelfEntry is a thing identified by whose it is and which of theirs.
type shelfEntry struct {
	Owner string
	Item  string
	Note  string
}
