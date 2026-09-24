package ddl

import (
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// The tables an aggregate is, as data before they are statements.

// Table is one table.
type Table struct {
	Name    string
	Comment string
	// Columns are its own, in the order the description declares them, with
	// any foreign key to a parent last -- because a reader looking for what
	// the row *is* should not have to step over the plumbing first.
	Columns []Column
	// PrimaryKey names the columns that identify a row. One column for an
	// entity with an identity; two for a child whose identity is only unique
	// within its parent.
	PrimaryKey []string
	// ForeignKeys are the references to a parent table, and to the tables of
	// the aggregates this one refers to.
	ForeignKeys []ForeignKey
	// Indexes are what the description implies rather than what a workload
	// needs: a foreign key gets one, because a parent's children are looked up
	// by parent and a database that had to scan for them would be the wrong
	// answer to a question the schema itself asks.
	Indexes []Index
	// Parent is, for a table beneath the root, where its rows belong: nil for
	// the root.
	Parent *ParentLink
}

// ParentLink is where a table's rows belong in the aggregate: which table's
// rows hold them, the columns that refer to the holder, the holder's key
// columns they refer to, and the member of the holder they are.
type ParentLink struct {
	Table   string
	Columns []string
	Targets []string
	Field   string
	// Single says a holder has at most one: a member that is one entity
	// rather than a list.
	Single bool
	// Element is, for a join table, the column holding each element: the
	// rows are references, not entities of their own.
	Element string
}

// Column is one column.
type Column struct {
	Name    string
	Comment string
	// Type is the dialect's own spelling, already resolved: this is data ready
	// to be written, not a description to be interpreted again.
	Type string
	// Nullable says the column admits null.
	//
	// It is *not* the description's Optional. An optional field is one a
	// document may leave out; a nullable column is one a row may have no value
	// for. They coincide often enough to be confused and are different
	// questions, so the derivation decides deliberately and says how.
	Nullable bool
	// Identity says the database produces the value, which is the one
	// generated column that needs no default.
	Identity bool
	// Default is the dialect's own spelling of what the column falls back to,
	// or empty when the description states none.
	Default string
	// Kind is the kind of value the column takes as the *description* said
	// it, which is a different question from Type: Type is this dialect's
	// spelling, ready to be written, and this is what a query may compare the
	// column to. Unknown for the columns the projection invents rather than
	// reads -- a reference to a parent -- because their kind is the parent's
	// and a query joining on one is checked by its name.
	Kind sql.Kind
	// Checks are the rules the description states that the database keeps:
	// a length, a bound, a pattern where the dialect has regular expressions.
	Checks []Check
	// Notes are what the description says and this dialect cannot check -- a
	// format, a pattern on SQLite -- as comments, which are honest about not
	// being enforced.
	Notes []string
}

// ForeignKey is a child's reference to its parent.
type ForeignKey struct {
	Columns []string
	Table   string
	Targets []string
	// OnDelete is what deleting the referenced row does: a child goes with its
	// parent, and a reference to another aggregate restricts unless its
	// description says otherwise.
	//
	// True for every key this derivation writes, and that is the point of the
	// aggregate being the unit: a child entity has no life without its root,
	// so a row that outlived its parent would be unreachable. A relationship
	// between two roots is not this and is not derived.
	OnDelete structure.Deletion
}

// Index is one index.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
}

// Source is this table as somewhere a query reads from, knowing its columns
// and what each of them holds.
//
// The schema-driven seam: a store that projected a description hands the
// result to a query, and every expression the query takes from it is checked
// against the description -- by name, and by the kind the description said.
func (table Table) Source() sql.Source {
	return sql.From(table.Name, table.ColumnTypes()...)
}

// ColumnTypes are this table's columns and what each holds.
func (table Table) ColumnTypes() []sql.ColumnType {
	holds := make([]sql.ColumnType, 0, len(table.Columns))
	for _, column := range table.Columns {
		holds = append(holds, sql.ColumnOf(column.Name, column.Kind))
	}
	return holds
}

// kindOfNode is what a described node holds, as a query's kind.
func kindOfNode(node structure.Node) sql.Kind { return sql.KindOf(node) }
