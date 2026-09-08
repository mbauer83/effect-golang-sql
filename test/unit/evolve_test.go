package unit

// A history: version one declared, every later one derived, and the steps
// running both ways.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

// crateV1 is where the history starts.
var crateV1 = schema.Struct[dynamic.Value]("Crate",
	schema.DescribedField("id", schema.Int64()).Identity().Computed(),
	schema.DescribedField("depot", schema.Text()),
	schema.DescribedField("legacyCode", schema.Text()),
)

// crates is the history: one declaration and two steps, and versions two and
// three are derived from them.
var crates = evolve.Of("logistics.Crate").
	Starting("1.0.0", crateV1.Structure()).
	Then("1.1.0",
		evolve.Renamed{From: "depot", To: "warehouse"},
		evolve.Added{Field: structure.Field{
			Name:    "handling",
			Node:    schema.Text().Structure(),
			Default: structure.DefaultTo{Value: dynamic.OfText("standard")},
		}},
	).
	Then("2.0.0", evolve.Removed{Name: "legacyCode"})

func TestALaterVersionIsDerivedRatherThanDeclaredTwice(t *testing.T) {
	if err := crates.Fault(); err != nil {
		t.Fatal(err)
	}
	if crates.Latest() != "2.0.0" {
		t.Fatalf("expected the last declared version, got %q", crates.Latest())
	}
	// Named and ordered, so a document tagged "1.1.0" has something to match
	// against rather than a position somebody has to know.
	if got := strings.Join(crates.Versions(), ","); got != "1.0.0,1.1.0,2.0.0" {
		t.Fatalf("unexpected versions: %s", got)
	}

	// Version one is what was written.
	first, err := crates.At("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := named(t, first); strings.Join(got, ",") != "id,depot,legacyCode" {
		t.Fatalf("unexpected 1.0.0: %v", got)
	}

	// Version two is the steps applied, and there was nothing to write it in.
	second, err := crates.At("1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	// The rename happened in place, so the order did not change -- which
	// matters because every statement built from this takes its argument order
	// from here.
	if got := named(t, second); strings.Join(got, ",") != "id,warehouse,legacyCode,handling" {
		t.Fatalf("unexpected 1.1.0: %v", got)
	}

	third, err := crates.At("2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := named(t, third); strings.Join(got, ",") != "id,warehouse,handling" {
		t.Fatalf("unexpected 2.0.0: %v", got)
	}
}

func TestAValueIsCarriedForwardAcrossSeveralVersions(t *testing.T) {
	// 1.0.0 to 2.0.0 in one call, across two evolutions: N-1 declared steps answer every pair, because
	// the changes compose.
	var held dynamic.Value = dynamic.Object{Fields: []dynamic.Field{
		{Name: "id", Value: dynamic.OfInteger(7)},
		{Name: "depot", Value: dynamic.OfText("Kiel")},
		{Name: "legacyCode", Value: dynamic.OfText("XK-9")},
	}}

	moved, err := crates.Migrate("1.0.0", "2.0.0", held)
	if err != nil {
		t.Fatal(err)
	}
	object := moved.(dynamic.Object)

	// The rename carried the value rather than dropping it, which is the whole
	// reason a change is declared instead of diffed.
	warehouse, present := object.Member("warehouse")
	if !present || warehouse != dynamic.OfText("Kiel") {
		t.Errorf("expected the renamed value carried, got %#v", object)
	}
	if _, gone := object.Member("depot"); gone {
		t.Error("expected the old name gone")
	}
	// The added field got its default, because a value that predates the field
	// has nothing else it could hold.
	handling, present := object.Member("handling")
	if !present || handling != dynamic.OfText("standard") {
		t.Errorf("expected the default filled in, got %#v", object)
	}
	// And the removed one is gone.
	if _, gone := object.Member("legacyCode"); gone {
		t.Error("expected the removed field gone")
	}

	// What comes out satisfies the version it was migrated to, which is the
	// claim that matters: the target's own schema reads it.
	shape := schema.Dynamic(mustAt(t, crates, "2.0.0"))
	written, err := schema.EncodeJSON(shape, moved)
	if err != nil {
		t.Fatalf("the migrated value does not satisfy 2.0.0: %v", err)
	}
	if !strings.Contains(string(written), `"warehouse":"Kiel"`) {
		t.Errorf("unexpected document: %s", written)
	}
}

func TestAValueIsCarriedBackAndSaysWhatItCannotRestore(t *testing.T) {
	// Backwards, which is the same declared steps inverted. A rename loses
	// nothing; a removal cannot put the values back, only the column.
	var held dynamic.Value = dynamic.Object{Fields: []dynamic.Field{
		{Name: "id", Value: dynamic.OfInteger(7)},
		{Name: "warehouse", Value: dynamic.OfText("Kiel")},
		{Name: "handling", Value: dynamic.OfText("fragile")},
	}}

	moved, err := crates.Migrate("2.0.0", "1.0.0", held)
	if err != nil {
		t.Fatal(err)
	}
	object := moved.(dynamic.Object)

	// The rename came back exactly, because that inverse is total.
	depot, present := object.Member("depot")
	if !present || depot != dynamic.OfText("Kiel") {
		t.Errorf("expected the rename undone, got %#v", object)
	}
	// The field version three added is gone again.
	if _, gone := object.Member("handling"); gone {
		t.Error("expected the added field removed on the way back")
	}
	// And legacyCode is absent rather than invented: the column comes back,
	// the values do not, which is what makes a down migration best-effort.
	if _, invented := object.Member("legacyCode"); invented {
		t.Error("expected the dropped column's values not to be invented")
	}
	// The order the inverses run in, which is reversed twice over: the steps
	// backwards, and the changes within each step backwards too. A step that
	// renamed one field and added another has to remove the addition before
	// undoing the rename, or the inverse would look for a field under a name
	// it no longer has.
	back, err := crates.Between("2.0.0", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 {
		t.Fatalf("expected three inverses, got %d", len(back))
	}
	// Step three undone first.
	restored, isAdded := back[0].(evolve.Added)
	if !isAdded || restored.Field.Name != "legacyCode" {
		t.Fatalf("expected the removal undone first, got %#v", back[0])
	}
	// And the restored field is optional, because a required column with no
	// values is one no row satisfies.
	if !restored.Field.Optional {
		t.Errorf("expected the restored field optional, got %#v", restored.Field)
	}
	// Then step two, its own changes in reverse: the addition dropped, then
	// the rename undone.
	if dropped, isRemoved := back[1].(evolve.Removed); !isRemoved || dropped.Name != "handling" {
		t.Errorf("expected the addition dropped second, got %#v", back[1])
	}
	renamed, isRenamed := back[2].(evolve.Renamed)
	if !isRenamed || renamed.From != "warehouse" || renamed.To != "depot" {
		t.Errorf("expected the rename undone last, got %#v", back[2])
	}
}

func TestTheSameVersionIsNoChangeAtAll(t *testing.T) {
	changes, err := crates.Between("1.1.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected nothing to do, got %d changes", len(changes))
	}
}

func mustAt(t *testing.T, history evolve.History, version string) structure.Node {
	t.Helper()
	node, err := history.At(version)
	if err != nil {
		t.Fatal(err)
	}
	return node
}
