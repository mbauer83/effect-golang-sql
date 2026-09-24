package ddl

// MySQL and MariaDB, which agree about everything here.

import (
	"fmt"
	"github.com/mbauer83/effect-golang-sql/sql"
	"strconv"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// MySQL is the MySQL and MariaDB dialect.
var MySQL Dialect = mysql{}

type mysql struct{}

func (mysql) Name() string { return "mysql" }

func (mysql) Document() string { return "JSON" }

// TableSuffix pins the engine and the character set.
//
// Not decoration: the default engine has changed between versions, and a table
// created without a character set inherits the server's -- so a schema that did
// not say would mean different things on two servers, which is the one thing a
// generated schema must not do. utf8mb4 is the only encoding that holds all of
// Unicode, and utf8mb3 masquerading under the name "utf8" is why it has to be
// said.
// UpsertClause is MySQL's upsert.
//
// The row alias is what MySQL 8.0.19 and later offer in place of values(),
// which is deprecated: an alias for the incoming row reads the same way the
// other two dialects' excluded does, and does not go away. It is written here
// rather than by the caller because it belongs between the values and the
// clause that uses it.
//
// A key that is the whole row still needs a clause, since MySQL has no "do
// nothing", so it is given the assignment that changes least: the first key
// column set to what it already matched on.
func (dialect mysql) UpsertClause(key []string, columns []string) string {
	assignments := assignments(dialect, key, columns, "incoming.")
	if len(assignments) == 0 && len(key) > 0 {
		identifier := dialect.QuoteIdentifier(key[0])
		assignments = []string{identifier + " = incoming." + identifier}
	}
	return "AS incoming ON DUPLICATE KEY UPDATE " + strings.Join(assignments, ", ")
}

func (mysql) TableSuffix() string {
	return " engine=innodb default charset=utf8mb4 collate=utf8mb4_0900_ai_ci"
}

// QuoteIdentifier writes an identifier in backticks, which is MySQL's own spelling.
func (mysql) QuoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func (dialect mysql) Column(scalar structure.Scalar) (string, error) {
	switch scalar.Kind {
	case structure.Text:
		if longest, stated := longest(scalar.Constraints); stated {
			return "VARCHAR(" + strconv.Itoa(longest) + ")", nil
		}
		// longtext, not text: MySQL's text holds 64KB and its varchar needs a
		// length, so a string the description put no bound on has to go
		// somewhere that holds any of them.
		return "LONGTEXT", nil
	case structure.Boolean:
		// MySQL has no boolean. tinyint(1) is what BOOLEAN is a synonym for,
		// and writing it out is honest about what the column holds.
		return "TINYINT(1)", nil
	case structure.Bytes:
		if longest, stated := longest(scalar.Constraints); stated {
			return "VARBINARY(" + strconv.Itoa(longest) + ")", nil
		}
		return "LONGBLOB", nil
	case structure.Timestamp:
		// datetime(6) rather than timestamp: MySQL's timestamp is bounded by
		// the epoch and 2038 and is rewritten into the session's time zone,
		// neither of which an instant should suffer. The microseconds are
		// explicit because the default is none, which would silently round.
		return "DATETIME(6)", nil
	case structure.Number:
		if scalar.Precision == structure.Float32Bits {
			return "FLOAT", nil
		}
		return "DOUBLE", nil
	case structure.Integer:
		return dialect.integer(scalar.Precision)
	default:
		return "", fmt.Errorf("kind %v has no mysql column type", scalar.Kind)
	}
}

// integer keeps the unsigned types, because MySQL has them.
//
// This is the one place the two dialects differ in what they can hold rather
// than in how they spell it, so a uint64 describes a table on MySQL and does
// not on Postgres. Widening here would throw away a range MySQL has.
func (mysql) integer(precision structure.Precision) (string, error) {
	switch precision {
	case structure.Int8Bits:
		return "TINYINT", nil
	case structure.Int16Bits:
		return "SMALLINT", nil
	case structure.Int32Bits:
		return "int", nil
	case structure.Uint8Bits:
		return "TINYINT UNSIGNED", nil
	case structure.Uint16Bits:
		return "SMALLINT UNSIGNED", nil
	case structure.Uint32Bits:
		return "int unsigned", nil
	case structure.Uint64Bits, structure.UintBits:
		return "BIGINT UNSIGNED", nil
	default:
		return "BIGINT", nil
	}
}

// Identity is MySQL's generated key.
func (dialect mysql) Identity(scalar structure.Scalar) (string, error) {
	if scalar.Kind != structure.Integer {
		return "", fmt.Errorf("a generated key is an integer, and this one is %v", scalar.Kind)
	}
	kind, err := dialect.integer(scalar.Precision)
	if err != nil {
		return "", err
	}
	return kind + " NOT NULL AUTO_INCREMENT", nil
}

// Now is spelled with the precision, because a datetime(6) given a default of
// bare current_timestamp is filled to the second and the microseconds are
// silently lost.
func (mysql) Now() string { return "CURRENT_TIMESTAMP(6)" }

// Placeholder is a question mark: MySQL takes the values a statement
// binds in the order they are given, so the ordinal says nothing here and is
// ignored rather than checked.
func (mysql) Placeholder(int) string { return "?" }

// QuoteLiteral is a string literal, with the one character that has to be escaped
// escaped: a quote inside a literal is written twice, which is the standard's
// own rule and the same in all three of these.
func (mysql) QuoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// Key refuses an unbounded string.
//
// Not a stylistic objection. MySQL cannot put a TEXT or BLOB column in a key
// specification without a prefix length, so a table declaring one is rejected
// outright -- and a prefix length invented here would make two different keys
// equal whenever they agreed for that many characters, which is a correctness
// bug rather than a limitation. So the description has to bound the text, and
// MaxLength is how it says so.
func (dialect mysql) Key(scalar structure.Scalar) (string, error) {
	if scalar.Kind != structure.Text && scalar.Kind != structure.Bytes {
		return dialect.Column(scalar)
	}
	if _, bounded := longest(scalar.Constraints); !bounded {
		return "", errUnboundedKey
	}
	return dialect.Column(scalar)
}

// Retype: MySQL's modify restates the whole definition, so anything left out of the
// restatement is lost -- which is why the form has to be known rather than
// assumed.
func (mysql) Retype() (RetypeForm, error) { return RetypeWhole, nil }

// ValidateDefault refuses a default on an unbounded string or blob.
//
// MySQL rejects the statement: a TEXT or BLOB column takes no default, and
// there is no expression form that changes that. The remedy is the same as for
// a key -- bound the text with MaxLength, which makes it a varchar, and a
// varchar takes a default.
func (mysql) ValidateDefault(scalar structure.Scalar) error {
	if scalar.Kind != structure.Text && scalar.Kind != structure.Bytes {
		return nil
	}
	if _, bounded := longest(scalar.Constraints); !bounded {
		return errUnboundedDefault
	}
	return nil
}

// IndexBelongsToTable: an index belongs to a table here.
func (mysql) IndexBelongsToTable() bool { return true }

// Syntax is what MySQL can do to a value, where it does not do it the ordinary
// way.
//
// Six answers, and two of them are the reason this is asked rather than
// assumed. MySQL's length counts *bytes*, so counting characters is another
// function -- a store that had written length would have been right on two
// servers and quietly wrong on the third for every string that was not ASCII.
// And its difference between moments takes the earlier moment first, so the
// answer is the ordinary phrase with its arguments read the other way round:
// stated once here rather than by every caller who has to remember which
// server it is talking to.
func (mysql) Syntax(operation sql.Operation) (sql.Syntax, bool) {
	switch operation {
	case sql.Concatenation:
		return sql.Function("CONCAT"), true
	case sql.SubstringOf:
		return sql.Function("SUBSTRING"), true
	case sql.CharacterCount:
		return sql.Function("CHAR_LENGTH"), true
	case sql.StringAggregation:
		return sql.DetailPhrase("GROUP_CONCAT(", " SEPARATOR %s)"), true
	case sql.SecondsBetween:
		return sql.Flip(sql.Phrase("TIMESTAMPDIFF(SECOND, ", ", ", ")")), true
	case sql.ExpressionMatch:
		return sql.Infix(" REGEXP "), true
	case sql.WholeTotal:
		// Every sum is a decimal here, whatever it was over, and the driver
		// hands a decimal back as bytes.
		return sql.Phrase("CAST(SUM(", ") AS SIGNED)"), true
	case sql.Average:
		return sql.Phrase("CAST(AVG(", ") AS DOUBLE)"), true
	default:
		return nil, false
	}
}

// LengthOf counts characters, as a check means.
func (mysql) LengthOf(column string) string { return "CHAR_LENGTH(" + column + ")" }

// Matching is a regular expression match.
func (mysql) Matching(column string, pattern string) (string, bool) {
	return column + " REGEXP " + mysql{}.QuoteLiteral(pattern), true
}
