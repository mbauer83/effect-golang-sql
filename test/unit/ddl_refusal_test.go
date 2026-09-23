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
				schema.DynamicField("name", schema.Text())),
			reason: "names no identity",
		},
		"a computed column with nothing to compute it": {
			shape: schema.Struct[dynamic.Value]("Stamped",
				schema.DynamicField("id", schema.Int64()).Identity().Computed(),
				schema.DynamicField("at", schema.Time()).Computed()),
			reason: "not what computes it",
		},
		"an identity with parts": {
			shape: schema.Struct[dynamic.Value]("Composite",
				schema.DynamicField("key", schema.Struct[dynamic.Value]("Key",
					schema.DynamicField("a", schema.Text()))).Identity()),
			reason: "an identity is one value",
		},
		"an identity that may be absent": {
			shape: schema.Struct[dynamic.Value]("Maybe",
				schema.DynamicField("id", schema.Int64()).Identity().Optional()),
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
	schema.DynamicField("id", schema.UUID().Check(schema.MaxLength(36))).Identity(),
	schema.DynamicField("what", schema.Text()),
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
	if _, err := ddl.Create(ddl.Postgres, compositeParentSchema().Structure()); err == nil {
		t.Fatal("expected a composite-keyed parent holding entities to be refused")
	}
	if _, err := ddl.Alter(ddl.Postgres, compositeParentHistory(t), "1.0.0", "1.1.0"); err == nil {
		t.Fatal("expected Alter to refuse the same description Create refuses")
	}
}

// compositeParentSchema is a thing identified by two fields that holds entities of its
// own, which is the combination refused above.
func compositeParentSchema() schema.Schema[parentThing] {
	return schema.Struct[parentThing]("OwningThing",
		schema.FieldOf("owner", schema.Text().Check(schema.MinLength(1)),
			func(item parentThing) string { return item.Owner },
			func(item *parentThing, owner string) { item.Owner = owner }).Identity(),
		schema.FieldOf("item", schema.Text().Check(schema.MinLength(1)),
			func(item parentThing) string { return item.Item },
			func(item *parentThing, named string) { item.Item = named }).Identity(),
		schema.FieldOf("held", schema.List(childSchema()),
			func(item parentThing) []childThing { return item.Held },
			func(item *parentThing, holding []childThing) { item.Held = holding }),
	)
}

func childSchema() schema.Schema[childThing] {
	return schema.Struct[childThing]("HeldThing",
		schema.FieldOf("id", schema.Text().Check(schema.MinLength(1)),
			func(item childThing) string { return item.ID },
			func(item *childThing, id string) { item.ID = id }).Identity(),
	)
}

// compositeParentHistory is that description with a version to alter to, so Alter has
// a stage to project.
func compositeParentHistory(t *testing.T) evolve.History {
	t.Helper()
	history := evolve.Of("OwningThing").
		Start("1.0.0", compositeParentSchema().Structure()).
		Then("1.1.0", evolve.Addition{Field: structure.Field{
			Name:        "note",
			Node:        schema.Text().Structure(),
			Description: "Note is anything else somebody wants to say.",
			Default: structure.DefaultValue{
				Value: dynamic.OfText(""),
			},
		}})
	if history.Fault() != nil {
		t.Fatal(history.Fault())
	}
	return history
}

type childThing struct {
	ID string
}

type parentThing struct {
	Owner string
	Item  string
	Held  []childThing
}
