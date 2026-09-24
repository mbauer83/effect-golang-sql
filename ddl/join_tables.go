package ddl

// A list of references to other aggregates, as a join table.
//
// Held in the aggregate -- read and written with it -- but not owned by it:
// each element identifies an aggregate of its own, so the table is the
// holder's key, the element's, and where it stands in the list, with a
// foreign key each way. The holder's goes with the holder; the element's
// deletes as the reference says, restricting unless it says otherwise.

import (
	"fmt"

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
func joinTable(dialect Dialect, root structure.Object, identity structure.Field, field structure.Field) (Table, error) {
	element, _ := referencesListed(field.Node)
	if len(root.Identities()) > 1 {
		return Table{}, fmt.Errorf("%s: %w", root.Name, errCompositeParent)
	}
	holderKind, _, err := resolveColumn(dialect, identity.Node)
	if err != nil {
		return Table{}, err
	}
	elementKind, err := dialect.Column(narrowed(element))
	if err != nil {
		return Table{}, err
	}
	holder := root.Name + "_" + identity.Name
	elementColumn := element.Refers.Object + "_" + element.Refers.Key
	if elementColumn == holder {
		elementColumn = field.Name + "_" + element.Refers.Key
	}
	table := Table{
		Name:    root.Name + "_" + field.Name,
		Comment: firstParagraph(field.Description),
		Columns: []Column{
			{Name: holder, Type: holderKind, Kind: kindOfNode(identity.Node), Comment: "the " + root.Name + " whose list this is"},
			{Name: elementColumn, Type: elementKind, Kind: kindOfNode(element), Comment: "the " + element.Refers.Object + " it lists"},
		},
		PrimaryKey: []string{holder, elementColumn},
		ForeignKeys: []ForeignKey{{
			Columns: []string{holder}, Table: root.Name, Targets: []string{identity.Name}, OnDelete: structure.Cascade,
		}},
		Parent: &ParentLink{Table: root.Name, Column: holder, Target: identity.Name, Field: field.Name, Element: elementColumn},
	}
	if err := addTarget(&table, root, reference{
		field:  structure.Field{Name: elementColumn, Node: element},
		column: table.Columns[1],
		target: *element.Refers,
	}); err != nil {
		return Table{}, err
	}
	if err := addPositionColumn(dialect, &table); err != nil {
		return Table{}, err
	}
	return table, nil
}
