package ddl

// What a dialect has to answer, and what it may refuse.

import (
	"errors"
	"fmt"
	"github.com/mbauer83/effect-golang-sql/sql"
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
	// Placeholder is how this dialect spells the nth value a statement binds,
	// counted from one.
	//
	// Here rather than fixed at one spelling and rewritten later, because
	// rewriting means scanning composed SQL for a character that also occurs
	// inside string literals, quoted identifiers and comments -- so the
	// rewriter has to understand the statement, and it would be understanding
	// it in order to undo a choice this interface exists to make. An adapter
	// composing a statement holds the dialect already; asking it how to spell
	// a placeholder is the same act as asking how to spell an identifier.
	//
	// The ordinal is passed even to dialects that ignore it, because a
	// caller cannot know which ones do: Postgres numbers its parameters and
	// SQLite and MySQL do not, and a caller that had to know would be a
	// caller that stopped being portable the first time it was moved.
	Placeholder(ordinal int) string
	// Replacing is the clause that follows an insert's values and says "and if
	// a row with this key is already there, make it this one".
	//
	// A dialect's business because it is the one statement the three spell
	// three ways -- two of them with a conflict target and an excluded row,
	// the third with a duplicate-key clause and an alias -- and because the
	// alternative is a read, a branch and a write, which is two round trips
	// and a race between them.
	//
	// The key is what a conflict is judged on and the columns are the whole
	// row; a dialect writes assignments for the columns that are not the key,
	// since assigning the key the value it was matched on says nothing.
	Replacing(key []string, columns []string) string
	// Writes is how this dialect performs one operation on a value, and
	// whether it can at all.
	//
	// The extensible seam, and it is a method rather than a method per
	// operation because the set of operations a server offers is that
	// server's: two of the three concatenate with an operator and the third
	// with a function, one counts characters under another name, one has no
	// regular expression. A dialect answers about what it has and says nothing
	// about what it has not, and a query that asked for the latter is refused
	// with the dialect named -- rather than composed and sent.
	Writes(operation sql.Operation) (sql.Written, bool)
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

// Every dialect is a Spelling, which is what lets a store hold the dialect it
// was built with and never name one: the query says what it asks and the
// dialect says how its server writes it.
var (
	_ sql.Spelling = Postgres
	_ sql.Spelling = MySQL
	_ sql.Spelling = SQLite
)

// literal is a value written as this dialect's own literal.
//
// Only what a column can hold: a document default would be a document written
// twice, once in the description and once as a string nobody validated, so it
// is refused.
func literal(dialect Dialect, value dynamic.Value) (string, error) {
	switch shape := value.(type) {
	case dynamic.Text:
		return dialect.Text(shape.Value), nil
	case dynamic.Integer:
		return strconv.FormatInt(shape.Value, 10), nil
	case dynamic.Number:
		return strconv.FormatFloat(shape.Value, 'g', -1, 64), nil
	case dynamic.Boolean:
		if shape.Value {
			return dialect.Text("true"), nil
		}
		return dialect.Text("false"), nil
	case dynamic.Absent:
		return "null", nil
	default:
		return "", fmt.Errorf("%T is not a value a default can be written as", value)
	}
}

// resolveColumn is the type a node becomes, and whether the column admits null.
//
// A nullable node is a nullable column; everything else is decided by the
// derivation rather than here, because whether an *optional field* becomes a
// nullable column is a question about the aggregate and not about the type.
func resolveColumn(dialect Dialect, node structure.Node) (kind string, nullable bool, err error) {
	switch shape := node.(type) {
	case structure.Nullable:
		inner, _, err := resolveColumn(dialect, shape.Inner)
		return inner, true, err
	case structure.Scalar:
		kind, err := dialect.Column(shape)
		return kind, false, err
	case structure.Object, structure.Union, structure.Sequence, structure.Mapping:
		// A value object, a list of values, a map or a union in one column.
		// An *entity* never reaches here: the derivation gives it a table.
		return dialect.Document(), false, nil
	case structure.Reference:
		if shape.Resolve == nil {
			return "", false, fmt.Errorf("%q: %w", shape.Name, errUnresolved)
		}
		return resolveColumn(dialect, shape.Resolve())
	default:
		return "", false, fmt.Errorf("%T has no column form", node)
	}
}

// widenPrecision is the signed type an unsigned one fits in losslessly, for a dialect
// that has no unsigned integers.
//
// Widening is not approximating: every value of a uint32 is a value of a
// bigint, so nothing is lost and the column is still an integer. The one that
// does not fit is uint64, and that is refused rather than turned into a decimal
// -- a decimal holds the values and is not an integer, so a key that was fast
// would quietly stop being one.
func widenPrecision(precision structure.Precision) (structure.Precision, error) {
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
		if maxLength, isMax := constraint.(structure.MaxLength); isMax {
			return maxLength.Value, true
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
