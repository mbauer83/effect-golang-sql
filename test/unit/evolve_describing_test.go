package unit

// Whether a history and the description a program holds agree.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

func TestAHistoryAgreesWithTheDescriptionAProgramHolds(t *testing.T) {
	// The check the forward anchor needs. Version one is declared and every
	// later one derived, so a step and a declaration cannot disagree about
	// what the declaration became -- but nothing said the end of the chain is
	// the shape the program reads rows into.
	held := schema.Struct[keptThing]("KeptThing",
		schema.FieldOf("id", schema.Text().Constrained(schema.MinLength(1)),
			func(item keptThing) string { return item.ID },
			func(item *keptThing, id string) { item.ID = id }).Identity(),
		schema.FieldOf("note", schema.Text(),
			func(item keptThing) string { return item.Note },
			func(item *keptThing, note string) { item.Note = note }),
	)

	// A history whose first version is that description, unchanged, agrees.
	agreeing := evolve.Of("KeptThing").Starting("1.0.0", held.Structure())
	if err := agreeing.Describes(held.Structure()); err != nil {
		t.Fatalf("expected the history to agree with itself, got %v", err)
	}

	// A history that has moved on and a program that has not: what an edited
	// step without an edited description looks like.
	moved := agreeing.Then("1.1.0", evolve.Added{Field: structure.Field{
		Name:    "grade",
		Node:    schema.Text().Constrained(schema.MinLength(1), schema.MaxLength(16)).Structure(),
		Doc:     "Grade is what somebody says about it.",
		Default: structure.DefaultTo{Value: dynamic.OfText("unstated")},
	}})
	err := moved.Describes(held.Structure())
	if err == nil {
		t.Fatal("expected a history past the description to be refused")
	}
	if !strings.Contains(err.Error(), "grade") {
		t.Fatalf("expected the field it differs by to be named, got %v", err)
	}

	// And the other way round, which is the case that actually happens: the
	// description grew and nobody added the step.
	grown := schema.Struct[keptThing]("KeptThing",
		schema.FieldOf("id", schema.Text().Constrained(schema.MinLength(1)),
			func(item keptThing) string { return item.ID },
			func(item *keptThing, id string) { item.ID = id }).Identity(),
		schema.FieldOf("note", schema.Text(),
			func(item keptThing) string { return item.Note },
			func(item *keptThing, note string) { item.Note = note }),
		schema.FieldOf("grade", schema.Text(),
			func(item keptThing) string { return item.Grade },
			func(item *keptThing, grade string) { item.Grade = grade }),
	)
	err = agreeing.Describes(grown.Structure())
	if err == nil {
		t.Fatal("expected a description past the history to be refused")
	}
	if !strings.Contains(err.Error(), "grade") {
		t.Fatalf("expected the field it differs by to be named, got %v", err)
	}
}

type keptThing struct {
	ID    string
	Note  string
	Grade string
}

func TestAHistoryOfOneVersionAgreesWithTheDescriptionByConstruction(t *testing.T) {
	// And this is why the guard above arms itself rather than being on from
	// the start. A history whose only version is the description the program
	// holds cannot drift from it: they are one thing. Stating it so that the
	// vacuity is a decision somebody can read rather than a check somebody
	// later mistakes for cover it does not give.
	live := schema.Struct[keptThing]("KeptThing",
		schema.FieldOf("id", schema.Text().Constrained(schema.MinLength(1)),
			func(item keptThing) string { return item.ID },
			func(item *keptThing, id string) { item.ID = id }).Identity(),
		schema.FieldOf("grade", schema.Text(),
			func(item keptThing) string { return item.Grade },
			func(item *keptThing, grade string) { item.Grade = grade }),
	)
	if err := evolve.Of("KeptThing").
		Starting("1.0.0", live.Structure()).
		Describes(live.Structure()); err != nil {
		t.Fatalf("expected one version to agree with itself, got %v", err)
	}
}

func TestAFirstStepAgainstTheLiveDescriptionIsRefusedAndSaysWhy(t *testing.T) {
	// What forces the first version to be frozen, and the moment the guard
	// starts to mean something. The description already carries the change,
	// so the step has nothing to add -- and the refusal has to say that,
	// because read as "this step is wrong" it sends somebody looking at the
	// step. It cost an afternoon once.
	live := schema.Struct[keptThing]("KeptThing",
		schema.FieldOf("id", schema.Text().Constrained(schema.MinLength(1)),
			func(item keptThing) string { return item.ID },
			func(item *keptThing, id string) { item.ID = id }).Identity(),
		schema.FieldOf("grade", schema.Text(),
			func(item keptThing) string { return item.Grade },
			func(item *keptThing, grade string) { item.Grade = grade }),
	)

	history := evolve.Of("KeptThing").
		Starting("1.0.0", live.Structure()).
		Then("1.1.0", evolve.Added{Field: structure.Field{
			Name:    "grade",
			Node:    schema.Text().Structure(),
			Default: structure.DefaultTo{Value: dynamic.OfText("")},
		}})

	fault := history.Fault()
	if fault == nil {
		t.Fatal("expected a step against the live description to be refused")
	}
	if !strings.Contains(fault.Error(), "freeze the first version") {
		t.Fatalf("expected the refusal to say what to do, got %v", fault)
	}
}
