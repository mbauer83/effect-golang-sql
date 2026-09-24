package ddl

// SQLite.
//
// A third dialect, and a deliberate one. Postgres and MySQL are what was asked
// for and neither runs in every place these tests run, so without this the
// derivation -- which tables there are, what the keys are, which column a child
// carries, what order the statements go in -- could only be checked by
// comparing strings to strings. SQLite is already a dependency here, so the
// statements can be *executed* and the schema then asked what it holds.
//
// It is a real dialect and not a stub: what it spells differently, it spells
// differently for reasons.

import (
	"fmt"
	"github.com/mbauer83/effect-golang-sql/sql"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// SQLite is the SQLite dialect.
var SQLite Dialect = sqlite{}

type sqlite struct{}

func (sqlite) Name() string { return "sqlite" }

// Document is text: SQLite's JSON functions work on text and there is no
// separate type to put it in.
func (sqlite) Document() string { return "TEXT" }

func (sqlite) TableSuffix() string { return "" }

// UpsertClause is SQLite's upsert, which it took from Postgres and spells the
// same way. Not "INSERT OR REPLACE", which deletes the old row and so drops
// whatever a column not mentioned was holding and fires delete triggers for a
// row nobody deleted.
func (dialect sqlite) UpsertClause(key []string, columns []string) string {
	return onConflictClause(dialect, key, columns, "EXCLUDED")
}

func (sqlite) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Column resolves to SQLite's storage classes.
//
// SQLite has five and applies them as affinities rather than as constraints, so
// a length or an unsigned range would be documentation and not enforcement.
// Writing the affinity honestly is better than writing varchar(64) and implying
// a limit nothing keeps.
func (sqlite) Column(scalar structure.Scalar) (string, error) {
	switch scalar.Kind {
	case structure.Text:
		return "TEXT", nil
	case structure.Boolean:
		// No boolean: SQLite stores 0 and 1 in an integer column.
		return "INTEGER", nil
	case structure.Bytes:
		return "BLOB", nil
	case structure.Timestamp:
		// Text, in RFC 3339. SQLite has no date type, and text sorts
		// chronologically in that format where a number would need a unit
		// nobody wrote down.
		return "TEXT", nil
	case structure.Number:
		return "REAL", nil
	case structure.Integer:
		return "INTEGER", nil
	default:
		return "", fmt.Errorf("kind %v has no sqlite column type", scalar.Kind)
	}
}

// Identity is a plain integer.
//
// That is not a shortcut. A column declared INTEGER that is the sole primary
// key is an alias for SQLite's own rowid, so it is assigned when a row is
// inserted without a value -- which is exactly what a generated key is. Adding
// AUTOINCREMENT would only stop reuse of a deleted key and would have to be
// written inline, where every other dialect here declares the key separately.
func (dialect sqlite) Identity(scalar structure.Scalar) (string, error) {
	if scalar.Kind != structure.Integer {
		return "", fmt.Errorf("a generated key is an integer, and this one is %v", scalar.Kind)
	}
	return "INTEGER", nil
}

// Now is SQLite's own function, in the format its text timestamps use.
//
// Parenthesised, because SQLite only takes an expression as a default inside
// parentheses -- current_timestamp on its own is a keyword it accepts but which
// writes a format without the T, and reading that back as an instant would fail.
func (sqlite) Now() string { return "(STRFTIME('%Y-%m-%dT%H:%M:%SZ'))" }

// Placeholder is a question mark: SQLite takes the values a statement
// binds in the order they are given, so the ordinal says nothing here and is
// ignored rather than checked.
func (sqlite) Placeholder(int) string { return "?" }

// QuoteLiteral is a string literal, with the one character that has to be escaped
// escaped: a quote inside a literal is written twice, which is the standard's
// own rule and the same in all three of these.
func (sqlite) QuoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// Key is an ordinary column: this dialect indexes any of these types.
func (dialect sqlite) Key(scalar structure.Scalar) (string, error) {
	return dialect.Column(scalar)
}

// Retype refuses.
//
// SQLite's ALTER TABLE can rename a column, add one and drop one, and cannot
// change one's type at all: the way to do it is a new table, a copy, a drop and
// a rename. That is four statements and a decision about what to do with the
// values, so it is the caller's to write rather than something to emit as if it
// were one change.
func (sqlite) Retype() (RetypeForm, error) { return 0, errNoRetype }

// ValidateDefault accepts any of them: this dialect puts a default on any column.
func (sqlite) ValidateDefault(structure.Scalar) error { return nil }

// IndexBelongsToTable: an index belongs to the schema here.
func (sqlite) IndexBelongsToTable() bool { return false }

// Syntax is what SQLite can do to a value, where it does not do it the
// ordinary way.
//
// Four answers, and one deliberate silence: SQLite has no regular expression
// unless the program that opened the database registered one, so it says
// nothing about ExpressionMatch and a query that asks for one is refused with
// the dialect named. That is the point of the operation being asked rather
// than assumed -- a program that did register one says so with Also.
//
// It also has no difference between moments: there is no interval type at all,
// so a difference is two Julian days subtracted and scaled, which is a number
// of seconds like the other two answer with.
func (sqlite) Syntax(operation sql.Operation) (sql.Syntax, bool) {
	switch operation {
	case sql.Concatenation:
		return sql.Operator(" || "), true
	case sql.SubstringOf:
		return sql.Function("SUBSTR"), true
	case sql.StringAggregation:
		return sql.DetailPhrase("GROUP_CONCAT(", ", %s)"), true
	case sql.SecondsBetween:
		return sql.Phrase("((JULIANDAY(", ") - JULIANDAY(", ")) * 86400)"), true
	case sql.WholeTotal:
		// SQLite answers a whole sum with a whole number already. The cast is
		// written anyway, because a sum that overflowed would otherwise come
		// back as a float and decode as nothing.
		return sql.Phrase("CAST(SUM(", ") AS INTEGER)"), true
	case sql.Average:
		return sql.Phrase("CAST(AVG(", ") AS REAL)"), true
	default:
		return nil, false
	}
}

// LengthOf counts characters, as a check means.
func (sqlite) LengthOf(column string) string { return "LENGTH(" + column + ")" }

// Matching is not available: SQLite's REGEXP works only when the application
// registers a function for it, so a pattern stays a comment.
func (sqlite) Matching(column string, pattern string) (string, bool) {
	return "", false
}
