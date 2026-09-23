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

// LedgerEntry is one aggregate's row in the ledger.
type LedgerEntry struct {
	// Aggregate is the fully-qualified name the history is of.
	Aggregate string
	// Version is the name of the version its tables are at.
	Version string
}

// LedgerEntrySchema describes a row of the ledger, so it is read by the same
// machinery every other row is.
var LedgerEntrySchema = schema.Struct[LedgerEntry]("LedgerEntry",
	schema.FieldOf("aggregate", ledgerText,
		func(entry LedgerEntry) string { return entry.Aggregate },
		func(entry *LedgerEntry, value string) { entry.Aggregate = value }).
		Identity(),
	schema.FieldOf("version", ledgerText,
		func(entry LedgerEntry) string { return entry.Version },
		func(entry *LedgerEntry, value string) { entry.Version = value }),
).WithDescription("LedgerEntry is which version of an aggregate a database holds.")

// ledgerText is bounded, because the aggregate name is the primary key and
// MySQL cannot key an unbounded string.
var ledgerText = schema.Text().Check(schema.MinLength(1), schema.MaxLength(255))

var ledgerKey = structure.Scalar{
	Kind:        structure.Text,
	Constraints: []structure.Constraint{structure.MaxLength{Value: 255}},
}

// createLedgerStatement is the statement that makes the ledger.
//
// The one table this module creates *if absent*, because it is the table that
// has to exist before anything can be read about what exists. Everywhere else
// this module refuses half-idempotency; here there is nowhere else to put the
// question.
func createLedgerStatement(dialect ddl.Dialect, ledger string) (string, error) {
	kind, err := dialect.Key(ledgerKey)
	if err != nil {
		return "", err
	}
	name := dialect.QuoteIdentifier(ledger)
	aggregate := dialect.QuoteIdentifier("aggregate")
	version := dialect.QuoteIdentifier("version")
	return "create table if not exists " + name + " (\n" +
		"  " + aggregate + " " + kind + " not null,\n" +
		"  " + version + " " + kind + " not null,\n" +
		"  primary key (" + aggregate + ")\n" +
		")" + dialect.TableSuffix(), nil
}
