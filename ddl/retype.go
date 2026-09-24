package ddl

// A change of shape, which is the change the dialects disagree about most.
//
// A column's type, spelled two ways and refused by a third. And a relation's
// cardinality, which is not a type at all in the database: one becomes many by
// dropping the constraint that kept it to one, and many becomes one by adding
// it back -- which the database will refuse if the rows do not already satisfy
// it, and that refusal is the right answer.

import (
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

// RetypeForm is how a dialect spells a change of a column's type.
type RetypeForm uint8

const (
	// RetypeTypeOnly changes the type and leaves the rest of the definition
	// alone, which is what Postgres does.
	RetypeTypeOnly RetypeForm = iota
	// RetypeWhole restates the whole definition, which is what MySQL requires
	// -- so anything the restatement leaves out is lost.
	RetypeWhole
)

// changeColumn writes a change of shape.
func changeColumn(
	dialect Dialect,
	root structure.Object,
	identity structure.Field,
	change evolve.Retype,
) ([]string, error) {
	before, found := findField(root, change.Name)
	if !found {
		return nil, fmt.Errorf("%q: %w", change.Name, errNoSuchField)
	}

	was, related := structure.EntityBehind(before.Node)
	becomes, stillRelated := structure.EntityBehind(change.Node)
	switch {
	case related && stillRelated:
		return recardinalise(dialect, root, identity, change, was, becomes)
	case related != stillRelated:
		// A relation becoming a column, or a column becoming a relation, is a
		// table appearing or going as well as a column changing. Two changes,
		// and saying so is free -- where guessing an order for them would be
		// choosing which of the two the caller meant.
		return nil, fmt.Errorf("%q: %w", change.Name, errAcrossTheDivide)
	}
	return retypeColumn(dialect, root, change)
}

// retypeColumn writes a plain column's new type.
func retypeColumn(
	dialect Dialect,
	root structure.Object,
	change evolve.Retype,
) ([]string, error) {
	column, err := columnOf(dialect,
		structure.Field{Name: change.Name, Node: change.Node}, structure.Field{})
	if err != nil {
		return nil, err
	}
	form, err := dialect.Retype()
	if err != nil {
		return nil, err
	}
	if form == RetypeWhole {
		return []string{alterTable(dialect, root.Name) + "MODIFY COLUMN " +
			columnClause(dialect, column)}, nil
	}
	return []string{alterTable(dialect, root.Name) + "ALTER COLUMN " +
		dialect.QuoteIdentifier(column.Name) + " TYPE " + column.Type}, nil
}

// recardinalise writes a relation going from one to many, or many to one.
//
// The child table stays where it is either way: the entity is the same entity,
// so its rows are the same rows. What changes is whether a parent may have more
// than one of them, and that is the index on the reference column -- unique for
// one, ordinary for many -- plus the position column, which only an ordered
// relation has.
func recardinalise(
	dialect Dialect,
	root structure.Object,
	identity structure.Field,
	change evolve.Retype,
	was structure.Object,
	becomes structure.Object,
) ([]string, error) {
	if was.Name != becomes.Name {
		// A different entity is a different table, so this is a relation
		// removed and another added rather than one changed.
		return nil, fmt.Errorf("%q: %w", change.Name, errAnotherEntity)
	}

	before, err := childTables(dialect, root,
		identity, structure.Field{Name: change.Name, Node: fieldNodeOf(root, change.Name)})
	if err != nil {
		return nil, err
	}
	after, err := childTables(dialect, root,
		identity, structure.Field{Name: change.Name, Node: change.Node})
	if err != nil {
		return nil, err
	}
	child := before[0].Name
	reference := before[0].Indexes[0]

	_, wasOrdered := findColumn(before[0], positionColumn)
	_, isOrdered := findColumn(after[0], positionColumn)
	statements := []string{}

	switch {
	case !wasOrdered && isOrdered:
		// One to many: the child gains its place in the list, and the index on
		// the reference stops being unique.
		position, err := positionColumnOf(dialect)
		if err != nil {
			return nil, err
		}
		statements = append(statements,
			alterTable(dialect, child)+"ADD COLUMN "+columnClause(dialect, position),
			"DROP INDEX "+dialect.QuoteIdentifier(reference.Name)+onTable(dialect, child),
			Index{Name: reference.Name, Columns: reference.Columns}.Create(dialect, child))
	case wasOrdered && !isOrdered:
		// Many to one: the place in the list goes, and the index becomes
		// unique -- which the database refuses if some parent already has two,
		// and that refusal is the honest answer rather than something to
		// smooth over.
		statements = append(statements,
			alterTable(dialect, child)+"DROP COLUMN "+dialect.QuoteIdentifier(positionColumn),
			"DROP INDEX "+dialect.QuoteIdentifier(reference.Name)+onTable(dialect, child),
			Index{Name: reference.Name, Columns: reference.Columns, Unique: true}.
				Create(dialect, child))
	}
	return statements, nil
}

// onTable is the part of a drop-index statement MySQL needs and Postgres does
// not: an index belongs to a table there and to the schema here.
func onTable(dialect Dialect, table string) string {
	if dialect.IndexBelongsToTable() {
		return " ON " + dialect.QuoteIdentifier(table)
	}
	return ""
}

func fieldNodeOf(root structure.Object, name string) structure.Node {
	field, _ := findField(root, name)
	return field.Node
}

func positionColumnOf(dialect Dialect) (Column, error) {
	kind, err := dialect.Column(structure.Scalar{
		Kind: structure.Integer, Precision: structure.Int32Bits,
	})
	if err != nil {
		return Column{}, err
	}
	return Column{
		Name:    positionColumn,
		Type:    kind,
		Comment: "where this sits in the list that holds it",
	}, nil
}
