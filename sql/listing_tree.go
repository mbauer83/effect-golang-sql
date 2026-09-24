package sql

// A page of aggregates kept in several tables.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// whole is a page's rows with what each holds beneath it, for a listing of
// aggregates kept in several tables: one statement per table, for the page.
func (listing Listing[A]) whole[R any](database Querier, spelling Spelling, rows []dynamic.Object) effect.Effect[R, Fault, []dynamic.Object] {
	if listing.tree == nil || len(rows) == 0 {
		return effect.For[R, Fault]().Succeed(rows)
	}
	laid, err := listing.tree.of(spelling)
	if err != nil {
		return effect.For[R, Fault]().Fail[[]dynamic.Object](faultOf("read a page", "", err))
	}
	if len(laid) == 1 {
		return effect.For[R, Fault]().Succeed(rows)
	}
	return readTree[R](database, spelling, laid, rows).
		Map(func(read [][]dynamic.Object) []dynamic.Object { return assemble(laid, read) })
}
