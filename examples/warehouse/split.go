package warehouse

// One change the closed four cannot express: a reference split into a prefix
// and a serial.
//
// Four changes derive their own value migration, because moving a member needs
// no function. Computing one does, so this says how -- in both directions, and
// per dialect for the rows a database already holds.

import (
	"errors"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

// bounded is a short text column, bounded because MySQL takes no default on an
// unbounded one and cannot key one either.
func bounded(most int) structure.Node {
	return schema.MaxLength(schema.Text(), most).Structure()
}

// splittingTheReference turns "KI-0001" into a prefix and a serial.
//
// The structural part is the ordinary three changes -- two columns added and
// one removed -- because there is no reason to describe a split twice. What
// this adds is the two directions of the value and the statements that move the
// rows, and the statements differ by dialect because the function that takes
// the part before a dash is what each of them spells differently.
var splittingTheReference = evolve.Rewritten{
	Doing: "splitting the reference into a prefix and a serial",
	// The two columns have to exist before the values can move into them,
	// and the one they come from cannot go until after -- which is why these
	// are two lists and not one.
	Adding: []evolve.Change{
		evolve.Added{Field: structure.Field{
			Name:    "prefix",
			Node:    bounded(8),
			Doc:     "Prefix is the depot the reference was issued by.",
			Default: structure.DefaultTo{Value: dynamic.OfText("")},
		}},
		evolve.Added{Field: structure.Field{
			Name:    "serial",
			Node:    bounded(32),
			Doc:     "Serial is the reference within that depot.",
			Default: structure.DefaultTo{Value: dynamic.OfText("")},
		}},
	},
	Dropping: []evolve.Change{evolve.Removed{Name: "reference"}},
	Forward: evolve.Rewrite{
		Value: splitReference,
		Statements: map[string][]string{
			// Each of them spells "the part before the dash" its own way, and
			// the reference column is still there when these run because the
			// structural statements run first.
			"postgres": {
				`update "Pallet" set "prefix" = split_part("reference", '-', 1), ` +
					`"serial" = split_part("reference", '-', 2)`,
			},
			"mysql": {
				"update `Pallet` set `prefix` = substring_index(`reference`, '-', 1), " +
					"`serial` = substring_index(`reference`, '-', -1)",
			},
			"sqlite": {
				`update "Pallet" set "prefix" = substr("reference", 1, instr("reference", '-') - 1), ` +
					`"serial" = substr("reference", instr("reference", '-') + 1)`,
			},
		},
	},
	Back: evolve.Rewrite{
		Value: joinReference,
		Statements: map[string][]string{
			"postgres": {`update "Pallet" set "reference" = "prefix" || '-' || "serial"`},
			"mysql":    {"update `Pallet` set `reference` = concat(`prefix`, '-', `serial`)"},
			"sqlite":   {`update "Pallet" set "reference" = "prefix" || '-' || "serial"`},
		},
	},
}

// splitReference is the value's half of the split.
func splitReference(value dynamic.Object) (dynamic.Object, error) {
	held, present := value.Member("reference")
	if !present {
		// The structural changes have already removed it, so the value it had
		// is gone -- which is what happens to a value that was written under
		// the later version and is being migrated again.
		return value, nil
	}
	text, isText := held.(dynamic.Text)
	if !isText {
		return dynamic.Object{}, errNotAReference
	}
	prefix, serial, found := strings.Cut(text.Value, "-")
	if !found {
		// A reference with no dash is all serial and no prefix, which is what
		// the depots that never used one wrote.
		prefix, serial = "", text.Value
	}
	return replacing(value, map[string]dynamic.Value{
		"prefix": dynamic.OfText(prefix),
		"serial": dynamic.OfText(serial),
	}), nil
}

// joinReference is the value's half of putting it back.
func joinReference(value dynamic.Object) (dynamic.Object, error) {
	prefix, _ := value.Member("prefix")
	serial, _ := value.Member("serial")
	written := textOf(serial)
	if held := textOf(prefix); held != "" {
		written = held + "-" + written
	}
	return replacing(value, map[string]dynamic.Value{
		"reference": dynamic.OfText(written),
	}), nil
}

// replacing sets the named members, keeping the order the object had.
func replacing(value dynamic.Object, held map[string]dynamic.Value) dynamic.Object {
	after := dynamic.Object{Fields: make([]dynamic.Field, 0, len(value.Fields))}
	for _, field := range value.Fields {
		if replaced, named := held[field.Name]; named {
			field.Value = replaced
			delete(held, field.Name)
		}
		after.Fields = append(after.Fields, field)
	}
	for name, remaining := range held {
		after.Fields = append(after.Fields,
			dynamic.Field{Name: name, Value: remaining})
	}
	return after
}

func textOf(value dynamic.Value) string {
	held, isText := value.(dynamic.Text)
	if !isText {
		return ""
	}
	return held.Value
}

var errNotAReference = errors.New("a reference is text, and this is something else")

// Sited is what a pallet looks like from version 1.1.0 on.
type Sited struct {
	Site     string
	Handling string
}

// SitedSchema reads the two columns version 1.1.0 introduced.
var SitedSchema = schema.Struct[Sited]("Sited",
	schema.FieldOf("site", schema.Text(),
		func(held Sited) string { return held.Site },
		func(held *Sited, value string) { held.Site = value }),
	schema.FieldOf("handling", schema.Text(),
		func(held Sited) string { return held.Handling },
		func(held *Sited, value string) { held.Handling = value }),
)
