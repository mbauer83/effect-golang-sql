package ddl

// What a dialect has to answer, and what it may refuse.

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Dialect is one database's spelling.
//
// Narrow on purpose: a dialect resolves a scalar to its own type, names the
// generated-identity form, quotes an identifier, and says whether it can put a
// document in a column. Everything else about a table -- which tables there
// are, what the keys are, which columns a child carries -- is the description's
// and is the same everywhere.
type Dialect interface {
	// Name is the dialect's own name, for a report that has to say which one
	// refused.
	Name() string
	// Column is the type a scalar becomes, or the refusal that it cannot be
	// expressed.
	Column(scalar structure.Scalar) (string, error)
	// Identity is the type a generated key becomes: a serial, or an integer
	// with an auto-increment.
	Identity(scalar structure.Scalar) (string, error)
	// Key is the type an application-supplied key becomes, which is not always
	// the type the same value would have in an ordinary column: MySQL cannot
	// index an unbounded text column at all, so a dialect gets to refuse a key
	// it could not build an index on.
	Key(scalar structure.Scalar) (string, error)
	// Document is the type a value object, list, map or union becomes when it
	// is stored as one value.
	Document() string
	// Quoted is an identifier as this dialect writes it.
	Quoted(name string) string
	// TableSuffix is whatever has to follow the closing parenthesis: MySQL's
	// engine and charset, and nothing at all for Postgres.
	TableSuffix() string
	// Now is how this dialect writes the moment a row is written.
	Now() string
	// Retype says how this dialect spells a change of a column's type, or
	// refuses because it cannot do it in place.
	Retype() (RetypeForm, error)
	// MayDefault refuses a default this dialect will not accept on a column of
	// that shape. MySQL takes none on an unbounded text or blob column, which
	// is a statement it rejects outright rather than a preference.
	MayDefault(scalar structure.Scalar) error
	// IndexBelongsToTable says whether dropping an index has to name the table
	// it is on. It does in MySQL, where an index belongs to a table, and does
	// not in Postgres, where it belongs to the schema.
	IndexBelongsToTable() bool
	// Text is a string literal in this dialect's own quoting, for a default.
	Text(value string) string
}

// literal is a value written as this dialect's own literal.
//
// Only what a column can hold: a document default would be a document written
// twice, once in the description and once as a string nobody validated, so it
// is refused.
func literal(dialect Dialect, value dynamic.Value) (string, error) {
	switch held := value.(type) {
	case dynamic.Text:
		return dialect.Text(held.Value), nil
	case dynamic.Integer:
		return strconv.FormatInt(held.Value, 10), nil
	case dynamic.Number:
		return strconv.FormatFloat(held.Value, 'g', -1, 64), nil
	case dynamic.Boolean:
		if held.Value {
			return dialect.Text("true"), nil
		}
		return dialect.Text("false"), nil
	case dynamic.Absent:
		return "null", nil
	default:
		return "", fmt.Errorf("%T is not a value a default can be written as", value)
	}
}

// resolved is the type a node becomes, and whether the column admits null.
//
// A nullable node is a nullable column; everything else is decided by the
// derivation rather than here, because whether an *optional field* becomes a
// nullable column is a question about the aggregate and not about the type.
func resolved(dialect Dialect, node structure.Node) (kind string, nullable bool, err error) {
	switch held := node.(type) {
	case structure.Nullable:
		inner, _, err := resolved(dialect, held.Inner)
		return inner, true, err
	case structure.Scalar:
		kind, err := dialect.Column(held)
		return kind, false, err
	case structure.Object, structure.Union, structure.Sequence, structure.Mapping:
		// A value object, a list of values, a map or a union in one column.
		// An *entity* never reaches here: the derivation gives it a table.
		return dialect.Document(), false, nil
	case structure.Reference:
		if held.Resolve == nil {
			return "", false, fmt.Errorf("%q: %w", held.Name, errUnresolved)
		}
		return resolved(dialect, held.Resolve())
	default:
		return "", false, fmt.Errorf("%T has no column form", node)
	}
}

// widened is the signed type an unsigned one fits in losslessly, for a dialect
// that has no unsigned integers.
//
// Widening is not approximating: every value of a uint32 is a value of a
// bigint, so nothing is lost and the column is still an integer. The one that
// does not fit is uint64, and that is refused rather than turned into a decimal
// -- a decimal holds the values and is not an integer, so a key that was fast
// would quietly stop being one.
func widened(precision structure.Precision) (structure.Precision, error) {
	switch precision {
	case structure.Uint8Bits:
		return structure.Int16Bits, nil
	case structure.Uint16Bits:
		return structure.Int32Bits, nil
	case structure.Uint32Bits:
		return structure.Int64Bits, nil
	case structure.Uint64Bits, structure.UintBits:
		return 0, errNoUnsigned
	default:
		return precision, nil
	}
}

// longest is the length a text constraint states, and whether it states one.
//
// A dialect with a bounded string type wants it: a varchar needs a length, and
// one invented would be a limit the description never claimed.
func longest(constraints []structure.Constraint) (int, bool) {
	for _, constraint := range constraints {
		if held, isMax := constraint.(structure.MaxLength); isMax {
			return held.Value, true
		}
	}
	return 0, false
}

var (
	errNoUnsigned = errors.New(
		"this dialect has no unsigned 64-bit integer, and a decimal that held the values " +
			"would not be an integer: describe it as a signed 64-bit integer, or as text " +
			"if the range is really needed")
	errUnresolved      = errors.New("a reference has nothing behind it to make a column from")
	errAcrossTheDivide = errors.New(
		"a relation becoming a column, or a column becoming a relation, is a table " +
			"appearing or going as well as a column changing: say it as a removal and " +
			"an addition, because guessing an order for the two would be guessing which " +
			"of them was meant")
	errAnotherEntity = errors.New(
		"a relation to a different entity is a different table: say it as a removal and " +
			"an addition")
	errUnboundedDefault = errors.New(
		"this dialect takes no default on an unbounded text or blob column and would " +
			"reject the statement: give the field a maximum length, which makes it a " +
			"varchar, and a varchar takes one")
	errUnboundedKey = errors.New(
		"this dialect cannot key an unbounded string, and a prefix length invented here " +
			"would make two different keys equal whenever they agreed for that many " +
			"characters: give the identity a maximum length")
)
