package ddl

// References to other aggregates, as foreign keys.

import (
	"errors"
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// reference is a column that identifies another aggregate.
type reference struct {
	field  structure.Field
	column Column
	target structure.Target
}

// targetOf is what a field's value identifies, when it is a reference.
func targetOf(node structure.Node) (structure.Target, bool) {
	if nullable, wrapped := node.(structure.Nullable); wrapped {
		node = nullable.Inner
	}
	scalar, isScalar := node.(structure.Scalar)
	if !isScalar || scalar.Refers == nil {
		return structure.Target{}, false
	}
	return *scalar.Refers, true
}

var (
	errUnmappedTarget = errors.New(
		"the mapping was not told where it is stored: name its mapping with Referring")
	errSetNullRequired = errors.New(
		"deleting the target would empty it, and it is required: make it optional or restrict")
)

// addTarget gives a reference its foreign key, and the index a lookup by it
// and a join on it use -- unique when the field is, which addUniques makes.
func addTarget(table *Table, root structure.Object, held reference) error {
	if held.target.Table == "" {
		return fmt.Errorf("field %q of %s refers to %s, and %w",
			held.field.Name, root.Name, held.target.Object, errUnmappedTarget)
	}
	if held.target.OnDelete == structure.SetNull && !held.column.Nullable {
		return fmt.Errorf("field %q of %s: %w", held.field.Name, root.Name, errSetNullRequired)
	}
	table.ForeignKeys = append(table.ForeignKeys, ForeignKey{
		Columns:  []string{held.column.Name},
		Table:    held.target.Table,
		Targets:  []string{held.target.Column},
		OnDelete: held.target.OnDelete,
	})
	if !held.field.Unique {
		table.Indexes = append(table.Indexes, Index{
			Name:    table.Name + "_" + held.column.Name,
			Columns: []string{held.column.Name},
		})
	}
	return nil
}
