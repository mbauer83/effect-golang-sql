package sql

// What a column holds, as far as a query has to know.
//
// Seven answers, and the reason there are only seven is that this is what a
// comparison needs: whether two things can be compared at all, and whether an
// operation that wants text has been given text. The dialect's own spelling of
// the type is a different question and is answered in the projection.
//
// A term's Go type is the query's type, and this is how the two are reconciled
// -- so a column described as text and read as Expr[int64] is a refusal where
// the query names it, rather than a comparison a server evaluates by coercing
// one side.

import (
	"reflect"
	"time"
)

// Kind is what a column holds.
//
// The zero value is unknown, which is what a source built without a
// description says about every column: unknown checks nothing, which is honest
// about having been told nothing.
type Kind int

const (
	OfUnknown Kind = iota
	OfText
	OfWhole
	OfNumber
	OfTruth
	OfBytes
	OfMoment
	OfDocument
)

// String is what to call a kind in a refusal.
func (kind Kind) String() string {
	switch kind {
	case OfText:
		return "text"
	case OfWhole:
		return "a whole number"
	case OfNumber:
		return "a number"
	case OfTruth:
		return "a truth value"
	case OfBytes:
		return "bytes"
	case OfMoment:
		return "a moment"
	case OfDocument:
		return "a document"
	default:
		return "something this query was not told about"
	}
}

// Admits reports whether a column of this kind may be read as a term of that
// one.
//
// Unknown on either side admits everything, because a check needs both to have
// been said. Otherwise they have to agree: a whole number and a number are not
// interchangeable here, since a column described as one and read as the other
// is a description somebody has since changed.
func (kind Kind) Admits(other Kind) bool {
	return kind == OfUnknown || other == OfUnknown || kind == other
}

var timeType = reflect.TypeFor[time.Time]()

// kindOf is what a Go type is, as a column kind.
//
// Read from the type rather than from a type switch on a value, so that a
// domain's own identity -- a FilmID that is an int64 -- is a whole number
// here. That is the point of the query's types being the domain's: the
// description says the column holds a whole number and the domain says which
// whole numbers, and neither has to know about the other.
func kindOf[A any]() Kind {
	goType := reflect.TypeFor[A]()
	if goType == nil {
		return OfUnknown
	}
	if goType == timeType {
		return OfMoment
	}
	switch goType.Kind() {
	case reflect.String:
		return OfText
	case reflect.Bool:
		return OfTruth
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return OfWhole
	case reflect.Float32, reflect.Float64:
		return OfNumber
	case reflect.Slice:
		if goType.Elem().Kind() == reflect.Uint8 {
			return OfBytes
		}
		return OfUnknown
	default:
		return OfUnknown
	}
}
