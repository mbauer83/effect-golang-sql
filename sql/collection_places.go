package sql

// Where an element goes in an order a person chose: the key between the
// neighbours of the place it is put.

import (
	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// keyFor is a key for a place in owner's collection. An element being moved is
// left out of its own neighbours. A key that has grown too long has the owner's
// keys rewritten evenly first, and is found again.
func (collection Collection[ID, E]) keyFor[R any](database Querier, spelling Spelling, owner ID, at Placement[E], moving *E) effect.Effect[R, Fault, string] {
	return collection.placeFor[R](database, spelling, owner, at, moving).
		FlatMap(func(key string) effect.Effect[R, Fault, string] {
			if len(key) <= longestOrderKey {
				return effect.For[R, Fault]().Succeed(key)
			}
			return collection.rewriteKeys[R](database, spelling, owner).
				FlatMap(func(effect.Unit) effect.Effect[R, Fault, string] {
					return collection.placeFor[R](database, spelling, owner, at, moving)
				})
		})
}

func (collection Collection[ID, E]) placeFor[R any](database Querier, spelling Spelling, owner ID, at Placement[E], moving *E) effect.Effect[R, Fault, string] {
	scope := collection.ownerIs(owner)
	if moving != nil {
		scope = Both(scope, Not(collection.elementIs(*moving)))
	}
	placed := Of[string](collection.source, position)
	between := func(before string, after string) effect.Effect[R, Fault, string] {
		key, err := OrderKeyBetween(before, after)
		if err != nil {
			return effect.For[R, Fault]().Fail[string](faultOf("place an element", "", err))
		}
		return effect.For[R, Fault]().Succeed(key)
	}
	switch at.at {
	case atStart:
		return collection.extreme[R](database, spelling, Min(placed), scope).
			FlatMap(func(first string) effect.Effect[R, Fault, string] { return between("", first) })
	case beforeOne, afterOne:
		return collection.placeOf[R](database, spelling, owner, at.element).
			FlatMap(func(beside string) effect.Effect[R, Fault, string] {
				if at.at == afterOne {
					return collection.extreme[R](database, spelling, Min(placed),
						Both(scope, Above(placed, Param(beside)))).
						FlatMap(func(next string) effect.Effect[R, Fault, string] { return between(beside, next) })
				}
				return collection.extreme[R](database, spelling, Max(placed),
					Both(scope, Below(placed, Param(beside)))).
					FlatMap(func(previous string) effect.Effect[R, Fault, string] { return between(previous, beside) })
			})
	default:
		return collection.extreme[R](database, spelling, Max(placed), scope).
			FlatMap(func(last string) effect.Effect[R, Fault, string] { return between(last, "") })
	}
}

// extreme is the least or greatest key the criterion admits, and empty when it
// admits none.
func (collection Collection[ID, E]) extreme[R any](database Querier, spelling Spelling, of Expr[string], where Criterion) effect.Effect[R, Fault, string] {
	reading := SelectQuery{Select: []Selection{of.As(position)}, From: collection.source, Where: where}
	return Row[R](database, placeSchema, reading.Statement(spelling)).
		Map(func(row placeRow) string { return row.Key })
}

// placeOf is where an element of owner's collection stands.
func (collection Collection[ID, E]) placeOf[R any](database Querier, spelling Spelling, owner ID, element E) effect.Effect[R, Fault, string] {
	reading := SelectQuery{
		Select: []Selection{Of[string](collection.source, position).As(position)},
		From:   collection.source,
		Where:  Both(collection.ownerIs(owner), collection.elementIs(element)),
	}
	return effect.RunCollect(Rows[R](database, placeSchema, reading.Statement(spelling))).
		FlatMap(func(rows []placeRow) effect.Effect[R, Fault, string] {
			if len(rows) == 0 {
				return effect.For[R, Fault]().Fail[string](faultOf("place an element", "", ErrNotInCollection))
			}
			return effect.For[R, Fault]().Succeed(rows[0].Key)
		})
}

// rewriteKeys gives owner's elements evenly spread keys, in the order they
// stand: what keys that have grown too long become. A statement per element,
// so run it inside a transaction where several writers may meet; interrupted,
// it leaves every element a key, and the order as it was or as it becomes.
func (collection Collection[ID, E]) rewriteKeys[R any](database Querier, spelling Spelling, owner ID) effect.Effect[R, Fault, effect.Unit] {
	stored := collection.stored.Schema()
	reading := SelectQuery{
		Select:  collection.source.Columns(),
		From:    collection.source,
		Where:   collection.ownerIs(owner),
		OrderBy: []Ordering{Of[string](collection.source, position).Ascending()},
	}
	return effect.RunCollect(Rows[R](database, stored, reading.Statement(spelling))).
		FlatMap(func(rows []collectionRow[ID, E]) effect.Effect[R, Fault, effect.Unit] {
			keys := evenOrderKeys(len(rows))
			for index := range rows {
				rows[index].Position = keys[index]
			}
			return effect.ForEach(rows, func(row collectionRow[ID, E]) effect.Effect[R, Fault, effect.Unit] {
				return collection.execute[R](database, UpdateQuery{
					Table: collection.source.table, Columns: []string{position},
					Values: []dynamic.Value{dynamic.OfText(row.Position)},
					Where:  Both(collection.ownerIs(owner), collection.elementIs(row.Element)),
				}.Statement(spelling))
			}).As(effect.Unit{})
		})
}

type placeRow struct{ Key string }

var placeSchema = schema.Struct[placeRow]("",
	schema.OptionalFieldOf(position, schema.Text(),
		func(row placeRow) (string, bool) { return row.Key, row.Key != "" },
		func(row *placeRow, key string) { row.Key = key }))
