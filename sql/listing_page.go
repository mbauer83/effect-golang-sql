package sql

// Reading a listing's pages, and counting its rows.

import (
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang/effect"
)

// Page reads one page of the listing.
//
// A keyset page reads one row more than it holds, to know whether another
// follows. A numbered page reads only the keys of the rows before it -- which
// an index answers without reading the table -- and joins the page's own rows
// to them, so passing over earlier rows costs index entries rather than rows.
func (listing Listing[A]) Page[R any](database Querier, spelling Spelling, query PageQuery) effect.Effect[R, Fault, Page[A]] {
	asked, err := listing.plan(spelling, query)
	if err != nil {
		return effect.For[R, Fault]().Fail[Page[A]](faultOf("read a page", "", err))
	}
	reading, backwards, err := listing.reading(asked)
	if err != nil {
		return effect.For[R, Fault]().Fail[Page[A]](faultOf("read a page", "", err))
	}
	return effect.RunCollect(rawRows[R](database, reading.Statement(spelling))).
		FlatMap(func(rows []dynamic.Object) effect.Effect[R, Fault, Page[A]] {
			page, err := listing.pageOf(asked, rows, backwards)
			if err != nil {
				return effect.For[R, Fault]().Fail[Page[A]](faultOf("read a page", "", err))
			}
			return effect.For[R, Fault]().Succeed(page)
		})
}

// reading is the query a page is read with, and whether it reads backwards.
func (listing Listing[A]) reading(asked plan) (SelectQuery, bool, error) {
	source := listing.source
	switch {
	case !asked.before.IsStart():
		position, err := positionOf(asked.before, asked.tag)
		if err != nil {
			return SelectQuery{}, false, err
		}
		return SelectQuery{
			With:   listing.with,
			Select: source.Columns(), From: source, Where: asked.where,
			OrderBy: reversed(asked.order), After: position, Limit: asked.size + 1,
		}, true, nil
	case asked.number > 0:
		keys := make([]Term, 0, len(listing.key))
		for _, column := range listing.key {
			keys = append(keys, Term{node: node{kind: aColumn, source: source.alias, name: column}})
		}
		passed := SelectQuery{
			Select: SelectTerms(keys...), From: source, Where: asked.where, OrderBy: asked.order,
			Limit: asked.size + 1, Offset: (asked.number - 1) * asked.size,
		}
		page := FromQuery(passed).As("page")
		on := make([]Criterion, 0, len(listing.key))
		for _, column := range listing.key {
			on = append(on, ColumnsEqual(source, column, page, column))
		}
		return SelectQuery{
			With:   listing.with,
			Select: qualifiedColumns(source), From: source,
			Joins:   []Join{InnerJoin(page, Both(on...))},
			OrderBy: qualifiedOrder(asked.order, source),
		}, false, nil
	default:
		var position []dynamic.Value
		if !asked.after.IsStart() {
			var err error
			if position, err = positionOf(asked.after, asked.tag); err != nil {
				return SelectQuery{}, false, err
			}
		}
		return SelectQuery{
			With:   listing.with,
			Select: source.Columns(), From: source, Where: asked.where,
			OrderBy: asked.order, After: position, Limit: asked.size + 1,
		}, false, nil
	}
}

// pageOf is the rows read as a page, with the cursors that continue it.
func (listing Listing[A]) pageOf(asked plan, rows []dynamic.Object, backwards bool) (Page[A], error) {
	more := len(rows) > asked.size
	if more {
		rows = rows[:asked.size]
	}
	if backwards {
		for low, high := 0, len(rows)-1; low < high; low, high = low+1, high-1 {
			rows[low], rows[high] = rows[high], rows[low]
		}
	}
	page := Page[A]{Items: make([]A, 0, len(rows)), Number: asked.number}
	for _, row := range rows {
		item, err := schema.FromDynamic(listing.shape, row)
		if err != nil {
			return Page[A]{}, err
		}
		page.Items = append(page.Items, item)
	}
	if len(rows) == 0 {
		return page, nil
	}
	first, err := listing.cursorAt(asked, rows[0])
	if err != nil {
		return Page[A]{}, err
	}
	last, err := listing.cursorAt(asked, rows[len(rows)-1])
	if err != nil {
		return Page[A]{}, err
	}
	switch {
	case backwards:
		page.Next = last
		if more {
			page.Previous = first
		}
	default:
		if more {
			page.Next = last
		}
		if !asked.after.IsStart() || asked.number > 1 {
			page.Previous = first
		}
	}
	return page, nil
}

