package warehouse

// One change the closed four cannot express: a reference split into a prefix
// and a serial.
//
// Four changes derive their own value migration, because moving a member needs
// no function. Computing one does, so this says how, in both directions: what
// happens to a value in memory, and what happens to the rows a database already
// holds.
//
// Both halves are Go. The rows could have been moved with a statement --
// Postgres has split_part, MySQL substring_index, SQLite substr with instr --
// and that would have been three declarations of one change, each to be got
// right separately, and none of them able to do anything a statement cannot.

import (
	"errors"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

// textUpTo is a short text column, textUpTo because MySQL takes no default on an
// unbounded one and cannot key one either.
func textUpTo(most int) structure.Node {
	return schema.Text().Check(schema.MaxLength(most)).Structure()
}

// referenceSplit turns "KI-0001" into a prefix and a serial.
//
// The structural part is the ordinary three changes -- two columns added and
// one removed -- because there is no reason to describe a split twice. What
// this adds is the two directions of the value and the statements that move the
// rows, and the statements differ by dialect because the function that takes
// the part before a dash is what each of them spells differently.
var referenceSplit = evolve.Recomputation{
	Name: "splitting the reference into a prefix and a serial",
	// The two columns have to exist before the values can move into them,
	// and the one they come from cannot go until after -- which is why these
	// are two lists and not one.
	Additions: []evolve.Change{
		evolve.Addition{Field: structure.Field{
			Name:    "prefix",
			Node:    textUpTo(8),
			Doc:     "Prefix is the depot the reference was issued by.",
			Default: structure.DefaultValue{Value: dynamic.OfText("")},
		}},
		evolve.Addition{Field: structure.Field{
			Name:    "serial",
			Node:    textUpTo(32),
			Doc:     "Serial is the reference within that depot.",
			Default: structure.DefaultValue{Value: dynamic.OfText("")},
		}},
	},
	Removals: []evolve.Change{evolve.Removal{Name: "reference"}},
	Forward: evolve.Rewrite{
		Value: splitReference,
		Rows:  splitRows,
	},
	Back: evolve.Rewrite{
		Value: joinReference,
		Rows:  joinRows,
	},
}

// splitReference is the value's half of the split.
func splitReference(value dynamic.Object) (dynamic.Object, error) {
	member, present := value.Member("reference")
	if !present {
		// The structural changes have already removed it, so the value it had
		// is gone -- which is what happens to a value that was written under
		// the later version and is being migrated again.
		return value, nil
	}
	text, isText := member.(dynamic.Text)
	if !isText {
		return dynamic.Object{}, errNotAReference
	}
	prefix, serial, found := strings.Cut(text.Value, "-")
	if !found {
		// A reference with no dash is all serial and no prefix, which is what
		// the depots that never used one wrote.
		prefix, serial = "", text.Value
	}
	return replaceMembers(value, map[string]dynamic.Value{
		"prefix": dynamic.OfText(prefix),
		"serial": dynamic.OfText(serial),
	}), nil
}

// joinReference is the value's half of putting it back.
func joinReference(value dynamic.Object) (dynamic.Object, error) {
	prefix, _ := value.Member("prefix")
	serial, _ := value.Member("serial")
	reference := textOf(serial)
	if prefixText := textOf(prefix); prefixText != "" {
		reference = prefixText + "-" + reference
	}
	return replaceMembers(value, map[string]dynamic.Value{
		"reference": dynamic.OfText(reference),
	}), nil
}

// replaceMembers sets the named members, keeping the order the object had.
func replaceMembers(value dynamic.Object, entries map[string]dynamic.Value) dynamic.Object {
	after := dynamic.Object{Fields: make([]dynamic.Field, 0, len(value.Fields))}
	for _, field := range value.Fields {
		if replacement, found := entries[field.Name]; found {
			field.Value = replacement
			delete(entries, field.Name)
		}
		after.Fields = append(after.Fields, field)
	}
	for name, replacement := range entries {
		after.Fields = append(after.Fields,
			dynamic.Field{Name: name, Value: replacement})
	}
	return after
}

func textOf(value dynamic.Value) string {
	// dynamic.TextOf and not a type assertion, because a driver may hand a
	// character column back as bytes: MySQL does and Postgres does not, and a
	// reference read as "" would split into nothing at all.
	text, _ := dynamic.TextOf(value)
	return text
}

var errNotAReference = errors.New("a reference is text, and this is something else")

// SiteHandling is what a pallet looks like from version 1.1.0 on.
type SiteHandling struct {
	Site     string
	Handling string
}

// SiteHandlingSchema reads the two columns version 1.1.0 introduced.
var SiteHandlingSchema = schema.Struct[SiteHandling]("SiteHandling",
	schema.FieldOf("site", schema.Text(),
		func(pallet SiteHandling) string { return pallet.Site },
		func(pallet *SiteHandling, value string) { pallet.Site = value }),
	schema.FieldOf("handling", schema.Text(),
		func(pallet SiteHandling) string { return pallet.Handling },
		func(pallet *SiteHandling, value string) { pallet.Handling = value }),
)
