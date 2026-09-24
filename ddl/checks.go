package ddl

// The rules a column's values keep, enforced by the database.
//
// A constraint the description states is a rule somebody asked for, so the
// table enforces it too: the schema keeps it on what this program writes, and
// a check keeps it on whatever else writes -- a migration, a console, another
// program. What the dialect cannot express stays a comment, which is honest
// about not being enforced.

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Check is one rule a column's values keep.
type Check struct {
	// Rule names what is checked -- "min_length", "at_most" -- and, with the
	// table and column, the constraint's name.
	Rule string
	// Expression is the condition, in the dialect's spelling.
	Expression string
}

// checksFor are the checks a column's constraints become, and the constraints
// the dialect cannot check, which stay notes.
func checksFor(dialect Dialect, column string, node structure.Node, columnType string) ([]Check, []structure.Constraint) {
	scalar, isScalar := node.(structure.Scalar)
	if !isScalar {
		if nullable, wrapped := node.(structure.Nullable); wrapped {
			return checksFor(dialect, column, nullable.Inner, columnType)
		}
		return nil, nil
	}
	// Checked against the width the column has, so a bound the chosen type
	// already keeps is not checked again.
	scalar = narrowed(scalar)
	quoted := dialect.QuoteIdentifier(column)
	var checks []Check
	var unchecked []structure.Constraint
	for _, constraint := range scalar.Constraints {
		if isImplied(scalar.Precision, constraint) {
			continue
		}
		switch shape := constraint.(type) {
		case structure.AtLeast:
			checks = append(checks, Check{"at_least", quoted + " >= " + bound(shape.Value)})
		case structure.AtMost:
			checks = append(checks, Check{"at_most", quoted + " <= " + bound(shape.Value)})
		case structure.Above:
			checks = append(checks, Check{"above", quoted + " > " + bound(shape.Value)})
		case structure.Below:
			checks = append(checks, Check{"below", quoted + " < " + bound(shape.Value)})
		case structure.MinLength:
			checks = append(checks, Check{"min_length",
				dialect.LengthOf(quoted) + " >= " + strconv.Itoa(shape.Value)})
		case structure.MaxLength:
			// A bounded varchar keeps its own limit.
			if strings.HasPrefix(columnType, "VARCHAR(") {
				continue
			}
			checks = append(checks, Check{"max_length",
				dialect.LengthOf(quoted) + " <= " + strconv.Itoa(shape.Value)})
		case structure.Pattern:
			if expression, can := dialect.Matching(quoted, shape.Expression); can {
				checks = append(checks, Check{"pattern", expression})
			} else {
				unchecked = append(unchecked, constraint)
			}
		default:
			unchecked = append(unchecked, constraint)
		}
	}
	return checks, unchecked
}

// definition is the check as it is written inside a column's definition,
// named so that a refusal says which rule a row broke.
func (check Check) definition(dialect Dialect, table string, column string) string {
	return "CONSTRAINT " + dialect.QuoteIdentifier(checkName(table, column, check.Rule)) +
		" CHECK (" + check.Expression + ")"
}

// checkName is table_column_rule, shortened with a hash of the whole when it
// is longer than Postgres takes an identifier -- sixty-three bytes, one fewer
// than MySQL -- so the name stays unique and stays within both.
func checkName(table string, column string, rule string) string {
	name := table + "_" + column + "_" + rule
	const longest = 63
	if len(name) <= longest {
		return name
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	return name[:longest-9] + "_" + fmt.Sprintf("%08x", hash.Sum32())
}

// bound is a number as a check writes it: in full, never in exponent form.
func bound(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
