package unit

// What a history refuses, and where.
//
// Go cannot check a step's totality at compile time: a version is a value and
// not a type, and encoding a version's columns in types would need a type
// parameter per column and be unusable. So the check is at assembly, the way an
// ambiguous route and an endpoint declaration are checked -- and a test that
// asks Fault is a test that has checked it.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

func TestAStepThatDoesNotAddUpIsRefusedAtAssembly(t *testing.T) {
	for named, expected := range map[string]struct {
		history evolve.History
		reason  string
	}{
		"renaming a field that is not there": {
			history: evolve.Of("t.Crate").Start("1.0.0", crateV1.Structure()).
				Then("1.1.0", evolve.Rename{From: "absent", To: "present"}),
			reason: "no such field",
		},
		"renaming onto a name already taken": {
			history: evolve.Of("t.Crate").Start("1.0.0", crateV1.Structure()).
				Then("1.1.0", evolve.Rename{From: "depot", To: "legacyCode"}),
			reason: "already has a field",
		},
		"removing a field that is not there": {
			history: evolve.Of("t.Crate").Start("1.0.0", crateV1.Structure()).
				Then("1.1.0", evolve.Removal{Name: "absent"}),
			reason: "no such field",
		},
		"adding a field that is already there": {
			history: evolve.Of("t.Crate").Start("1.0.0", crateV1.Structure()).
				Then("1.1.0", evolve.Addition{Field: structure.Field{
					Name: "depot", Node: schema.Text().Structure(), Optional: true,
				}}),
			reason: "already has a field",
		},
		"adding a required field with nowhere for its values to come from": {
			history: evolve.Of("t.Crate").Start("1.0.0", crateV1.Structure()).
				Then("1.1.0", evolve.Addition{Field: structure.Field{
					Name: "handling", Node: schema.Text().Structure(),
				}}),
			reason: "no value for the rows that already exist",
		},
		"a version that changes nothing": {
			history: evolve.Of("t.Crate").Start("1.0.0", crateV1.Structure()).Then("1.1.0"),
			reason:  "not a version",
		},
		"changing the shape of a field that is not there": {
			history: evolve.Of("t.Crate").Start("1.0.0", crateV1.Structure()).
				Then("1.1.0", evolve.Retype{Name: "absent", Node: schema.Int64().Structure()}),
			reason: "no such field",
		},
	} {
		err := expected.history.Fault()
		if err == nil {
			t.Errorf("expected %s to be refused", named)
			continue
		}
		if !strings.Contains(err.Error(), expected.reason) {
			t.Errorf("%s: expected the reason to mention %q, got %v",
				named, expected.reason, err)
		}
		// And a refused history answers nothing rather than answering
		// something built on a step that did not add up.
		if _, err := expected.history.At("1.1.0"); err == nil {
			t.Errorf("%s: a refused history handed out a version anyway", named)
		}
		if _, err := ddl.Alter(ddl.SQLite, expected.history, "1.0.0", "1.1.0"); err == nil {
			t.Errorf("%s: a refused history handed out statements anyway", named)
		}
	}
}

func TestAVersionThatIsNotThereIsSaidRatherThanGuessed(t *testing.T) {
	for _, version := range []string{"", "3.0.0", "1", "v1.0.0"} {
		if _, err := crates.At(version); err == nil {
			t.Errorf("expected version %q to be refused", version)
		}
	}
	if _, err := crates.Between("1.0.0", "9.9.9"); err == nil {
		t.Error("expected a migration to a version that is not there to be refused")
	}
	// The message says what there is, because a caller that asked for the
	// wrong one needs to know the range rather than that it was wrong.
	_, err := crates.At("9.9.9")
	if !strings.Contains(err.Error(), "1.0.0, 1.1.0, 2.0.0") {
		t.Errorf("expected the versions there are to be named, got %v", err)
	}
}

func TestSQLiteRefusesToChangeAColumnsTypeInPlace(t *testing.T) {
	// It cannot: SQLite's ALTER TABLE renames, adds and drops, and the way to
	// change a type is a new table, a copy, a drop and a rename. That is four
	// statements and a decision about what to do with values that no longer
	// fit, so it is the caller's to write rather than something to emit as if
	// it were one change.
	retyping := evolve.Of("t.Crate").Start("1.0.0", crateV1.Structure()).
		Then("1.1.0", evolve.Retype{Name: "legacyCode", Node: schema.Int64().Structure()})
	if err := retyping.Fault(); err != nil {
		t.Fatal(err)
	}

	_, err := ddl.Alter(ddl.SQLite, retyping, "1.0.0", "1.1.0")
	if err == nil {
		t.Fatal("expected sqlite to refuse a change of type")
	}
	if !strings.Contains(err.Error(), "make a new column, copy, and drop") {
		t.Errorf("expected the reason to say what to do instead, got %v", err)
	}

	// The two dialects that can do it spell it differently, and the difference
	// is not cosmetic: MySQL's form restates the whole definition, so anything
	// left out of the restatement is lost.
	postgres, err := ddl.Alter(ddl.Postgres, retyping, "1.0.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(postgres[0], `alter column "legacyCode" type bigint`) {
		t.Errorf("unexpected postgres statement: %q", postgres[0])
	}
	mysql, err := ddl.Alter(ddl.MySQL, retyping, "1.0.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mysql[0], "modify column `legacyCode` bigint not null") {
		t.Errorf("unexpected mysql statement: %q", mysql[0])
	}
}

func TestAMigrationOfSomethingThatIsNotAnObjectIsRefused(t *testing.T) {
	if err := evolve.Of("t.Scalar").Start("1.0.0", schema.Text().Structure()).Fault(); err == nil {
		t.Error("expected a scalar to be refused: a version is a set of named fields")
	}
	if err := evolve.Of("").Start("1.0.0", crateV1.Structure()).Fault(); err == nil {
		t.Error("expected a history with no name to be refused")
	}
	if err := evolve.Of("t.Crate").Start("", crateV1.Structure()).Fault(); err == nil {
		t.Error("expected a version with no name to be refused")
	}
	// And a name used twice, because two versions of one name is two things a
	// document tagged with it could mean.
	twice := evolve.Of("t.Crate").Start("1.0.0", crateV1.Structure()).
		Then("1.0.0", evolve.Removal{Name: "legacyCode"})
	if err := twice.Fault(); err == nil {
		t.Error("expected a version name used twice to be refused")
	}
	if _, err := crates.Migrate("1.0.0", "1.1.0", dynamic.OfText("not an object")); err == nil {
		t.Error("expected a value that is not an object to be refused")
	}
}
