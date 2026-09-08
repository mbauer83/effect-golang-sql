package warehouse

// What the pallet has been.
//
// Version one is the description in warehouse.go; every later one is derived
// from the steps here, so there is no second declaration to keep in step with
// them. The same steps produce the statements that move the tables and the
// function that moves a value.
//
// The versions are named and not numbered. A position would renumber every
// later version whenever one was inserted, and would give a document tagged
// "2.0.0" nothing to match against but a convention -- where a name is what the
// document, the service that wrote it and the service that reads it already
// agree on.
//
// Between them these steps do everything a step can: a field added, renamed,
// changed and removed, and a relation added, changed and removed.

import (
	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

// Inspection is a look at a pallet. An entity, so it has a table of its own.
type Inspection struct {
	ID     string
	By     string
	Passed bool
}

// InspectionSchema describes one.
var InspectionSchema = schema.Struct[Inspection]("PalletInspection",
	schema.FieldOf("id", schema.MaxLength(schema.UUID(), 36),
		func(held Inspection) string { return held.ID },
		func(held *Inspection, value string) { held.ID = value }).Identity(),
	schema.FieldOf("by", schema.MaxLength(schema.MinLength(schema.Text(), 1), 64),
		func(held Inspection) string { return held.By },
		func(held *Inspection, value string) { held.By = value }),
	schema.FieldOf("passed", schema.Bool(),
		func(held Inspection) bool { return held.Passed },
		func(held *Inspection, value bool) { held.Passed = value }),
).Documented("PalletInspection is one inspection of a pallet.")

// Pallets is the pallet's history.
var Pallets = evolve.Of("logistics.Pallet").
	Starting("1.0.0", PalletSchema.Structure()).
	// A field renamed and a field added. The rename is the change a diff
	// cannot see: it would have found a column gone and a column arrived, and
	// thrown away every pallet's location.
	Then("1.1.0",
		evolve.Renamed{From: "warehouse", To: "site"},
		evolve.Added{Field: structure.Field{
			Name: "handling",
			// Bounded, because MySQL takes no default on an unbounded text
			// column and would reject the statement.
			Node: schema.MaxLength(schema.MinLength(schema.Text(), 1), 32).Structure(),
			Doc:  "Handling is how the pallet is to be moved.",
			// A default, because the pallets that already exist have no value
			// for it -- and without one a database will not add a not-null
			// column to a table that has rows in it.
			Default: structure.DefaultTo{Value: dynamic.OfText("standard")},
		}},
	).
	// A relation added and a field removed. The relation is a table appearing,
	// not a column: an inspection has an identity of its own, so it is a thing
	// rather than a part of a pallet. At most one to begin with.
	Then("2.0.0",
		evolve.Added{Field: structure.Field{
			Name: "inspection",
			Node: structure.Nullable{Inner: InspectionSchema.Structure()},
			Doc:  "Inspection is the last look at this pallet, if there was one.",
		}},
		evolve.Removed{Name: "storedAt"},
	).
	// The relation changed: pallets get looked at more than once, so one
	// becomes many. The child table stays where it is -- the entity is the
	// same entity -- and what changes is whether a pallet may have more than
	// one of them.
	Then("2.1.0",
		evolve.Retyped{
			Name: "inspection",
			Node: structure.Sequence{Element: InspectionSchema.Structure()},
		},
	).
	// The relation removed: inspections moved to a service of their own, so
	// the table goes.
	Then("3.0.0", evolve.Removed{Name: "inspection"}).
	// A field split into two, which is the change the closed four cannot
	// express: they move values and this one computes them. So it says how, in
	// both directions, and what the database has to do to the rows it already
	// holds -- per dialect, because there is no dialect-neutral way to say
	// "take the part before the dash".
	Then("3.1.0", splittingTheReference).
	// A field's shape changed: serials got longer. Last, because SQLite cannot
	// change a column's type at all, so every step before this one runs
	// everywhere and this one runs where a real database is.
	Then("4.0.0",
		evolve.Retyped{
			Name: "serial",
			Node: schema.MaxLength(schema.MinLength(schema.Text(), 1), 64).Structure(),
		},
	)
