package ddl

// The tables an aggregate is, as data before they are statements.

// Table is one table.
type Table struct {
	Name string
	Doc  string
	// Columns are its own, in the order the description declares them, with
	// any foreign key to a parent last -- because a reader looking for what
	// the row *is* should not have to step over the plumbing first.
	Columns []Column
	// PrimaryKey names the columns that identify a row. One column for an
	// entity with an identity; two for a child whose identity is only unique
	// within its parent.
	PrimaryKey []string
	// ForeignKeys are the references to a parent table.
	ForeignKeys []ForeignKey
	// Indexes are what the description implies rather than what a workload
	// needs: a foreign key gets one, because a parent's children are looked up
	// by parent and a database that had to scan for them would be the wrong
	// answer to a question the schema itself asks.
	Indexes []Index
}

// Column is one column.
type Column struct {
	Name string
	Doc  string
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
	// Notes are what the description says and DDL has no way to state -- the
	// constraints, principally. Comments, because a comment is honest about
	// not being enforced where an invented CHECK would be a rule nobody asked
	// for and every dialect spells differently.
	Notes []string
}

// ForeignKey is a child's reference to its parent.
type ForeignKey struct {
	Columns []string
	Table   string
	Targets []string
	// Cascade says the child goes when the parent does.
	//
	// True for every key this derivation writes, and that is the point of the
	// aggregate being the unit: a child entity has no life without its root,
	// so a row that outlived its parent would be unreachable. A relationship
	// between two roots is not this and is not derived.
	Cascade bool
}

// Index is one index.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
}
