package ddl

// How wide an integer column is: as narrow as the range the domain states.

import (
	"math"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// narrowed is an integer with the narrowest signed width that holds the range
// its constraints state, never wider than the width it declared.
//
// A year stated as 0 to 9999 is a small integer whatever Go type holds it, and
// the column is sized by the range the domain promised rather than by the Go
// type it happens to be. A range stated on an unsigned sixty-four-bit integer
// fits a signed width too, which is what makes such a field storable where
// Postgres has no unsigned type.
func narrowed(scalar structure.Scalar) structure.Scalar {
	if scalar.Kind != structure.Integer {
		return scalar
	}
	low, high, bounded := statedRange(scalar.Constraints)
	if !bounded {
		return scalar
	}
	// An unsigned sixty-four-bit integer has no signed width of its own, so any
	// signed width that holds its stated range is narrower; one whose range
	// needs all sixty-four unsigned bits is left to be refused.
	limit := 65
	if declared, err := widenPrecision(scalar.Precision); err == nil {
		limit = bitsOf(declared)
	}
	candidates := []structure.Precision{
		structure.Int8Bits, structure.Int16Bits, structure.Int32Bits, structure.Int64Bits,
	}
	for _, candidate := range candidates {
		if holds(candidate, low, high) && bitsOf(candidate) < limit {
			scalar.Precision = candidate
			return scalar
		}
	}
	return scalar
}

// statedRange is the least and greatest value an integer's constraints admit,
// and whether they state both.
func statedRange(constraints []structure.Constraint) (low float64, high float64, bounded bool) {
	// The tightest of each, whatever order they were stated in: a width
	// states its own range and a domain a narrower one.
	haveLow, haveHigh := false, false
	lower := func(value float64) {
		if !haveLow || value > low {
			low, haveLow = value, true
		}
	}
	upper := func(value float64) {
		if !haveHigh || value < high {
			high, haveHigh = value, true
		}
	}
	for _, constraint := range constraints {
		switch shape := constraint.(type) {
		case structure.AtLeast:
			lower(shape.Value)
		case structure.Above:
			lower(shape.Value + 1)
		case structure.AtMost:
			upper(shape.Value)
		case structure.Below:
			upper(shape.Value - 1)
		}
	}
	return low, high, haveLow && haveHigh
}

func bitsOf(precision structure.Precision) int {
	switch precision {
	case structure.Int8Bits:
		return 8
	case structure.Int16Bits:
		return 16
	case structure.Int32Bits:
		return 32
	default:
		return 64
	}
}

// holds reports whether a signed width holds every integer from low to high.
//
// The sixty-four-bit width is compared against two to the sixty-third, which a
// float64 holds exactly where it cannot hold the width's largest value: every
// integer below it fits.
func holds(precision structure.Precision, low float64, high float64) bool {
	if precision == structure.Int64Bits {
		return low >= -math.Exp2(63) && high < math.Exp2(63)
	}
	least, most, known := spans(precision)
	return known && low >= least && high <= most
}
