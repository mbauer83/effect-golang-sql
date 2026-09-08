package sql

// Walking a result set, and a transaction that answers the same operations a
// database does.

import (
	"context"

	stdsql "database/sql"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// walking is a database/sql result set behind the cursor.
type walking struct {
	rows *stdsql.Rows
}

func (cursor *walking) Next() bool {
	return cursor.rows.Next()
}

// Row reads the current row as the named values it is.
//
// The names come from the result set rather than from the schema, so a
// statement that selected a column the schema does not know contributes a
// member the decoder skips -- which is the same tolerance a document gets, and
// for the same reason: a query written for a newer table should still read.
func (cursor *walking) Row() (dynamic.Object, error) {
	names, err := cursor.rows.Columns()
	if err != nil {
		return dynamic.Object{}, err
	}
	into, received := destinations(len(names))
	if err := cursor.rows.Scan(into...); err != nil {
		return dynamic.Object{}, err
	}

	row := dynamic.Object{Fields: make([]dynamic.Field, 0, len(names))}
	for index, name := range names {
		row.Fields = append(row.Fields,
			dynamic.Field{Name: name, Value: received[index].held})
	}
	return row, nil
}

func (cursor *walking) Err() error {
	return cursor.rows.Err()
}

func (cursor *walking) Close() error {
	return cursor.rows.Close()
}

// transacting is a database/sql transaction behind the port.
//
// It answers Query and Execute and not Begin, because a transaction cannot
// start one: nested transactions are a different feature with different
// semantics, and a type offering one it does not have would be lying.
type transacting struct {
	transaction *stdsql.Tx
}

func (open *transacting) Query(
	ctx context.Context,
	statement string,
	arguments []dynamic.Value,
) (Cursor, error) {
	bound, err := bindings(arguments)
	if err != nil {
		return nil, err
	}
	rows, err := open.transaction.QueryContext(ctx, statement, bound...)
	if err != nil {
		return nil, err
	}
	return &walking{rows: rows}, nil
}

func (open *transacting) Execute(
	ctx context.Context,
	statement string,
	arguments []dynamic.Value,
) (Outcome, error) {
	bound, err := bindings(arguments)
	if err != nil {
		return Outcome{}, err
	}
	result, err := open.transaction.ExecContext(ctx, statement, bound...)
	if err != nil {
		return Outcome{}, err
	}
	return outcomeOf(result), nil
}

func (open *transacting) Commit() error {
	return open.transaction.Commit()
}

func (open *transacting) Rollback() error {
	return open.transaction.Rollback()
}
