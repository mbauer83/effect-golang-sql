package unit

// The advisory locks, and the question a description answers about relations.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/migrate"
)

func TestTheAdvisoryLocksSayWhatEachDatabaseUnderstands(t *testing.T) {
	// Postgres's is transaction-scoped, so it frees itself and Free says
	// nothing: one fewer thing to get wrong than releasing it by hand.
	taking, arguments := migrate.PostgresAdvisory.Take("logistics.Pallet")
	if !strings.Contains(taking, "pg_advisory_xact_lock") {
		t.Errorf("unexpected statement: %q", taking)
	}
	if len(arguments) != 1 {
		t.Fatalf("expected the key as an argument, got %v", arguments)
	}
	// Numbered, because Postgres's advisory locks are and this one is named.
	numbered, isInteger := arguments[0].(dynamic.Integer)
	if !isInteger || numbered.Value <= 0 {
		t.Errorf("expected a positive number, got %#v", arguments[0])
	}
	if freeing, _ := migrate.PostgresAdvisory.Free("logistics.Pallet"); freeing != "" {
		t.Errorf("expected the transaction to free it, got %q", freeing)
	}

	// MySQL's is session-scoped, which is necessary rather than unfortunate:
	// its DDL commits as it goes, so a transaction-scoped lock would be freed
	// by the first ALTER and the next instance could walk in behind it. So it
	// has to be released by hand.
	taking, arguments = migrate.MySQLNamed.Take("logistics.Pallet")
	if !strings.Contains(taking, "get_lock") || len(arguments) != 1 {
		t.Errorf("unexpected statement: %q %v", taking, arguments)
	}
	if arguments[0] != dynamic.OfText("logistics.Pallet") {
		t.Errorf("expected the name as the key, got %#v", arguments[0])
	}
	freeing, freeingArguments := migrate.MySQLNamed.Free("logistics.Pallet")
	if !strings.Contains(freeing, "release_lock") || len(freeingArguments) != 1 {
		t.Errorf("unexpected statement: %q %v", freeing, freeingArguments)
	}
}

func TestTheLockKeyIsTheSameNumberEveryTime(t *testing.T) {
	// The value has to be stable across releases of the package, because two
	// instances that hashed the same name differently would take two different
	// locks and both proceed -- which is the one failure the lock exists to
	// prevent. So the number is asserted and not merely its determinism.
	_, arguments := migrate.PostgresAdvisory.Take("logistics.Pallet")
	held := arguments[0].(dynamic.Integer).Value

	_, again := migrate.PostgresAdvisory.Take("logistics.Pallet")
	if again[0].(dynamic.Integer).Value != held {
		t.Fatal("the same name hashed to two numbers")
	}
	// A different name is a different lock, so two aggregates migrating at
	// once do not wait for each other.
	_, other := migrate.PostgresAdvisory.Take("logistics.Crate")
	if other[0].(dynamic.Integer).Value == held {
		t.Fatal("two names hashed to one number")
	}
	// The number itself, so a change to the hash is a change somebody sees.
	if held != 4180461568915040706 {
		t.Fatalf("the hash changed: %d", held)
	}
}

func TestWhetherAFieldIsARelationIsTheDescriptionsAnswer(t *testing.T) {
	// One question, one answer, three projections asking it. An object with an
	// identity is an entity and so a relation; one without is a value.
	relation := schema.Struct[dynamic.Value]("Line",
		schema.DescribedField("id", schema.MaxLength(schema.UUID(), 36)).Identity(),
		schema.DescribedField("what", schema.Text()),
	).Structure()
	value := schema.Struct[dynamic.Value]("Address",
		schema.DescribedField("street", schema.Text()),
	).Structure()

	for named, expected := range map[string]struct {
		node    structure.Node
		related bool
	}{
		"an entity":          {node: relation, related: true},
		"a value object":     {node: value, related: false},
		"a list of entities": {node: structure.Sequence{Element: relation}, related: true},
		"a nullable entity":  {node: structure.Nullable{Inner: relation}, related: true},
		"a list of values":   {node: structure.Sequence{Element: value}, related: false},
		"a scalar":           {node: schema.Text().Structure(), related: false},
		"an unresolved name": {node: structure.Reference{Name: "Elsewhere"}, related: false},
		// The deliberate one. A map of entities would need somewhere for its
		// key to live and the description does not name it, so storage stores
		// the map as one value -- and inventing a name for the key would put
		// it in a schema forever.
		"a map of entities": {node: structure.Mapping{
			Key: schema.Text().Structure(), Value: relation,
		}, related: false},
	} {
		_, related := structure.EntityBehind(expected.node)
		if related != expected.related {
			t.Errorf("%s: expected related=%v, got %v", named, expected.related, related)
		}
	}
}
