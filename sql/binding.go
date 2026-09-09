package sql

// A Go value as something a statement binds.
//
// Read from the value's type rather than from a switch on the value, so a
// domain's own identity -- a FilmID that is an int64, a UserID that is a
// string -- binds as the whole number or the text it is. That is what makes
// the query's types the domain's: the description says the column holds a
// whole number, the domain says which ones, and neither has to know about the
// other.
//
// The mirror of driver_values.go, which crosses from what a driver produced to
// the same representation. Read through the value's own type rather than by
// asserting on it, so nothing here holds a value it cannot name: an instant is
// the one concrete type this has to recognise, because it is a struct and
// every other struct is refused.

import (
	"fmt"
	"reflect"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

func bindable[A any](a A) (dynamic.Value, error) {
	read := reflect.ValueOf(a)
	if read.IsValid() && read.Type() == momentary {
		// The one concrete type this has to recognise, because an instant is
		// a struct and every other struct is refused: crossing back to it is
		// the top type this file exists for.
		moment, isMoment := read.Interface().(time.Time)
		if !isMoment {
			return nil, fmt.Errorf("sql: %T is not the instant it claims to be", a)
		}
		return dynamic.OfTimestamp(moment), nil
	}
	if !read.IsValid() {
		return dynamic.Absent{}, nil
	}
	switch read.Kind() {
	case reflect.String:
		return dynamic.OfText(read.String()), nil
	case reflect.Bool:
		return dynamic.OfBoolean(read.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return dynamic.OfInteger(read.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return dynamic.OfInteger(int64(read.Uint())), nil
	case reflect.Float32, reflect.Float64:
		return dynamic.OfNumber(read.Float()), nil
	case reflect.Slice:
		if read.Type().Elem().Kind() == reflect.Uint8 {
			return dynamic.OfBytes(read.Bytes()), nil
		}
	}
	return nil, fmt.Errorf("sql: %T is not a value a statement can bind", a)
}
