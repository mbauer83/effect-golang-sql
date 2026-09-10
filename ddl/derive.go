package ddl

// Which tables an aggregate is, and what is in them.

import (
	"errors"
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Tables are the tables an aggregate's description implies: the root first,
// then the entities beneath it in the order they were declared.
//
// The order is not decoration. A parent has to exist before a child can
// reference it, so this is the order the statements have to be run in, and
// reversing it is the order they have to be dropped in.
func Tables(dialect Dialect, node structure.Node) ([]Table, error) {
	root, isObject := object(node)
	if !isObject {
		return nil, errNotAnObject
	}
	if !root.IsEntity() {
		// A value has no identity, so there is no row to find again. Storing
		// one as a table would be storing something nothing can refer to.
		return nil, errNoIdentity
	}
	return deriveTables(dialect, root, nil)
}

// parent is what a child table needs to know about the table above it.
type parent struct {
	table  string
	column string
	kind   string
	target string
	// atMostOne says the parent may have at most one of these, which is what
	// a relation that is not a list says and what the index then enforces.
	atMostOne bool
}

func deriveTables(dialect Dialect, root structure.Object, above *parent) ([]Table, error) {
	if root.Name == "" {
		return nil, errUnnamed
	}
	identities := root.Identities()
	identity, _ := root.Identity()

	table := Table{Name: root.Name, Doc: firstParagraph(root.Doc)}
	children := []structure.Field{}

	for _, field := range root.Fields {
		if _, nested := structure.EntityBehind(field.Node); nested {
			children = append(children, field)
			continue
		}
		column, err := columnOf(dialect, field, identity)
		if err != nil {
			return nil, fmt.Errorf("field %q of %s: %w", field.Name, root.Name, err)
		}
		table.Columns = append(table.Columns, column)
	}
	// No check that the table holds more than its key. A root that is only an
	// identity with children beneath it is a legitimate aggregate -- a basket
	// is its lines and nothing else -- and every entity has an identity, so
	// there is no case where a table would come out with no columns at all.
	table.PrimaryKey = namesOf(identities)

	if above != nil {
		if len(identities) > 1 {
			// A child of a composite-keyed parent would need a reference of
			// several columns, and nothing here writes one. Refused where the
			// description is read rather than projected into a foreign key
			// pointing at half a key, which a database would accept and then
			// enforce nothing with.
			return nil, fmt.Errorf("%s: %w", root.Name, errCompositeChild)
		}
		if err := reference(&table, *above, root); err != nil {
			return nil, err
		}
		// A child entity's identity distinguishes it among its parent's and
		// not among everybody's, so the key is the parent and the identity
		// together. That is what being a child means and needs no declaring.
		//
		// It is also load-bearing. An identity a client chooses -- a viewing
		// somebody reports, a line somebody adds -- is chosen within their own
		// records, and a single-column key makes it global: two people who
		// pick the same one collide, and depending on how the row is written
		// one of them is refused or one of them silently replaces the other.
		table.PrimaryKey = []string{above.column, identity.Name}
	}

	tables := []Table{table}
	for _, field := range children {
		below, err := childTables(dialect, root, identity, field)
		if err != nil {
			return nil, fmt.Errorf("field %q of %s: %w", field.Name, root.Name, err)
		}
		tables = append(tables, below...)
	}
	return tables, nil
}

// reference gives a child the column that points at its parent, and the index
// that makes looking children up by parent something other than a scan.
//
// The index is unique when the parent may have at most one of these, which is
// what a relation that is not a list says. Without that nothing would enforce
// it and the description would be making a claim the schema did not keep --
// and it is what makes a change of cardinality a change of this index.
func reference(table *Table, above parent, root structure.Object) error {
	if _, taken := columnNamed(*table, above.column); taken {
		return fmt.Errorf(
			"%s: %w: a column called %q is already there, so the reference to %s has nowhere to go",
			root.Name, errNameTaken, above.column, above.table)
	}
	table.Columns = append(table.Columns, Column{
		Name: above.column,
		Type: above.kind,
		Doc:  "the " + above.table + " this belongs to",
	})
	table.ForeignKeys = append(table.ForeignKeys, ForeignKey{
		Columns: []string{above.column},
		Table:   above.table,
		Targets: []string{above.target},
		Cascade: true,
	})
	table.Indexes = append(table.Indexes, Index{
		Name:    table.Name + "_" + above.column,
		Columns: []string{above.column},
		Unique:  above.atMostOne,
	})
	return nil
}

var (
	errNotAnObject = errors.New("a table holds rows of named values, and this description has none")
	errNoIdentity  = errors.New(
		"a table's rows have to be findable again, and this description names no identity: " +
			"mark one field Identity")
	errUnnamed = errors.New(
		"a table has a name, and this description has none: name the struct, because a name " +
			"taken from the field holding it would change when that field did")
	errNameTaken        = errors.New("a derived column collides with a declared one")
	errComputedUnstated = errors.New(
		"the description says this value is computed and not what computes it, so there is " +
			"no default to write: a generated key is the one computed column this can " +
			"produce, and the rest needs the description to say")
	errCompositeIdentity = errors.New(
		"an identity is one value, and this field holds something with parts")
	errOptionalIdentity = errors.New(
		"an identity that may be absent identifies nothing")
	errCompositeParent = errors.New(
		"an entity whose identity is several fields cannot hold entities of its own yet: " +
			"a child references its parent by one column, and pointing one at half a key " +
			"is a foreign key a database accepts and enforces nothing with")
	errCompositeChild = errors.New(
		"an entity held by another cannot have an identity of several fields: its key is " +
			"already its parent's reference and its own identity, so the second field " +
			"would be a third column in a key nothing selects a row by")
)

// namesOf are these fields' names, in the order the description named them --
// which is the order a composite key's columns are written in, and therefore
// the order the index sorts by.
func namesOf(fields []structure.Field) []string {
	named := make([]string, 0, len(fields))
	for _, field := range fields {
		named = append(named, field.Name)
	}
	return named
}
