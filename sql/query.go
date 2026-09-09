package sql

// Running statements. A result set is a stream, so a large one need not be
// held; a row is an object, so the schema that decodes a request body decodes a
// row.

import (
	"context"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// Query runs a statement and streams its rows, each decoded through the schema.
//
// The cursor is acquired in the consumer's scope, so it is released when the
// consumer is finished -- including when it stopped early, failed, or was
// cancelled. A consumer that reads three rows of a million does not read the
// rest and does not hold them.
func Query[R, A any](
	database Querying,
	shape schema.Schema[A],
	statement string,
	arguments ...dynamic.Value,
) effect.Stream[R, Fault, A] {
	return effect.StreamFromResource(
		func(scope effect.Scope) effect.Effect[R, Fault, Cursor] {
			return opening[R](scope, database, statement, arguments)
		},
		func(cursor Cursor) effect.Stream[R, Fault, A] {
			return rows[R](cursor, shape, statement)
		},
	)
}

// QueryRow runs a statement that must return exactly one row.
//
// Neither none nor several is the answer to a question phrased this way, so
// both are refused: a caller that wanted "none is fine" wants Query and a look
// at what came back.
func QueryRow[R, A any](
	database Querying,
	shape schema.Schema[A],
	statement string,
	arguments ...dynamic.Value,
) effect.Effect[R, Fault, A] {
	// Two, so that a second row is noticed rather than quietly ignored.
	return effect.RunCollect(
		Query[R](database, shape, statement, arguments...).TakeStream(2),
	).FlatMap(func(found []A) effect.Effect[R, Fault, A] {
		operations := effect.For[R, Fault]()
		switch len(found) {
		case 1:
			return operations.Succeed(found[0])
		case 0:
			return operations.Fail[A](faulted("reading one row", statement, ErrNoRows))
		default:
			return operations.Fail[A](faulted("reading one row", statement, ErrSeveralRows))
		}
	}).Named("query-row")
}

// Execute runs a statement that returns no rows.
func Execute[R any](
	database Querying,
	statement string,
	arguments ...dynamic.Value,
) effect.Effect[R, Fault, Outcome] {
	return effect.Try(
		func(ctx context.Context, _ R) (Outcome, error) {
			return database.Execute(ctx, statement, arguments)
		},
		func(err error) Fault { return faulted("executing", statement, err) },
	).Named("execute")
}

// opening starts the walk and gives the scope the cursor to release.
func opening[R any](
	scope effect.Scope,
	database Querying,
	statement string,
	arguments []dynamic.Value,
) effect.Effect[R, Fault, Cursor] {
	acquire := effect.Try(
		func(ctx context.Context, _ R) (Cursor, error) {
			return database.Query(ctx, statement, arguments)
		},
		func(err error) Fault { return faulted("querying", statement, err) },
	).Named("query")

	return scope.AcquireRelease(acquire, closing[R])
}

func closing[R any](cursor Cursor) effect.Effect[R, effect.Never, effect.Unit] {
	return effect.Release[R](func(context.Context) error { return cursor.Close() })
}

// rows walks the cursor, decoding each row through the schema.
func rows[R, A any](
	cursor Cursor,
	shape schema.Schema[A],
	statement string,
) effect.Stream[R, Fault, A] {
	return effect.StreamFromSteps(func() effect.Effect[R, Fault, effect.Step[A]] {
		return effect.From(func(context.Context, R) effect.Exit[Fault, effect.Step[A]] {
			if !cursor.Next() {
				return finished[A](cursor, statement)
			}
			row, err := cursor.Row()
			if err != nil {
				return effect.ExitFailure[Fault, effect.Step[A]](
					faulted("reading a row", statement, err))
			}
			decoded, err := schema.FromDynamic(shape, row)
			if err != nil {
				return effect.ExitFailure[Fault, effect.Step[A]](
					faulted("decoding a row", statement, err))
			}
			return effect.ExitSuccess[Fault](effect.Emit(effect.ChunkOf(decoded)))
		})
	})
}

// finished decides what the end of the cursor meant: the end of the rows, or
// something that went wrong while walking them.
func finished[A any](cursor Cursor, statement string) effect.Exit[Fault, effect.Step[A]] {
	if err := cursor.Err(); err != nil {
		return effect.ExitFailure[Fault, effect.Step[A]](
			faulted("walking the rows", statement, err))
	}
	return effect.ExitSuccess[Fault](effect.EndOfStream[A]())
}

// Run, Rows and Row are the three above, given a statement that was composed
// rather than written.
//
// They exist for two reasons. Nothing outside this package ever takes a
// Composed apart: the text and the values it binds agreed when they were
// rendered together, and a caller that unpacked them to pass them on would be
// the one place that could put them back in the wrong order. And a statement
// that could not be composed -- a column no source has, an operation this
// dialect cannot perform -- fails here, carrying what was wrong, rather than
// reaching a server. There is no path by which a refused statement is sent.

// Run runs a composed statement that returns no rows.
func Run[R any](database Querying, statement Composed) effect.Effect[R, Fault, Outcome] {
	if why := statement.Refused(); why != nil {
		return effect.For[R, Fault]().Fail[Outcome](refusedStatement(why))
	}
	return Execute[R](database, statement.Text(), statement.Values()...)
}

// Rows streams what a composed statement returns, each row decoded through the
// schema.
func Rows[R, A any](
	database Querying,
	shape schema.Schema[A],
	statement Composed,
) effect.Stream[R, Fault, A] {
	if why := statement.Refused(); why != nil {
		return effect.StreamFail[R, A, Fault](refusedStatement(why))
	}
	return Query[R](database, shape, statement.Text(), statement.Values()...)
}

// Row is the one row a composed statement must return.
func Row[R, A any](
	database Querying,
	shape schema.Schema[A],
	statement Composed,
) effect.Effect[R, Fault, A] {
	if why := statement.Refused(); why != nil {
		return effect.For[R, Fault]().Fail[A](refusedStatement(why))
	}
	return QueryRow[R](database, shape, statement.Text(), statement.Values()...)
}
