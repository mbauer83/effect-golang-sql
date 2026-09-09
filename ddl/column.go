package ddl

// One column, and the child tables a nested entity needs.

import (
	"fmt"
	"github.com/mbauer83/effect-golang-sql/sql"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// columnOf is the column a field becomes.
func columnOf(
	dialect Dialect,
	field structure.Field,
	identity structure.Field,
) (Column, error) {
	if field.Identity {
		return identityColumn(dialect, field)
	}
	if field.Computed && field.Default == nil {
		// The description says the value is not the caller's, and does not say
		// what produces it. A column with no default would make the table
		// un-insertable and one with an invented default would be a rule
		// nobody asked for -- so a computed column has to say, and a generated
		// key is the one that says it by being a key.
		return Column{}, errComputedUnstated
	}

	kind, nullable, err := resolveColumn(dialect, field.Node)
	if err != nil {
		return Column{}, err
	}
	fallback, err := defaultOf(dialect, field)
	if err != nil {
		return Column{}, err
	}
	return Column{
		Name:    field.Name,
		Doc:     firstParagraph(field.Doc),
		Type:    kind,
		Holds:   kindOfNode(field.Node),
		Default: fallback,
		// An optional field becomes a nullable column. They are different
		// questions -- a document may leave a field out, where a row must have
		// some state for every column -- and null is the state a row has for a
		// value nobody gave. A nullable node says the same thing outright.
		Nullable: nullable || field.Optional,
		Notes:    notesFor(field.Node),
	}, nil
}

// defaultOf is the dialect's spelling of what the field falls back to.
//
// The dialect gets to refuse first, because whether a column of that shape may
// carry a default at all is its rule and not the description's: a statement it
// would reject is worse than a description it will not project.
func defaultOf(dialect Dialect, field structure.Field) (string, error) {
	if field.Default != nil {
		if scalar, isScalar := scalarOf(field.Node); isScalar {
			if err := dialect.MayDefault(scalar); err != nil {
				return "", fmt.Errorf("the default of %q: %w", field.Name, err)
			}
		}
	}
	switch shape := field.Default.(type) {
	case nil:
		return "", nil
	case structure.DefaultNow:
		return dialect.Now(), nil
	case structure.DefaultTo:
		written, err := literal(dialect, shape.Value)
		if err != nil {
			return "", fmt.Errorf("the default of %q: %w", field.Name, err)
		}
		return written, nil
	default:
		return "", fmt.Errorf("%T is not a default this projection can write", shape)
	}
}

// identityColumn is the key.
//
// Computed means the database produces it, which is the one generated column
// this projection knows how to write. Not computed means the application
// supplies it -- a UUID, a natural key -- so it is an ordinary not-null column
// that happens to be the primary key.
func identityColumn(dialect Dialect, field structure.Field) (Column, error) {
	scalar, isScalar := field.Node.(structure.Scalar)
	if !isScalar {
		return Column{}, errCompositeIdentity
	}
	if field.Optional {
		return Column{}, errOptionalIdentity
	}

	if field.Computed {
		kind, err := dialect.Identity(scalar)
		if err != nil {
			return Column{}, err
		}
		return Column{
			Name: field.Name, Doc: firstParagraph(field.Doc),
			Type: kind, Holds: kindOfNode(field.Node), Identity: true,
		}, nil
	}
	kind, err := dialect.Key(scalar)
	if err != nil {
		return Column{}, err
	}
	return Column{
		Name: field.Name, Doc: firstParagraph(field.Doc),
		Type: kind, Holds: kindOfNode(field.Node), Notes: notesFor(field.Node),
	}, nil
}

// childTables are the tables an entity beneath the root needs.
func childTables(
	dialect Dialect,
	root structure.Object,
	identity structure.Field,
	field structure.Field,
) ([]Table, error) {
	entity, nested := structure.EntityBehind(field.Node)
	if !nested {
		return nil, nil
	}
	kind, _, err := resolveColumn(dialect, identity.Node)
	if err != nil {
		return nil, err
	}

	_, many := field.Node.(structure.Sequence)
	above := parent{
		table:     root.Name,
		column:    root.Name + "_" + identity.Name,
		kind:      kind,
		target:    identity.Name,
		atMostOne: !many,
	}
	tables, err := deriveTables(dialect, entity, &above)
	if err != nil {
		return nil, err
	}
	if _, ordered := field.Node.(structure.Sequence); ordered {
		if err := addPositionColumn(dialect, &tables[0]); err != nil {
			return nil, err
		}
	}
	return tables, nil
}

// addPositionColumn gives an ordered child the column that keeps its order.
//
// A list is ordered and a table is not, so without this a list read back would
// come in whatever order the database found convenient -- which is not the list
// that was written. The column is derived rather than declared, so a name
// collision is refused rather than resolved.
func addPositionColumn(dialect Dialect, table *Table) error {
	if _, taken := columnNamed(*table, positionColumn); taken {
		return fmt.Errorf("%s: %w: a column called %q is already there, and an ordered "+
			"child needs it to keep the order the list had",
			table.Name, errNameTaken, positionColumn)
	}
	kind, err := dialect.Column(structure.Scalar{
		Kind: structure.Integer, Precision: structure.Int32Bits,
	})
	if err != nil {
		return err
	}
	table.Columns = append(table.Columns, Column{
		Name:  positionColumn,
		Type:  kind,
		Holds: sql.OfWhole,
		Doc:   "where this sits in the list that holds it",
	})
	return nil
}

// positionColumn is what an ordered child's position is called.
const positionColumn = "position"

// object is the object a node is, following references.
func object(node structure.Node) (structure.Object, bool) {
	switch shape := node.(type) {
	case structure.Object:
		return shape, true
	case structure.Reference:
		if shape.Resolve == nil {
			return structure.Object{}, false
		}
		return object(shape.Resolve())
	default:
		return structure.Object{}, false
	}
}

func columnNamed(table Table, name string) (Column, bool) {
	for _, column := range table.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return Column{}, false
}

// firstParagraph is the part of a doc comment that belongs in a schema other
// people read.
func firstParagraph(doc string) string {
	if split := strings.Index(doc, "\n\n"); split >= 0 {
		return strings.TrimSpace(doc[:split])
	}
	return strings.TrimSpace(doc)
}

// scalarOf is the scalar a node is, through a nullable.
func scalarOf(node structure.Node) (structure.Scalar, bool) {
	switch shape := node.(type) {
	case structure.Scalar:
		return shape, true
	case structure.Nullable:
		return scalarOf(shape.Inner)
	default:
		return structure.Scalar{}, false
	}
}
