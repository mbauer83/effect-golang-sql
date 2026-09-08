package ddl

// Postgres.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Postgres is the PostgreSQL dialect.
var Postgres Dialect = postgres{}

type postgres struct{}

func (postgres) Name() string { return "postgres" }

func (postgres) Document() string { return "jsonb" }

// TableSuffix is empty: Postgres needs nothing after the parenthesis.
func (postgres) TableSuffix() string { return "" }

// Quoted writes an identifier in double quotes, which is the standard's own
// spelling and what keeps a column called "order" from being a syntax error.
func (postgres) Quoted(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (dialect postgres) Column(scalar structure.Scalar) (string, error) {
	switch scalar.Kind {
	case structure.Text:
		// text, not varchar: Postgres stores them identically and text has no
		// length to get wrong. A stated maximum becomes a length anyway,
		// because it is a claim the database can then keep.
		if longest, stated := longest(scalar.Constraints); stated {
			return "varchar(" + strconv.Itoa(longest) + ")", nil
		}
		return "text", nil
	case structure.Boolean:
		return "boolean", nil
	case structure.Bytes:
		return "bytea", nil
	case structure.Timestamp:
		// With the time zone. A timestamp without one is a time nobody can
		// place, and every instant this module carries is placed.
		return "timestamptz", nil
	case structure.Number:
		if scalar.Precision == structure.Float32Bits {
			return "real", nil
		}
		return "double precision", nil
	case structure.Integer:
		return dialect.integer(scalar.Precision)
	default:
		return "", fmt.Errorf("kind %v has no postgres column type", scalar.Kind)
	}
}

func (postgres) integer(precision structure.Precision) (string, error) {
	widened, err := widened(precision)
	if err != nil {
		return "", err
	}
	switch widened {
	case structure.Int8Bits, structure.Int16Bits:
		// No tinyint in Postgres, so the smallest is two bytes.
		return "smallint", nil
	case structure.Int32Bits:
		return "integer", nil
	default:
		return "bigint", nil
	}
}

// Identity is Postgres's own generated key.
//
// GENERATED ALWAYS AS IDENTITY rather than serial: serial is the older spelling
// and leaves a sequence whose ownership has to be managed by hand, which the
// standard form does not.
func (postgres) Identity(scalar structure.Scalar) (string, error) {
	switch scalar.Precision {
	case structure.Int8Bits, structure.Int16Bits:
		return "smallint generated always as identity", nil
	case structure.Int32Bits:
		return "integer generated always as identity", nil
	case structure.Int64Bits, structure.IntBits, structure.Unstated:
		return "bigint generated always as identity", nil
	default:
		return "", fmt.Errorf(
			"a generated key is a signed integer, and this one is %v", scalar.Precision)
	}
}

// Now is the standard's own spelling, and Postgres records it with the time
// zone -- which is what timestamptz columns want.
func (postgres) Now() string { return "current_timestamp" }

// Text is a string literal, with the one character that has to be escaped
// escaped: a quote inside a literal is written twice, which is the standard's
// own rule and the same in all three of these.
func (postgres) Text(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// Key is an ordinary column: this dialect indexes any of these types.
func (dialect postgres) Key(scalar structure.Scalar) (string, error) {
	return dialect.Column(scalar)
}

// Retype: Postgres changes the type and leaves the rest of the definition alone.
func (postgres) Retype() (RetypeForm, error) { return RetypeTypeOnly, nil }

// MayDefault accepts any of them: this dialect puts a default on any column.
func (postgres) MayDefault(structure.Scalar) error { return nil }

// IndexBelongsToTable: an index belongs to the schema here.
func (postgres) IndexBelongsToTable() bool { return false }