// cursorAt is a row's position in the plan's order.
func (listing Listing[A]) cursorAt(asked plan, object dynamic.Object) (PageCursor, error) {
	values := make([]dynamic.Value, 0, len(asked.order))
	for _, ordering := range asked.order {
		value, _ := object.Member(ordering.term.name)
		values = append(values, value)
	}
	return cursorOf(asked.tag, values)
}

// Count is how many rows the listing holds that where admits. It reads every
// one of them; CountUpTo stops at a number.
func (listing Listing[A]) Count[R any](database Querier, spelling Spelling, where Criterion) effect.Effect[R, Fault, int64] {
	counting := SelectQuery{
		With:   listing.with,
		Select: []Selection{Count().As("count")}, From: listing.source, Where: Both(listing.scope, where),
	}
	return Row[R](database, countSchema, counting.Statement(spelling)).Map(func(row countRow) int64 { return row.Count })
}

// CountUpTo is how many rows where admits, counting no further than most: for
// "more than a thousand", at the cost of at most that many index entries.
func (listing Listing[A]) CountUpTo[R any](database Querier, spelling Spelling, where Criterion, most int) effect.Effect[R, Fault, int64] {
	keys := make([]Term, 0, len(listing.key))
	for _, column := range listing.key {
		keys = append(keys, Term{node: node{kind: aColumn, source: listing.source.alias, name: column}})
	}
	capped := SelectQuery{
		Select: SelectTerms(keys...), From: listing.source, Where: Both(listing.scope, where), Limit: most,
	}
	counting := SelectQuery{
		With:   listing.with,
		Select: []Selection{Count().As("count")}, From: FromQuery(capped).As("capped"),
	}
	return Row[R](database, countSchema, counting.Statement(spelling)).Map(func(row countRow) int64 { return row.Count })
}

type countRow struct{ Count int64 }

var countSchema = schema.Struct[countRow]("count",
	schema.FieldAt("count", schema.Int64(), func(row *countRow) *int64 { return &row.Count }))

// reversed is an order read the other way, for a page before a position.
func reversed(order []Ordering) []Ordering {
	flipped := make([]Ordering, len(order))
	for index, ordering := range order {
		ordering.descending = !ordering.descending
		flipped[index] = ordering
	}
	return flipped
}

// qualifiedColumns and qualifiedOrder name a source's columns with its table,
// for a query that also reads a derived table holding columns of the same
// names.
func qualifiedColumns(source Source) []Selection {
	selections := source.Columns()
	for index := range selections {
		selections[index].term = qualify(selections[index].term, source)
	}
	return selections
}

func qualifiedOrder(order []Ordering, source Source) []Ordering {
	qualified := make([]Ordering, len(order))
	for index, ordering := range order {
		ordering.term = qualify(ordering.term, source)
		qualified[index] = ordering
	}
	return qualified
}

func qualify(term node, source Source) node {
	if term.kind == aColumn && term.source == "" {
		term.source = source.table
	}
	return term
}

// Statement is the statement a page would be read with: what a test composes
// against each dialect, and what an EXPLAIN is asked about.
func (listing Listing[A]) Statement(spelling Spelling, query PageQuery) Statement {
	asked, err := listing.plan(spelling, query)
	if err != nil {
		return Compose(spelling, Refusal(err))
	}
	reading, _, err := listing.reading(asked)
	if err != nil {
		return Compose(spelling, Refusal(err))
	}
	return reading.Statement(spelling)
}

// CursorAt is the position just past a row with these sort values -- the
// sort's own, then the key's -- under a sort and filter: how a reader seeks to
// a value rather than walking to it, and how a test stands a page somewhere.
func (listing Listing[A]) CursorAt(spelling Spelling, query PageQuery, values ...dynamic.Value) (PageCursor, error) {
	asked, err := listing.plan(spelling, PageQuery{Where: query.Where, Sort: query.Sort})
	if err != nil {
		return "", err
	}
	if len(values) != len(asked.order) {
		return "", fmt.Errorf("%w: a position in this sort has %d values, and %d were given",
			ErrPageQuery, len(asked.order), len(values))
	}
	return cursorOf(asked.tag, values)
}
