package ddl

// Joins on the keys between tables, so a join's condition is never written
// again by hand: it is the foreign key, which the description already implies.

import (
	"errors"
	"fmt"

	"github.com/mbauer83/effect-golang-sql/sql"
)

var (
	errNoKeyBetween = errors.New("no foreign key relates them")
	errSeveralKeys  = errors.New("more than one foreign key relates them, so a join has to say which")
)

// Join is the rows of to that the rows of from relate to, on the key between
// them, in whichever direction it points.
func Join(from Table, to Table) (sql.Join, error) {
	on, err := KeyCondition(from, from.Source(), to, to.Source())
	if err != nil {
		return sql.Join{}, err
	}
	return sql.InnerJoin(to.Source(), on), nil
}

// LeftJoin is Join keeping the rows of from that nothing in to relates to: an
// optional reference, or a parent with no children yet.
func LeftJoin(from Table, to Table) (sql.Join, error) {
	on, err := KeyCondition(from, from.Source(), to, to.Source())
	if err != nil {
		return sql.Join{}, err
	}
	return sql.LeftJoin(to.Source(), on), nil
}

// KeyCondition is the condition that relates rows of two tables on the one
// foreign key between them, read from the sources the query names them by --
// aliased, as a query that reads one table twice has to.
func KeyCondition(from Table, fromSource sql.Source, to Table, toSource sql.Source) (sql.Criterion, error) {
	var found []sql.Criterion
	add := func(key ForeignKey, referring sql.Source, referred sql.Source) {
		parts := make([]sql.Criterion, 0, len(key.Columns))
		for index, column := range key.Columns {
			parts = append(parts, sql.ColumnsEqual(referring, column, referred, key.Targets[index]))
		}
		found = append(found, sql.Both(parts...))
	}
	for _, key := range from.ForeignKeys {
		if key.Table == to.Name {
			add(key, fromSource, toSource)
		}
	}
	for _, key := range to.ForeignKeys {
		if key.Table == from.Name && from.Name != to.Name {
			add(key, toSource, fromSource)
		}
	}
	switch len(found) {
	case 0:
		return sql.Criterion{}, fmt.Errorf("join %s to %s: %w", from.Name, to.Name, errNoKeyBetween)
	case 1:
		return found[0], nil
	default:
		return sql.Criterion{}, fmt.Errorf("join %s to %s: %w", from.Name, to.Name, errSeveralKeys)
	}
}
