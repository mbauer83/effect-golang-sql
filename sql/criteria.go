package sql

// Which rows a statement is about.
//
// A criterion is an expression of truth, so everything an expression can do it
// can do: it is built by comparing two expressions of one type, it is joined
// to another criterion by an operation, and a described boolean column is
// already one. What that buys is the check a server would otherwise make at
// the first request -- text compared to a moment, a criterion joined to a
// column of numbers -- made by the compiler instead.
//
// The zero value excludes nothing, so a query with nothing to say about which
// rows it is about says nothing.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// True is every row, which is also the zero value of a criterion.
func True() Criterion { return Criterion{} }

// False is no row at all, in a spelling all three dialects accept.
//
// What an empty set of alternatives means, and it is written rather than
// refused because a caller whose filter turned out empty asked a question with
// an empty answer -- not a question with no answer.
func False() Criterion { return Criterion{node: node{kind: noRowAtAll}} }

const noRows = "1 = 0"

// The six comparisons, each between two expressions of the same type.
//
// Six constructors rather than one taking an operator, because an operator is
// a value somebody can get from a string and these cannot: the type of each
// side has to agree, which is what the shared parameter says, and there is no
// way to ask for a comparison a dialect does not have.
func Equal[A any](left Expr[A], right Expr[A]) Criterion {
	return compare(EqualTo, left, right)
}

func NotEqual[A any](left Expr[A], right Expr[A]) Criterion {
	return compare(UnequalTo, left, right)
}

func Below[A any](left Expr[A], right Expr[A]) Criterion {
	return compare(LessThan, left, right)
}

func AtMost[A any](left Expr[A], right Expr[A]) Criterion {
	return compare(NoMoreThan, left, right)
}

func Above[A any](left Expr[A], right Expr[A]) Criterion {
	return compare(GreaterThan, left, right)
}

func AtLeast[A any](left Expr[A], right Expr[A]) Criterion {
	return compare(NoLessThan, left, right)
}

func compare[A any](operation Operation, left Expr[A], right Expr[A]) Criterion {
	return Apply[bool](operation, left.Term(), right.Term())
}

// ColumnEquals is the rows whose column holds that value.
//
// The one shorthand, because it is the criterion nearly every store asks: a
// row by the identity it is kept under. The column's type is the value's, so
// it is still checked -- against the value rather than against a description,
// which is what a caller who names a column rather than asking a source can be
// given.
func ColumnEquals[A any](column string, value A) Criterion {
	return Equal(Column[A](column), Param(value))
}

// In is the rows whose expression holds any of those values.
//
// None of them is no rows, and it is spelled as a criterion nothing satisfies
// rather than as an empty list -- which is not a statement any of the three
// dialects accept.
func In[A any](of Expr[A], values ...Expr[A]) Criterion {
	if len(values) == 0 {
		return False()
	}
	return Apply[bool](OneOf, append([]Term{of.Term()}, Terms(values...)...)...)
}

// InValues is In over Go values, which is how a caller with a list of
// identities asks.
func InValues[A any](of Expr[A], values ...A) Criterion {
	params := make([]Expr[A], 0, len(values))
	for _, value := range values {
		params = append(params, Param(value))
	}
	return In(of, params...)
}

// IsNotNull is the rows whose expression holds something, and IsNull the rows
// where it holds nothing.
func IsNotNull[A any](of Expr[A]) Criterion { return Apply[bool](SomeValue, of.Term()) }
func IsNull[A any](of Expr[A]) Criterion    { return Apply[bool](NoValue, of.Term()) }

// Like is the rows whose text matches a wildcard pattern -- per cent for
// any run of characters, underscore for one.
//
// Whether it is case-sensitive is the server's and the column's collation, not
// this module's, and a program that depends on the answer should lower both
// sides itself.
func Like(of Expr[string], pattern Expr[string]) Criterion {
	return Apply[bool](PatternMatch, of.Term(), pattern.Term())
}

// Regexp is the rows whose text matches a regular expression.
//
// Not every server has one -- SQLite has none without an extension -- so this
// is the ordinary case of an affordance being refused rather than composed: a
// dialect that cannot say it says nothing, and the statement carries which
// dialect could not do what.
func Regexp(of Expr[string], pattern Expr[string]) Criterion {
	return Apply[bool](ExpressionMatch, of.Term(), pattern.Term())
}

// Beyond is the rows whose expression is further along than that value, in the
// direction the ordering reads.
//
// Stated as an ordering rather than as a greater-than, because which way round
// "further along" goes is the same fact as which way round the list is sorted:
// a descending list continues below its last row and an ascending one above
// it, and a caller writing the comparison separately from the sort is a caller
// who can write one of them backwards.
func Beyond(order Ordering, value dynamic.Value) Criterion {
	operation := GreaterThan
	if order.descending {
		operation = LessThan
	}
	if isNull(value) && order.nulls == nullsUnstated {
		return Refuse[bool](errNullsUnstated)
	}
	compared := Apply[bool](operation,
		Term{node: order.term},
		Term{node: node{kind: aValue, value: value}})
	if order.nulls == nullsUnstated {
		return compared
	}
	return beyondNulls(order, value, compared)
}

// After is the rows that come after a position, in a given order.
//
// This is a cursor, and the reason it is built here rather than assembled by a
// caller is that the assembly is the part people get wrong: an order over two
// expressions needs a lexicographic criterion over both, and the comparison on
// the second only applies where the first is equal. Written by hand it either
// skips the rows that tie at a page boundary or repeats them, and it does so
// only when there is a tie -- so a test over distinct fixtures says nothing
// about it.
//
// The position is read against the leading orderings, so fewer values than
// orderings is a position in the expressions it does give. Values with no
// ordering to compare them to are ignored, because there is nothing to compare
// them to.
func After(order []Ordering, at []dynamic.Value) Criterion {
	positions := min(len(at), len(order))
	if positions == 0 {
		return True()
	}
	members := make([]Criterion, 0, positions)
	for depth := range positions {
		criteria := make([]Criterion, 0, depth+1)
		for earlier := range depth {
			criteria = append(criteria, atPosition(order[earlier], at[earlier]))
		}
		criteria = append(criteria, Beyond(order[depth], at[depth]))
		members = append(members, And(criteria...))
	}
	return Or(members...)
}

// And is the rows every one of those is about, and every row when there are
// none of them.
//
// Variadic rather than a pair, because a query's criteria arrive as a list and
// nesting a pair constructor to say four of them is arithmetic on brackets.
func And(criteria ...Criterion) Criterion {
	return junction(Conjunction, criteria)
}

// Or is the rows any one of those is about, and no rows when there are
// none of them -- the same reading In gives an empty set.
func Or(criteria ...Criterion) Criterion {
	return junction(Disjunction, criteria)
}

// Not is the rows a criterion is not about.
func Not(criterion Criterion) Criterion {
	if criterion.IsEmpty() {
		return False()
	}
	return Apply[bool](Negation, criterion.Term())
}
