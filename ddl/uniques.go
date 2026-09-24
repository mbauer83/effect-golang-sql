package ddl

// The rules no two rows may share, as unique indexes.

import (
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// addUniques gives each field the description says no two rows share a unique
// index -- after the reference to a parent, which a child's first index is.
//
// The column has to be one the dialect can index: MySQL cannot key unbounded
// text, and says so here rather than when the statement runs.
func addUniques(dialect Dialect, table *Table, root structure.Object) error {
	for _, field := range root.Fields {
		if !field.Unique || field.Identity {
			continue
		}
		if _, nested := structure.EntityBehind(field.Node); nested {
			continue
		}
		node := field.Node
		if nullable, wrapped := node.(structure.Nullable); wrapped {
			node = nullable.Inner
		}
		if scalar, isScalar := node.(structure.Scalar); isScalar {
			if _, err := dialect.Key(scalar); err != nil {
				return fmt.Errorf("field %q of %s is unique: %w", field.Name, root.Name, err)
			}
		}
		table.Indexes = append(table.Indexes, Index{
			Name:    table.Name + "_" + field.Name + "_unique",
			Columns: []string{field.Name},
			Unique:  true,
		})
	}
	return addUniqueKeys(dialect, table, root)
}

// addUniqueKeys makes a unique index of each unique key: the fields that name
// it, in the order they are declared.
func addUniqueKeys(dialect Dialect, table *Table, root structure.Object) error {
	var keys []string
	columns := map[string][]string{}
	for _, field := range root.Fields {
		if field.UniqueKey == "" {
			continue
		}
		if _, nested := structure.EntityBehind(field.Node); nested {
			return fmt.Errorf("field %q of %s is a relation, and one of the unique key %q", field.Name, root.Name, field.UniqueKey)
		}
		if scalar, isScalar := scalarOf(field.Node); isScalar {
			if _, err := dialect.Key(scalar); err != nil {
				return fmt.Errorf("field %q of %s is one of the unique key %q: %w", field.Name, root.Name, field.UniqueKey, err)
			}
		}
		if _, known := columns[field.UniqueKey]; !known {
			keys = append(keys, field.UniqueKey)
		}
		columns[field.UniqueKey] = append(columns[field.UniqueKey], field.Name)
	}
	for _, key := range keys {
		table.Indexes = append(table.Indexes, Index{Name: table.Name + "_" + key, Columns: columns[key], Unique: true})
	}
	return nil
}
