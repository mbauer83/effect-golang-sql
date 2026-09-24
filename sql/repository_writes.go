package sql

// Writing an aggregate: whole, as a delta read against what is kept; its root
// alone; what changed between two values of it, without reading; a new one
// whose identity the database generates; and its removal.

import (
	"errors"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// Save writes the whole aggregate, as a delta: what is kept is read -- one
// statement per table -- and compared by key. A row gone is deleted, a row new
// is inserted, a row changed is updated, a row the same is left alone; an
// element of a list keeps its place unless it moved.
func (repository Repository[A, ID]) Save[R any](database Querier, spelling Spelling, value A) effect.Effect[R, Fault, effect.Unit] {
	laid, desired, err := repository.rowsOf(spelling, value)
	if err != nil {
		return failSaving[R](repository, err)
	}
	root, _ := desired[0][0].Member(repository.key)
	return inTransaction[R](database, func(within Querier) effect.Effect[R, Fault, effect.Unit] {
		return repository.kept[R](within, spelling, laid, columnIs(laid[0].Name, repository.key, root)).
			FlatMap(func(existing [][]dynamic.Object) effect.Effect[R, Fault, effect.Unit] {
				return runAll[R](within, changes(spelling, laid, existing, desired, everyTable(laid)))
			})
	})
}

// SaveRoot writes the aggregate's root row alone -- inserted, or replaced
// under the same identity -- and leaves what is beneath it as it is: the
// dialect's upsert, one statement. A root with a unique key besides its
// identity is read first and then updated or inserted, since on MySQL an
// upsert would update whichever row its other key collided with.
func (repository Repository[A, ID]) SaveRoot[R any](database Querier, spelling Spelling, value A) effect.Effect[R, Fault, effect.Unit] {
	laid, rows, err := repository.rowsOf(spelling, value)
	if err != nil {
		return failSaving[R](repository, err)
	}
	root := laid[0]
	if !repository.alsoUnique {
		columns := make([]string, 0, len(root.Columns))
		for _, column := range root.Columns {
			columns = append(columns, column.Name)
		}
		return Run[R](database, UpsertQuery{Table: root.Name, Columns: columns, Key: root.Key,
			Values: valuesOf(rows[0][0], columns)}.Statement(spelling)).As(effect.Unit{})
	}
	identity, _ := rows[0][0].Member(repository.key)
	return inTransaction[R](database, func(within Querier) effect.Effect[R, Fault, effect.Unit] {
		source := From(root.Name, root.Columns...)
		return effect.RunCollect(rawRows[R](within, SelectQuery{Select: source.Columns(), From: source,
			Where: columnIs(root.Name, repository.key, identity)}.Statement(spelling))).
			FlatMap(func(existing []dynamic.Object) effect.Effect[R, Fault, effect.Unit] {
				return runAll[R](within, changes(spelling, laid[:1], [][]dynamic.Object{existing}, rows[:1], everyTable(laid)))
			})
	})
}

// SaveChanges writes what changed between two values of one aggregate
// without reading it: before is what is stored -- found in the transaction
// this runs in, or under a version the caller checks -- and after what is to
// be. Only a list whose order changed or that gained elements is read, for
// where its elements stand, so a moved element is still one row written.
func (repository Repository[A, ID]) SaveChanges[R any](database Querier, spelling Spelling, before A, after A) effect.Effect[R, Fault, effect.Unit] {
	laid, kept, err := repository.rowsOf(spelling, before)
	if err != nil {
		return failSaving[R](repository, err)
	}
	_, desired, err := repository.rowsOf(spelling, after)
	if err != nil {
		return failSaving[R](repository, err)
	}
	return inTransaction[R](database, func(within Querier) effect.Effect[R, Fault, effect.Unit] {
		placed := make([]bool, len(laid))
		var read func(at int) effect.Effect[R, Fault, effect.Unit]
		read = func(at int) effect.Effect[R, Fault, effect.Unit] {
			if at == len(laid) {
				return runAll[R](within, changes(spelling, laid, kept, desired, placed))
			}
			table := laid[at]
			if table.Position == "" || !reordered(table, kept[at], desired[at]) {
				return read(at + 1)
			}
			source := From(table.Name, table.Columns...)
			statement := SelectQuery{Select: source.Columns(), From: source, Where: rowsBeneath(laid, at, kept[0])}.Statement(spelling)
			return effect.RunCollect(rawRows[R](within, statement)).
				FlatMap(func(stored []dynamic.Object) effect.Effect[R, Fault, effect.Unit] {
					kept[at], placed[at] = stored, true
					return read(at + 1)
				})
		}
		return read(1)
	})
}

// Insert writes a new aggregate, leaving to the database the columns it
// fills -- an identity it generates, a default. One of the same key is
// refused as ErrAlreadyThere. An aggregate with anything beneath it is saved
// by its own identity, so a generated one has nothing beneath it.
func (repository Repository[A, ID]) Insert[R any](database Querier, spelling Spelling, value A) effect.Effect[R, Fault, effect.Unit] {
	laid, rows, err := repository.rowsOf(spelling, value)
	if err != nil {
		return failSaving[R](repository, err)
	}
	if repository.computed[repository.key] && len(laid) > 1 {
		return failSaving[R](repository, errors.New("sql: an aggregate whose identity the database generates holds nothing beneath it"))
	}
	return inTransaction[R](database, func(within Querier) effect.Effect[R, Fault, effect.Unit] {
		statements := insertRows(spelling, laid[0], rows[0], repository.computed)
		for at := 1; at < len(laid); at++ {
			statements = append(statements, insertRows(spelling, laid[at], rows[at], nil)...)
		}
		return runAll[R](within, statements)
	})
}

// Delete removes the aggregate with that identity and everything beneath it;
// the outcome says whether one was kept.
func (repository Repository[A, ID]) Delete[R any](database Querier, spelling Spelling, identity ID) effect.Effect[R, Fault, Outcome] {
	laid, err := repository.layout(spelling)
	if err != nil {
		return effect.For[R, Fault]().Fail[Outcome](faultOf("delete an aggregate", repository.TableName(), err))
	}
	return inTransaction[R](database, func(within Querier) effect.Effect[R, Fault, Outcome] {
		return repository.kept[R](within, spelling, laid, repository.identityIs(identity)).
			FlatMap(func(existing [][]dynamic.Object) effect.Effect[R, Fault, Outcome] {
				gone := changes(spelling, laid, existing, make([][]dynamic.Object, len(laid)), everyTable(laid))
				return runAll[R](within, gone).As(Outcome{RowsAffected: int64(len(existing[0]))})
			})
	})
}

func failSaving[R, A, ID any](repository Repository[A, ID], err error) effect.Effect[R, Fault, effect.Unit] {
	return effect.For[R, Fault]().Fail[effect.Unit](faultOf("save an aggregate", repository.TableName(), err))
}
