package sql

// A query target read a page at a time.
//
// A listing is a table, a read model or one owner's collection, with what it
// offers a reader declared once: the sorts it can be read in, how many rows a
// page may hold, and how deep a numbered page may go. A page is asked for by
// keyset -- after or before a cursor -- or by number, and every sort ends with
// the listing's key, so a page boundary is exact under either.

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema"
)

// Listing is a query target read a page at a time.
type Listing[A any] struct {
	shape  schema.Schema[A]
	source Source
	key    []string
	sorts  []listingSort
	// defaultSize is a page's size when a query does not say, and
	// largestSize the most one may ask for.
	defaultSize int
	largestSize int
	// deepestPage is the furthest numbered page, or zero for no limit.
	deepestPage int
	// scope is the rows this listing is of at all: one owner's, for a
	// collection.
	scope Criterion
	// with are the named expressions its source is read from.
	with  []CTE
	fault error
}

type listingSort struct {
	name  string
	order []Ordering
}

// ErrPageQuery is a page asked for in a way its listing does not offer: a
// client's mistake, which the error goes on to name.
var ErrPageQuery = errors.New("sql: a page asked for what its listing does not offer")

// NewListing is rows of source, read through shape, identified by the key
// columns -- the last word of every sort, so no two rows tie. A page holds 25
// rows unless asked, and at most 100, until PageSize says otherwise.
func NewListing[A any](shape schema.Schema[A], source Source, key ...string) Listing[A] {
	listing := Listing[A]{shape: shape, source: source, key: key, defaultSize: 25, largestSize: 100}
	if len(key) == 0 {
		listing.fault = errors.New("sql: a listing names the key that tells its rows apart")
	}
	return listing
}

// Sort offers an order to read the listing in, under a name a reader asks for
// it by. The first one declared is the one a reader gets without asking. Each
// orders by columns of the listing's source, which is what a cursor records.
func (listing Listing[A]) Sort(name string, order ...Ordering) Listing[A] {
	for _, ordering := range order {
		if ordering.term.kind != aColumn {
			listing.fault = fmt.Errorf("sql: the sort %q orders by something other than a column", name)
		}
	}
	listing.sorts = append(append([]listingSort(nil), listing.sorts...), listingSort{name: name, order: order})
	return listing
}

// PageSize is how many rows a page holds when a query does not say, and the
// most a query may ask for.
func (listing Listing[A]) PageSize(defaultSize int, largest int) Listing[A] {
	if defaultSize < 1 || largest < defaultSize {
		listing.fault = errors.New("sql: a page holds at least one row, and the largest at least the default")
	}
	listing.defaultSize, listing.largestSize = defaultSize, largest
	return listing
}

// DeepestPage is the furthest numbered page a query may ask for: a table that
// grows without bound cannot then be asked to pass over millions of rows.
func (listing Listing[A]) DeepestPage(page int) Listing[A] {
	listing.deepestPage = page
	return listing
}

// Within narrows the listing to the rows it is of: one owner's, for a
// collection, so no page of it can reach another owner's rows.
func (listing Listing[A]) Within(scope Criterion) Listing[A] {
	listing.scope = scope
	return listing
}

// With names the expressions the listing's source is read from, when the
// source is one of them: a read model that computes its columns before they
// are sorted and filtered by, which a select list cannot do for itself.
func (listing Listing[A]) With(expressions ...CTE) Listing[A] {
	listing.with = append(append([]CTE(nil), listing.with...), expressions...)
	return listing
}

// PageQuery is which page of a listing to read.
//
// A page is asked for by one of After, Before or Number, or by none of them for
// the first page: two positions at once would be two questions.
type PageQuery struct {
	// Where is which rows, within the listing.
	Where Criterion
	// Sort is the declared sort to read in; empty is the first declared.
	Sort string
	// After and Before are keyset positions: the rows after one, or before.
	After  PageCursor
	Before PageCursor
	// Number is a numbered page, counted from one.
	Number int
	// Size is how many rows; zero is the listing's default.
	Size int
}

// Page is one page of a listing, with the cursors that continue it either way.
// Next is empty on the last page and Previous on the first.
type Page[A any] struct {
	Items    []A
	Next     PageCursor
	Previous PageCursor
	// Number is the page's number, when it was asked for by one.
	Number int
}

// plan is what a query asks, checked against what the listing offers.
type plan struct {
	order  []Ordering
	where  Criterion
	tag    string
	size   int
	after  PageCursor
	before PageCursor
	number int
}

func (listing Listing[A]) plan(spelling Spelling, query PageQuery) (plan, error) {
	if listing.fault != nil {
		return plan{}, listing.fault
	}
	if len(listing.sorts) == 0 {
		return plan{}, errors.New("sql: a listing offers at least one sort")
	}
	size := query.Size
	if size == 0 {
		size = listing.defaultSize
	}
	if size < 1 || size > listing.largestSize {
		return plan{}, fmt.Errorf("%w: a page holds from 1 to %d rows, and %d were asked for",
			ErrPageQuery, listing.largestSize, size)
	}
	chosen, err := listing.sortNamed(query.Sort)
	if err != nil {
		return plan{}, err
	}
	positions := 0
	for _, given := range []bool{!query.After.IsStart(), !query.Before.IsStart(), query.Number != 0} {
		if given {
			positions++
		}
	}
	switch {
	case positions > 1:
		return plan{}, fmt.Errorf("%w: a page is after a cursor, before one, or numbered, and not two of those", ErrPageQuery)
	case query.Number < 0:
		return plan{}, fmt.Errorf("%w: pages are numbered from 1", ErrPageQuery)
	case listing.deepestPage > 0 && query.Number > listing.deepestPage:
		return plan{}, fmt.Errorf("%w: pages go no deeper than %d", ErrPageQuery, listing.deepestPage)
	}
	// The key breaks ties in the direction the sort ends in: a list read
	// newest first shows, of two rows of one moment, the one keyed later.
	order := append([]Ordering(nil), chosen.order...)
	descending := len(order) > 0 && order[len(order)-1].descending
	for _, column := range listing.key {
		order = append(order, Ordering{
			term:       node{kind: aColumn, source: listing.source.alias, name: column},
			descending: descending,
		})
	}
	where := And(listing.scope, query.Where)
	return plan{
		order: order, where: where, size: size,
		tag:   chosen.name + "/" + fingerprint(spelling, where),
		after: query.After, before: query.Before, number: query.Number,
	}, nil
}

func (listing Listing[A]) sortNamed(name string) (listingSort, error) {
	if name == "" {
		return listing.sorts[0], nil
	}
	offered := make([]string, 0, len(listing.sorts))
	for _, candidate := range listing.sorts {
		if candidate.name == name {
			return candidate, nil
		}
		offered = append(offered, candidate.name)
	}
	sort.Strings(offered)
	return listingSort{}, fmt.Errorf("%w: no sort is called %q; there are %s",
		ErrPageQuery, name, strings.Join(offered, ", "))
}
