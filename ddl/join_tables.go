package ddl

// A list of references to other aggregates, as a join table.
//
// Held in the aggregate -- read and written with it -- but not owned by it:
// each element identifies an aggregate of its own, so the table is the
// holder's key, the element's, and where it stands in the list, with a
// foreign key each way. The holder's goes with the holder; the element's
// deletes as the reference says, restricting unless it says otherwise.

import (
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// referencesListed is the reference a list's elements are, when a field is a
// list of references.
func referencesListed(node structure.Node) (structure.Scalar, bool) {
	sequence, isSequence := node.(structure.Sequence)
	if !isSequence {
		return structure.Scalar{}, false
	}
	scalar, isScalar := sequence.Element.(structure.Scalar)
	return scalar, isScalar && scalar.Refers != nil
}

// joinTable is the table a list of references is kept in: holder_field, with
// the holder's key, the element, and its position; each element once.
func joinTable(dialect Dialect, key holderKey, root structure.Object, field structure.Field) (Table, error) {
	element, _ := referencesListed(field.Node)
	elementKind, err := dialect.Column(narrowed(element))
	if err != nil {
		return Table{}, err
	}
	elementColumn := element.Refers.Object + "_" + element.Refers.Key
	for _, column := range key.columns {
		if column == elementColumn {
			elementColumn = field.Name + "_" + element.Refers.Key
		}
	}
	table := Table{
		Name:       root.Name + "_" + field.Name,
		Comment:    firstParagraph(field.Description),
		PrimaryKey: append(append([]string(nil), key.columns...), elementColumn),
		ForeignKeys: []ForeignKey{{
			Columns: key.columns, Table: key.table, Targets: key.targets, OnDelete: structure.Cascade,
		}},
		Parent: &ParentLink{Table: key.table, Columns: key.columns, Targets: key.targets, Field: field.Name, Element: elementColumn},
	}
	for at, column := range key.columns {
		table.Columns = append(table.Columns, Column{Name: column, Type: key.kinds[at], Comment: "the " + key.table + " whose list this is"})
	}
	listed := Column{Name: elementColumn, Type: elementKind, Kind: kindOfNode(element), Comment: "the " + element.Refers.Object + " it lists"}
	table.Columns = append(table.Columns, listed)
	if err := addTarget(&table, root, reference{
		field:  structure.Field{Name: elementColumn, Node: element},
		column: listed,
		target: *element.Refers,
	}); err != nil {
		return Table{}, err
	}
	if err := addPositionColumn(dialect, &table); err != nil {
		return Table{}, err
	}
	return table, nil
}
