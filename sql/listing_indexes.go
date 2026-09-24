package sql

// The indexes a listing's pages are read by.
//
// Declared, because the index that serves a query best is a judgement: how
// many rows each column tells apart, whether it should carry the columns a page
// reads so that query never touches the table, whether one index serves
// several queries. A listing says which indexes it is read by; one that asks for
// DerivedIndex gets the index its declaration implies, which is a fair default
// and nothing more.

// Index is an index on a table: its key columns in order, the columns it
// carries besides them, and whether it is unique. One Index value may serve
// several listings, which is how it is stated once.
type Index struct {
	Name    string
	Columns []string
	// Include are columns the index carries without ordering by them --
	// Postgres's INCLUDE, SQL Server's included columns. A query that reads
	// only an index's columns is answered from the index alone, which is what
	// makes the index covering for that query: coverage is the query's, not
	// the index's.
	Include []string
	Unique  bool
	derived bool
}

// IndexOn is an index of that name on those columns, in that order.
func IndexOn(name string, columns ...string) Index {
	return Index{Name: name, Columns: columns}
}

// WithInclude is the index carrying columns besides its key: Postgres's
// INCLUDE.
func (index Index) WithInclude(columns ...string) Index {
	index.Include = append(append([]string(nil), index.Include...), columns...)
	return index
}

// DerivedIndex asks a listing for the index its declaration implies, one for
// each of its sorts: the columns its scope fixes by equality, then the sort's,
// then the key. A default for a listing whose best index is that one.
var DerivedIndex = Index{derived: true}

// IndexedBy says which indexes the listing's pages are read by.
func (listing Listing[A]) IndexedBy(indexes ...Index) Listing[A] {
	listing.indexes = append(append([]Index(nil), listing.indexes...), indexes...)
	return listing
}

// Indexes are the indexes the listing is read by: those it was given, and for
// DerivedIndex the ones its declaration implies.
func (listing Listing[A]) Indexes() []Index {
	var indexes []Index
	for _, index := range listing.indexes {
		if index.derived {
			for _, derived := range listing.derivedIndexes() {
				indexes = appendIndex(indexes, derived)
			}
			continue
		}
		indexes = appendIndex(indexes, index)
	}
	return indexes
}

// derivedIndexes are the index each sort implies. A sort read the other way
// needs no index of its own: an index is read in either direction.
func (listing Listing[A]) derivedIndexes() []Index {
	scoped := equalityColumns(listing.scope.node)
	indexes := make([]Index, 0, len(listing.sorts))
	for _, declared := range listing.sorts {
		columns := append([]string(nil), scoped...)
		for _, ordering := range declared.order {
			columns = appendMissing(columns, ordering.term.name)
		}
		for _, column := range listing.key {
			columns = appendMissing(columns, column)
		}
		indexes = appendIndex(indexes, IndexOn(listing.source.table+"_by_"+declared.name, columns...))
	}
	return indexes
}

// appendIndex adds an index unless one with the same columns is there.
func appendIndex(indexes []Index, index Index) []Index {
	for _, held := range indexes {
		if sameColumns(held.Columns, index.Columns) {
			return indexes
		}
	}
	return append(indexes, index)
}

func sameColumns(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// equalityColumns are the columns a criterion fixes by equality with a value,
// through any conjunction of them.
func equalityColumns(criterion node) []string {
	if criterion.kind != anApplication {
		return nil
	}
	switch criterion.operation {
	case Conjunction, Bracket:
		var columns []string
		for _, member := range criterion.arguments {
			for _, column := range equalityColumns(member) {
				columns = appendMissing(columns, column)
			}
		}
		return columns
	case EqualTo:
		if len(criterion.arguments) == 2 {
			left, right := criterion.arguments[0], criterion.arguments[1]
			switch {
			case left.kind == aColumn && right.kind == aValue:
				return []string{left.name}
			case right.kind == aColumn && left.kind == aValue:
				return []string{right.name}
			}
		}
	}
	return nil
}

func appendMissing(columns []string, column string) []string {
	for _, held := range columns {
		if held == column {
			return columns
		}
	}
	return append(columns, column)
}
