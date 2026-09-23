package sql

// One of the two places this module holds a value it cannot name.
//
// database/sql scans into any and a driver hands back any, because a driver
// cannot know what a column holds until it reads it. That is a genuine boundary
// rather than a shortcut, and it is confined to this file: everything above it
// works in the universal representation, which has a case for each of the seven
// kinds a driver may produce.
//
// The architecture test names this file and the other one, so each exemption is
// a decision on the record rather than a hole someone widened.

import (
	"fmt"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// column receives one scanned value and keeps it as the case it turned out to
// be.
type column struct {
	value dynamic.Value
}

// Scan is database/sql's contract, which is untyped because a driver's answer
// is not known until it arrives.
//
// The seven cases below are what a driver may produce, by the driver.Value
// contract: everything else is a driver going beyond it, and saying so beats
// guessing.
func (destination *column) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		destination.value = dynamic.Absent{}
	case bool:
		destination.value = dynamic.Boolean{Value: value}
	case int64:
		destination.value = dynamic.Integer{Value: value}
	case float64:
		destination.value = dynamic.Number{Value: value}
	case string:
		destination.value = dynamic.Text{Value: value}
	case []byte:
		// A driver may hand text back as bytes, and which it does is the
		// driver's business rather than the schema's. Bytes it is: a schema
		// asking for text reads it, because a text source is what a byte string
		// from a database column is.
		destination.value = dynamic.Bytes{Value: value}
	case time.Time:
		destination.value = dynamic.Timestamp{Value: value}
	default:
		return fmt.Errorf("a driver produced %T, which is not a value a column may hold", src)
	}
	return nil
}

// Instants is how a driver is given a moment.
//
// Two ways, because the drivers behind this port do not agree about what a
// timestamp column holds. Postgres and MySQL have a type for an instant and
// take a time.Time; SQLite has none, so this module's own projection makes it a
// text column -- and a driver handed a time.Time for a text column formats it
// however it likes. modernc's writes Go's default String, which is neither the
// RFC 3339 this module declares nor a format that sorts chronologically, and
// which nothing reads back as an instant. So for such a driver the module
// formats the value itself rather than leaving the spelling to whoever is
// underneath.
type Instants int

const (
	// AsNative hands the driver a time.Time, which is right wherever the
	// column has a type for one.
	AsNative Instants = iota
	// AsRFC3339 writes the text this module's SQLite projection declares, so
	// what a binding writes and what a column default writes are the same
	// spelling and both sort chronologically.
	AsRFC3339
)

// instantsFor is how a driver is given a moment, by the name it registered
// under.
//
// A decision made once, at Open, because the driver name is the only place
// this is knowable and a caller should not have to know it at all.
func instantsFor(driver string) Instants {
	switch driver {
	case "sqlite", "sqlite3":
		return AsRFC3339
	default:
		return AsNative
	}
}

// bindings turns arguments into what a driver takes.
//
// It is the same boundary in the other direction: a statement's parameters are
// values of whatever kind the columns are, and the driver's contract is
// untyped.
func bindings(arguments []dynamic.Value, instants Instants) ([]any, error) {
	values := make([]any, 0, len(arguments))
	for index, argument := range arguments {
		value, err := driverValue(argument, instants)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", index+1, err)
		}
		values = append(values, value)
	}
	return values, nil
}

func driverValue(argument dynamic.Value, instants Instants) (any, error) {
	switch value := argument.(type) {
	case dynamic.Absent:
		return nil, nil
	case dynamic.Boolean:
		return value.Value, nil
	case dynamic.Integer:
		return value.Value, nil
	case dynamic.Number:
		return value.Value, nil
	case dynamic.Text:
		return value.Value, nil
	case dynamic.Bytes:
		return value.Value, nil
	case dynamic.Timestamp:
		if instants == AsRFC3339 {
			return value.Value.UTC().Format(time.RFC3339Nano), nil
		}
		return value.Value, nil
	default:
		return nil, fmt.Errorf("%T is not a value a statement parameter may hold", argument)
	}
}

// destinations are what Scan is given: one column each, in the order the result
// set declares them.
func destinations(width int) ([]any, []column) {
	values := make([]column, width)
	into := make([]any, width)
	for index := range values {
		into[index] = &values[index]
	}
	return into, values
}
