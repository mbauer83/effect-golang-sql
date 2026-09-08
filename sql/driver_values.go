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
	held dynamic.Value
}

// Scan is database/sql's contract, which is untyped because a driver's answer
// is not known until it arrives.
//
// The seven cases below are what a driver may produce, by the driver.Value
// contract: everything else is a driver going beyond it, and saying so beats
// guessing.
func (received *column) Scan(scanned any) error {
	switch value := scanned.(type) {
	case nil:
		received.held = dynamic.Absent{}
	case bool:
		received.held = dynamic.Boolean{Value: value}
	case int64:
		received.held = dynamic.Integer{Value: value}
	case float64:
		received.held = dynamic.Number{Value: value}
	case string:
		received.held = dynamic.Text{Value: value}
	case []byte:
		// A driver may hand text back as bytes, and which it does is the
		// driver's business rather than the schema's. Bytes it is: a schema
		// asking for text reads it, because a text source is what a byte string
		// from a database column is.
		received.held = dynamic.Bytes{Value: value}
	case time.Time:
		received.held = dynamic.Timestamp{Value: value}
	default:
		return fmt.Errorf("a driver produced %T, which is not a value a column may hold", scanned)
	}
	return nil
}

// bindings turns arguments into what a driver takes.
//
// It is the same boundary in the other direction: a statement's parameters are
// values of whatever kind the columns are, and the driver's contract is
// untyped.
func bindings(arguments []dynamic.Value) ([]any, error) {
	bound := make([]any, 0, len(arguments))
	for index, argument := range arguments {
		value, err := driverValue(argument)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", index+1, err)
		}
		bound = append(bound, value)
	}
	return bound, nil
}

func driverValue(argument dynamic.Value) (any, error) {
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
		return value.Value, nil
	default:
		return nil, fmt.Errorf("%T is not a value a statement parameter may hold", argument)
	}
}

// destinations are what Scan is given: one column each, in the order the result
// set declares them.
func destinations(width int) ([]any, []column) {
	received := make([]column, width)
	into := make([]any, width)
	for index := range received {
		into[index] = &received[index]
	}
	return into, received
}
