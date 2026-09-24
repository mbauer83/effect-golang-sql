package ddl

// The tables an aggregate is, as a repository writes and reads them.

import (
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// layout is the tables a description implies, as the repository needs them:
// names, columns, keys, and where each table's rows belong.
func layout(dialect Dialect, node structure.Node) ([]sql.TableLayout, error) {
	tables, err := Tables(dialect, node)
	if err != nil {
		return nil, err
	}
	laid := make([]sql.TableLayout, 0, len(tables))
	for _, table := range tables {
		one := sql.TableLayout{Name: table.Name, Columns: table.ColumnTypes(), Key: table.PrimaryKey}
		if _, ordered := findColumn(table, positionColumn); ordered {
			one.Position = positionColumn
		}
		if link := table.Parent; link != nil {
			one.Parent = &sql.ParentLayout{
				Table: link.Table, Columns: link.Columns, Targets: link.Targets,
				Field: link.Field, Single: link.Single, Element: link.Element,
			}
		}
		laid = append(laid, one)
	}
	return laid, nil
}

// Layout is the tables Postgres keeps an aggregate in.
func (dialect postgres) Layout(node structure.Node) ([]sql.TableLayout, error) {
	return layout(dialect, node)
}

// Layout is the tables MySQL keeps an aggregate in.
func (dialect mysql) Layout(node structure.Node) ([]sql.TableLayout, error) {
	return layout(dialect, node)
}

// Layout is the tables SQLite keeps an aggregate in.
func (dialect sqlite) Layout(node structure.Node) ([]sql.TableLayout, error) {
	return layout(dialect, node)
}
