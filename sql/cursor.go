package sql

// Walking a result set, and a transaction that answers the same operations a
// database does.

import (
	"context"

	stdsql "database/sql"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// stdCursor is a database/sql result set behind the cursor.
type stdCursor struct {
	rows *stdsql.Rows
}

func (cursor *stdCursor) Next() bool {
	return cursor.rows.Next()
}

// Row reads the current row as the named values it is.
//
// The names come from the result set rather than from the schema, so a
// statement that selected a column the schema does not know contributes a
// member the decoder skips -- which is the same tolerance a document gets, and
// for the same reason: a query written for a newer table should still read.
func (cursor *stdCursor) Row() (dynamic.Object, error) {
	names, err := cursor.rows.Columns()
	if err != nil {
		return dynamic.Object{}, err
	}
	into, values := destinations(len(names))
	if err := cursor.rows.Scan(into...); err != nil {
		return dynamic.Object{}, err
	}

	row := dynamic.Object{Fields: make([]dynamic.Field, 0, len(names))}
	for index, name := range names {
		row.Fields = append(row.Fields,
			dynamic.Field{Name: name, Value: values[index].value})
	}
	return row, nil
}

func (cursor *stdCursor) Err() error {
	return cursor.rows.Err()
}

func (cursor *stdCursor) Close() error {
	return cursor.rows.Close()
}

// stdTransaction is a database/sql transaction behind the port.
//
// It answers Query and Execute and not Begin, because a transaction cannot
// start one: nested transactions are a different feature with different
// semantics, and a type offering one it does not have would be lying.
type stdTransaction struct {
	transaction *stdsql.Tx
	// instants is the connection's own, because a transaction binds values
	// the same way the database it began on does.
	instants Instants
}

func (open *stdTransaction) Query(
	ctx context.Context,
	statement string,
	arguments []dynamic.Value,
) (Cursor, error) {
	values, err := bindings(arguments, open.instants)
	if err != nil {
		return nil, err
	}
	rows, err := open.transaction.QueryContext(ctx, statement, values...)
	if err != nil {
		return nil, err
	}
	return &stdCursor{rows: rows}, nil
}

func (open *stdTransaction) Execute(
	ctx context.Context,
	statement string,
	arguments []dynamic.Value,
) (Outcome, error) {
	values, err := bindings(arguments, open.instants)
	if err != nil {
		return Outcome{}, err
	}
	result, err := open.transaction.ExecContext(ctx, statement, values...)
	if err != nil {
		return Outcome{}, err
	}
	return outcomeOf(result), nil
}

func (open *stdTransaction) Commit() error {
	return open.transaction.Commit()
}

func (open *stdTransaction) Rollback() error {
	return open.transaction.Rollback()
}
