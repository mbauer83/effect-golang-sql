package migrate

// The record of what has been applied.
//
// One row per aggregate, holding the version its tables are at. Not one row
// per step: a step is derived from the history and the history is code, so what
// a database has to remember is where it got to and not what the steps were.
// That is also what keeps a history editable -- inserting a version between two
// released ones is a code change and not a rewriting of somebody's ledger.

import (
	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/ddl"
)

// Recorded is one aggregate's row in the ledger.
type Recorded struct {
	// Aggregate is the fully-qualified name the history is of.
	Aggregate string
	// Version is the name of the version its tables are at.
	Version string
}

// RecordedSchema describes a row of the ledger, so it is read by the same
// machinery every other row is.
var RecordedSchema = schema.Struct[Recorded]("Recorded",
	schema.FieldOf("aggregate", ledgerText,
		func(recorded Recorded) string { return recorded.Aggregate },
		func(recorded *Recorded, value string) { recorded.Aggregate = value }).
		Identity(),
	schema.FieldOf("version", ledgerText,
		func(recorded Recorded) string { return recorded.Version },
		func(recorded *Recorded, value string) { recorded.Version = value }),
).Documented("Recorded is which version of an aggregate a database holds.")

// ledgerText is bounded, because the aggregate name is the primary key and
// MySQL cannot key an unbounded string.
var ledgerText = schema.Text().Constrained(schema.MinLength(1), schema.MaxLength(255))

var ledgerKey = structure.Scalar{
	Kind:        structure.Text,
	Constraints: []structure.Constraint{structure.MaxLength{Value: 255}},
}

// createLedgerStatements is the statement that makes the ledger.
//
// The one table this module creates *if absent*, because it is the table that
// has to exist before anything can be read about what exists. Everywhere else
// this module refuses half-idempotency; here there is nowhere else to put the
// question.
func createLedgerStatements(dialect ddl.Dialect, ledger string) (string, error) {
	kind, err := dialect.Key(ledgerKey)
	if err != nil {
		return "", err
	}
	name := dialect.Quoted(ledger)
	aggregate := dialect.Quoted("aggregate")
	version := dialect.Quoted("version")
	return "create table if not exists " + name + " (\n" +
		"  " + aggregate + " " + kind + " not null,\n" +
		"  " + version + " " + kind + " not null,\n" +
		"  primary key (" + aggregate + ")\n" +
		")" + dialect.TableSuffix(), nil
}
