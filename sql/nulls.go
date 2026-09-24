package sql

// Where an ordering puts its nulls, and how a page continues past them.
//
// The servers disagree: Postgres sorts a null after every value, MySQL and
// SQLite before. An ordering over a column that may be null says which it
// means, and every dialect is made to agree -- Postgres and SQLite say NULLS
// FIRST or NULLS LAST, and MySQL, which cannot, orders by whether the value is
// null first. A position in such an order may be a null, which a cursor holds
// and a keyset compares as the one place at that end.

import (
	"errors"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// nullPlacement is where an ordering puts its nulls: unstated, which is the
// server's choice, or first or last in the order read.
type nullPlacement uint8

const (
	nullsUnstated nullPlacement = iota
	nullsFirst
	nullsLast
)

// NullsFirstOrder and NullsLastOrder are an ordering that says where its nulls
// go. The detail is the direction, ASC or DESC, and the ordinary spelling
// orders by whether the value is null first, which every server can; a
// dialect with NULLS FIRST and NULLS LAST says so instead, so an index on the
// column still serves the order.
var (
	NullsFirstOrder = Declare("order with nulls first").WithDefault(nullsByFlag(true))
	NullsLastOrder  = Declare("order with nulls last").WithDefault(nullsByFlag(false))
)

func nullsByFlag(first bool) Syntax {
	return func(_ Spelling, application Application) []Part {
		flag := " IS NULL) ASC, "
		if first {
			flag = " IS NULL) DESC, "
		}
		term := application.Arguments[0]
		parts := append([]Part{Text("(")}, term...)
		parts = append(parts, Text(flag))
		parts = append(parts, term...)
		return append(parts, Text(" "+application.Detail))
	}
}

// NullsPhrase is how a dialect with the keywords writes an ordering that says
// where its nulls go: the term, its direction, and NULLS FIRST or NULLS LAST.
func NullsPhrase(placement string) Syntax {
	return func(_ Spelling, application Application) []Part {
		return append(append([]Part(nil), application.Arguments[0]...),
			Text(" "+application.Detail+" NULLS "+placement))
	}
}

// NullsFirst is this ordering with nulls before every value, in the order read.
func (ordering Ordering) NullsFirst() Ordering {
	ordering.nulls = nullsFirst
	return ordering
}

// NullsLast is this ordering with nulls after every value, in the order read.
func (ordering Ordering) NullsLast() Ordering {
	ordering.nulls = nullsLast
	return ordering
}

// orderingParts is one ordering as a dialect writes it.
func orderingParts(spelling Spelling, one Ordering) []Part {
	direction := "ASC"
	if one.descending {
		direction = "DESC"
	}
	if one.nulls == nullsUnstated {
		return append(one.term.parts(spelling), Text(" "+direction))
	}
	operation := NullsLastOrder
	if one.nulls == nullsFirst {
		operation = NullsFirstOrder
	}
	return renderApplication(spelling, Application{
		Operation: operation, Detail: direction, Arguments: [][]Part{one.term.parts(spelling)},
	})
}

// errNullsUnstated is a position that is a null in an ordering that does not
// say where nulls go, which each server answers differently.
var errNullsUnstated = errors.New(
	"sql: a page is ordered by a column that may be null; its ordering says NullsFirst or NullsLast")

func isNull(value dynamic.Value) bool {
	_, absent := value.(dynamic.Absent)
	return value == nil || absent
}

// atPosition is the rows whose expression is at the value: equal to it, or
// null with it.
func atPosition(order Ordering, value dynamic.Value) Criterion {
	term := Term{node: order.term}
	if isNull(value) {
		if order.nulls == nullsUnstated {
			return Refuse[bool](errNullsUnstated)
		}
		return Apply[bool](NoValue, term)
	}
	return Apply[bool](EqualTo, term, Term{node: node{kind: aValue, value: value}})
}

// beyondNulls is Beyond for an ordering that says where its nulls go: past a
// value are the greater values, and the nulls when they come last; past a null
// are the values when nulls come first, and nothing when they come last.
func beyondNulls(order Ordering, value dynamic.Value, compared Criterion) Criterion {
	term := Term{node: order.term}
	switch {
	case isNull(value) && order.nulls == nullsLast:
		return False()
	case isNull(value):
		return Apply[bool](SomeValue, term)
	case order.nulls == nullsLast:
		return Or(compared, Apply[bool](NoValue, term))
	default:
		return compared
	}
}
