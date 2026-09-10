package unit

// What the DDL projection refuses, and why each refusal beats the alternative.
//
// Every one of these is a table that could have been emitted, and would have
// meant something the description did not say.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

func TestTheProjectionRefusesWhatTheDescriptionDoesNotSay(t *testing.T) {
	for named, expected := range map[string]struct {
		shape  schema.Schema[dynamic.Value]
		reason string
	}{
		"no identity": {
			shape: schema.Struct[dynamic.Value]("Anonymous",
				schema.DescribedField("name", schema.Text())),
			reason: "names no identity",
		},
		"a computed column with nothing to compute it": {
			shape: schema.Struct[dynamic.Value]("Stamped",
				schema.DescribedField("id", schema.Int64()).Identity().Computed(),
				schema.DescribedField("at", schema.Time()).Computed()),
			reason: "not what computes it",
		},
		"an identity with parts": {
			shape: schema.Struct[dynamic.Value]("Composite",
				schema.DescribedField("key", schema.Struct[dynamic.Value]("Key",
					schema.DescribedField("a", schema.Text()))).Identity()),
			reason: "an identity is one value",
		},
		"an identity that may be absent": {
			shape: schema.Struct[dynamic.Value]("Maybe",
				schema.DescribedField("id", schema.Int64()).Identity().Optional()),
			reason: "identifies nothing",
		},
	} {
		_, err := ddl.Tables(ddl.Postgres, expected.shape.Structure())
		if err == nil {
			t.Errorf("expected %s to be refused", named)
			continue
		}
		if !strings.Contains(err.Error(), expected.reason) {
			t.Errorf("%s: expected the reason to mention %q, got %v",
				named, expected.reason, err)
		}
	}
}

// entityShape is a minimal entity, for the cases that need one. Its identity
// is bounded, because MySQL cannot key an unbounded string.
var entityShape = schema.Struct[dynamic.Value]("Line",
	schema.DescribedField("id", schema.UUID().Constrained(schema.MaxLength(36))).Identity(),
	schema.DescribedField("what", schema.Text()),
)

func TestAParentOfSeveralIdentitiesHoldingEntitiesIsRefused(t *testing.T) {
	// A child references its parent by one column and nothing here writes a
	// reference of several, so this is refused rather than projected into a
	// foreign key pointing at half a key -- which a database accepts and then
	// enforces nothing with, so the first anybody learns of it is a child row
	// outliving its parent.
	//
	// Refused where the reference is built, which is the one function Create
	// and Alter share: a description one accepted and the other did not would
	// be a table that cannot be migrated or a migration that cannot be made.
	if _, err := ddl.Create(ddl.Postgres, owningSeveral().Structure()); err == nil {
		t.Fatal("expected a composite-keyed parent holding entities to be refused")
	}
	if _, err := ddl.Alter(ddl.Postgres, holdingHistory(t), "1.0.0", "1.1.0"); err == nil {
		t.Fatal("expected Alter to refuse the same description Create refuses")
	}
}

// owningSeveral is a thing identified by two fields that holds entities of its
// own, which is the combination refused above.
func owningSeveral() schema.Schema[owningThing] {
	return schema.Struct[owningThing]("OwningThing",
		schema.FieldOf("owner", schema.Text().Constrained(schema.MinLength(1)),
			func(item owningThing) string { return item.Owner },
			func(item *owningThing, owner string) { item.Owner = owner }).Identity(),
		schema.FieldOf("item", schema.Text().Constrained(schema.MinLength(1)),
			func(item owningThing) string { return item.Item },
			func(item *owningThing, named string) { item.Item = named }).Identity(),
		schema.FieldOf("held", schema.List(heldSchema()),
			func(item owningThing) []heldThing { return item.Held },
			func(item *owningThing, holding []heldThing) { item.Held = holding }),
	)
}

func heldSchema() schema.Schema[heldThing] {
	return schema.Struct[heldThing]("HeldThing",
		schema.FieldOf("id", schema.Text().Constrained(schema.MinLength(1)),
			func(item heldThing) string { return item.ID },
			func(item *heldThing, id string) { item.ID = id }).Identity(),
	)
}

// holdingHistory is that description with a version to alter to, so Alter has
// a stage to project.
func holdingHistory(t *testing.T) evolve.History {
	t.Helper()
	history := evolve.Of("OwningThing").
		Starting("1.0.0", owningSeveral().Structure()).
		Then("1.1.0", evolve.Added{Field: structure.Field{
			Name: "note",
			Node: schema.Text().Structure(),
			Doc:  "Note is anything else somebody wants to say.",
			Default: structure.DefaultTo{
				Value: dynamic.OfText(""),
			},
		}})
	if history.Fault() != nil {
		t.Fatal(history.Fault())
	}
	return history
}

type heldThing struct {
	ID string
}

type owningThing struct {
	Owner string
	Item  string
	Held  []heldThing
}
