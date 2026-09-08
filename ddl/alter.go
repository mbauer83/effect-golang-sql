package ddl

// The statements that take an aggregate from one version to the next.
//
// This is where a declared change pays for itself. A rename is a rename --
// "alter table t rename column a to b" -- and the data stays where it is. A
// diff could only have seen a column gone and a column arrived, and the
// statements it wrote would have thrown the column's contents away.
//
// A relation is not a column, so a change to one is not a change to a column: a
// relation added is a table created, one removed is a table dropped, and one
// whose cardinality changed is a column and an index on the child. Which of
// those a change is depends on what the field was, which is why this walks the
// description as it was before each change rather than only the changes.

import (
	"errors"
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/evolve"
)

// Alter is the statements that carry an aggregate from one version to another,
// in the order they have to be run.
func Alter(
	dialect Dialect,
	history evolve.History,
	from string,
	to string,
) ([]string, error) {
	if err := history.Fault(); err != nil {
		return nil, err
	}
	stages, err := history.Stages(from, to)
	if err != nil {
		return nil, err
	}

	statements := []string{}
	for _, stage := range stages {
		written, err := altered(dialect, stage)
		if err != nil {
			return nil, fmt.Errorf("%q to %q of %s: %w", from, to, history.Name(), err)
		}
		statements = append(statements, written...)
	}
	return statements, nil
}

func altered(dialect Dialect, stage evolve.Stage) ([]string, error) {
	root, isObject := stage.Before.(structure.Object)
	if !isObject || root.Name == "" {
		return nil, errUnnamed
	}
	identity, named := root.Identity()
	if !named {
		return nil, errNoIdentity
	}

	switch held := stage.Change.(type) {
	case evolve.Added:
		return adding(dialect, root, identity, held)
	case evolve.Removed:
		return removing(dialect, root, identity, held)
	case evolve.Renamed:
		return renaming(dialect, root, held)
	case evolve.Retyped:
		return changing(dialect, root, identity, held)
	case evolve.Rewritten:
		return rewriting(dialect, stage, held)
	default:
		return nil, fmt.Errorf("%T is not a change this projection can write", stage.Change)
	}
}

// adding writes a field arriving: a column, or a whole child table.
func adding(
	dialect Dialect,
	root structure.Object,
	identity structure.Field,
	change evolve.Added,
) ([]string, error) {
	if _, related := structure.EntityBehind(change.Field.Node); related {
		// A relation, so what arrives is a table and not a column. Created
		// after the parent it references, which it already is.
		tables, err := childTables(dialect, root, identity, change.Field)
		if err != nil {
			return nil, err
		}
		return creating(dialect, tables), nil
	}
	column, err := columnOf(dialect, change.Field, structure.Field{})
	if err != nil {
		return nil, err
	}
	return []string{prefixed(dialect, root.Name) + "add column " +
		addedColumn(dialect, column)}, nil
}

// removing writes a field going: a column dropped, or a child table.
func removing(
	dialect Dialect,
	root structure.Object,
	identity structure.Field,
	change evolve.Removed,
) ([]string, error) {
	field, held := fieldNamed(root, change.Name)
	if !held {
		return nil, fmt.Errorf("%q: %w", change.Name, errNoSuchField)
	}
	if _, related := structure.EntityBehind(field.Node); related {
		tables, err := childTables(dialect, root, identity, field)
		if err != nil {
			return nil, err
		}
		// Children before the parent they reference, which is the reverse of
		// the order they were created in.
		statements := make([]string, 0, len(tables))
		for at := len(tables) - 1; at >= 0; at-- {
			statements = append(statements,
				"drop table "+dialect.Quoted(tables[at].Name))
		}
		return statements, nil
	}
	return []string{prefixed(dialect, root.Name) + "drop column " +
		dialect.Quoted(change.Name)}, nil
}

// renaming writes a column being renamed, and writes nothing for a relation.
//
// A relation's field name is not in the database at all: the child table is
// named for the entity and its reference column for the parent, so the name the
// root holds it under appears nowhere. Renaming it changes the description and
// nothing else, which is worth saying rather than leaving a caller to wonder
// why no statement came out.
func renaming(
	dialect Dialect,
	root structure.Object,
	change evolve.Renamed,
) ([]string, error) {
	field, held := fieldNamed(root, change.From)
	if !held {
		return nil, fmt.Errorf("%q: %w", change.From, errNoSuchField)
	}
	if _, related := structure.EntityBehind(field.Node); related {
		return nil, nil
	}
	return []string{prefixed(dialect, root.Name) + "rename column " +
		dialect.Quoted(change.From) + " to " + dialect.Quoted(change.To)}, nil
}

func prefixed(dialect Dialect, table string) string {
	return "alter table " + dialect.Quoted(table) + " "
}

func fieldNamed(object structure.Object, name string) (structure.Field, bool) {
	for _, field := range object.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return structure.Field{}, false
}

var (
	errNoSuchField = errors.New(
		"the version before this change has no such field, so there is nothing to write")
	errNothingForDialect = errors.New(
		"this change says how to move the rows for some dialects and not for the one " +
			"being projected, and half a rewriting is worse than none")
	errNoRetype = errors.New(
		"this dialect cannot change a column's type in place: make a new column, copy, and drop")
)

// rewriting writes what arrives, then what moves the rows, then what goes.
//
// A statement that fills a new column has to run after the column exists, and
// one that reads an old column has to run before it is dropped -- so the
// ordering is the shape of the change rather than something an author has to
// remember. By dialect name, because there is no dialect-neutral way to say
// "split this column", so a change with nothing to say for this dialect is
// refused rather than half-applied.
func rewriting(
	dialect Dialect,
	stage evolve.Stage,
	change evolve.Rewritten,
) ([]string, error) {
	adding, before, err := writing(dialect, change, change.Adding, stage.Before)
	if err != nil {
		return nil, err
	}

	moving, said := change.Forward.Statements[dialect.Name()]
	if !said {
		return nil, fmt.Errorf("%s: %w: %s", change.Doing, errNothingForDialect, dialect.Name())
	}

	// The sources go last, so the statement above had both ends to work with:
	// a split that dropped the old column first would be reading a column that
	// is not there.
	dropping, _, err := writing(dialect, change, change.Dropping, before)
	if err != nil {
		return nil, err
	}

	statements := append([]string{}, adding...)
	statements = append(statements, moving...)
	return append(statements, dropping...), nil
}

// writing is one list's statements, and the description they leave behind.
func writing(
	dialect Dialect,
	change evolve.Rewritten,
	list []evolve.Change,
	before structure.Node,
) ([]string, structure.Node, error) {
	statements := []string{}
	for _, held := range list {
		written, err := altered(dialect, evolve.Stage{Change: held, Before: before})
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", change.Doing, err)
		}
		statements = append(statements, written...)
		before, err = applying(held, before)
		if err != nil {
			return nil, nil, err
		}
	}
	return statements, before, nil
}

// applying is the description after one change, so the next change in a
// rewriting sees the shape the one before it made.
func applying(change evolve.Change, before structure.Node) (structure.Node, error) {
	object, isObject := before.(structure.Object)
	if !isObject {
		return nil, errUnnamed
	}
	after, err := evolve.Apply(change, object)
	if err != nil {
		return nil, err
	}
	return after, nil
}
