package ddl

// What the description says and DDL has no way to state.
//
// Comments, because a comment is honest about not being enforced. An invented
// CHECK would be a rule nobody asked for, spelled differently by every dialect,
// and enforced twice -- while the schema layer already keeps these on the way
// in and on the way out.

import (
	"math"
	"strconv"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// notesFor is what the description says and DDL has no way to state.
//
// A comment, because a comment is honest about not being enforced. An invented
// CHECK would be a rule nobody asked for, spelled differently by every dialect,
// and enforced twice -- while the schema layer already enforces these on the
// way in and on the way out.
func notesFor(node structure.Node) []string {
	scalar, isScalar := node.(structure.Scalar)
	if !isScalar {
		if nullable, wrapped := node.(structure.Nullable); wrapped {
			return notesFor(nullable.Inner)
		}
		return nil
	}

	notes := make([]string, 0, len(scalar.Constraints)+1)
	if scalar.Format != "" {
		notes = append(notes, "format: "+scalar.Format)
	}
	for _, constraint := range scalar.Constraints {
		if isImplied(scalar.Precision, constraint) {
			continue
		}
		if note := constraintNote(constraint); note != "" {
			notes = append(notes, note)
		}
	}
	if len(notes) == 0 {
		return nil
	}
	return notes
}

func constraintNote(constraint structure.Constraint) string {
	switch shape := constraint.(type) {
	case structure.AtLeast:
		return "at least " + number(shape.Value)
	case structure.AtMost:
		return "at most " + number(shape.Value)
	case structure.Above:
		return "above " + number(shape.Value)
	case structure.Below:
		return "below " + number(shape.Value)
	case structure.MinLength:
		return "at least " + pluralise(shape.Value, "character")
	case structure.MaxLength:
		return "at most " + pluralise(shape.Value, "character")
	case structure.Pattern:
		return "matching " + shape.Expression
	case structure.MinItems:
		return "at least " + pluralise(shape.Value, "item")
	case structure.MaxItems:
		return "at most " + pluralise(shape.Value, "item")
	default:
		return ""
	}
}

// isImplied reports whether a bound is one the column's own type already keeps.
//
// A description states the range its width implies, because a format with no
// integer widths -- JSON Schema -- has no other way to say it. A column typed
// "integer" says it in the type, so restating it as a comment would be noise in
// a file other people read, and noise that looked like a rule somebody chose.
//
// Exact rather than a guess: the bound is skipped only when it is precisely the
// width's own limit, so a narrower range the author asked for survives.
func isImplied(precision structure.Precision, constraint structure.Constraint) bool {
	low, high, known := spans(precision)
	if !known {
		return false
	}
	switch shape := constraint.(type) {
	case structure.AtLeast:
		return shape.Value == low
	case structure.AtMost:
		return shape.Value == high
	default:
		return false
	}
}

// spans is the range a width can hold.
func spans(precision structure.Precision) (low float64, high float64, known bool) {
	switch precision {
	case structure.Int8Bits:
		return -128, 127, true
	case structure.Int16Bits:
		return -32768, 32767, true
	case structure.Int32Bits:
		return -2147483648, 2147483647, true
	case structure.Uint8Bits:
		return 0, 255, true
	case structure.Uint16Bits:
		return 0, 65535, true
	case structure.Uint32Bits:
		return 0, 4294967295, true
	default:
		// The 64-bit widths are not here on purpose: a float64 cannot hold
		// their limits exactly, so comparing against one would be comparing
		// against a number that is not the limit.
		return 0, 0, false
	}
}

func pluralise(value int, noun string) string {
	if value == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(value) + " " + noun + "s"
}

// number writes a bound the way a person reads one.
//
// A whole number without an exponent, because "at least 1" belongs in a file
// somebody reads and "at least 1e+00" does not.
func number(value float64) string {
	if value == math.Trunc(value) && math.Abs(value) < 1e15 {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}
