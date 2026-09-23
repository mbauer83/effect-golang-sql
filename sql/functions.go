package sql

// The operations this module names, as expressions.
//
// One line each, because each is Apply with the operation and the answered
// type filled in -- which is the point: a caller who uses these states
// neither, and a caller who needs one nobody named here uses Apply and
// states both.
//
// The types are what makes them worth having. Length answers a whole number
// whatever it is given, Seconds takes two moments and answers a number,
// Concat takes text and answers text -- so a query that adds a moment to
// a title does not compile, rather than composing something a server evaluates
// by coercing one side.

import "time"

// Count is how many rows there are, which is count(*) and not a count of any
// column: a count of a column does not count the rows where it is null, and
// the two are asked by different questions.
func Count() Expr[int64] { return Apply[int64](RowCount) }

// CountOf is how many rows have a value in that expression.
func CountOf[A any](of Expr[A]) Expr[int64] {
	return Apply[int64](ValueCount, of.Term())
}

// Max and Min are the greatest and least value a group holds, which
// is of the same type as the values.
func Max[A any](of Expr[A]) Expr[A] { return Apply[A](Maximum, of.Term()) }
func Min[A any](of Expr[A]) Expr[A] { return Apply[A](Minimum, of.Term()) }

// Sum is the sum of a group's values, of the same type as the values.
//
// Which operation that is depends on the values: a total of whole numbers is
// asked for differently, because two of the three servers answer it with a
// decimal and would otherwise hand back something a whole number cannot be
// decoded from. The choice is made here rather than by a caller, since the
// caller has already said the type.
func Sum[A any](of Expr[A]) Expr[A] {
	if kindOf[A]() == OfWhole {
		return Apply[A](WholeTotal, of.Term())
	}
	return Apply[A](Summation, of.Term())
}

// Avg is the average of a group's values, which is a number even where the
// values are whole ones, because the average of two whole numbers is not one.
func Avg[A any](of Expr[A]) Expr[float64] {
	return Apply[float64](Average, of.Term())
}

// StringAgg is a group's values as one string, with that separator between them.
//
// The separator is said rather than bound, because two of the three dialects
// spell it as a keyword's argument and no server binds a keyword. So the
// dialect writes it, in the dialect's own quoting.
func StringAgg(of Expr[string], separator string) Expr[string] {
	return ApplyWithDetail[string](StringAggregation, separator, of.Term())
}

// Concat is several pieces of text as one.
func Concat(pieces ...Expr[string]) Expr[string] {
	return Apply[string](Concatenation, Terms(pieces...)...)
}

// Substring is a part of some text: from a position, counted from one, for
// that many characters.
func Substring(of Expr[string], from Expr[int64], count Expr[int64]) Expr[string] {
	return Apply[string](SubstringOf, of.Term(), from.Term(), count.Term())
}

// Lower, Upper and Trim are text with its case changed or its ends
// removed.
func Lower(of Expr[string]) Expr[string] { return Apply[string](LowerCase, of.Term()) }
func Upper(of Expr[string]) Expr[string] { return Apply[string](UpperCase, of.Term()) }
func Trim(of Expr[string]) Expr[string]  { return Apply[string](WhitespaceTrim, of.Term()) }

// Length is how many characters some text has -- characters and not bytes,
// which is why one of the three dialects answers about it itself.
func Length(of Expr[string]) Expr[int64] {
	return Apply[int64](CharacterCount, of.Term())
}

// Coalesce is the first of those that has a value, which is how a nullable
// column becomes something a caller can order or compare by.
func Coalesce[A any](alternatives ...Expr[A]) Expr[A] {
	return Apply[A](Coalescence, Terms(alternatives...)...)
}

// The four arithmetic operations, over expressions of one numeric type.
func Plus[A any](left Expr[A], right Expr[A]) Expr[A] {
	return Apply[A](Addition, left.Term(), right.Term())
}

func Minus[A any](left Expr[A], right Expr[A]) Expr[A] {
	return Apply[A](Subtraction, left.Term(), right.Term())
}

func Times[A any](left Expr[A], right Expr[A]) Expr[A] {
	return Apply[A](Multiplication, left.Term(), right.Term())
}

func Div[A any](left Expr[A], right Expr[A]) Expr[A] {
	return Apply[A](Division, left.Term(), right.Term())
}

// Seconds is how many seconds later one moment is than another.
//
// Seconds and only seconds, because a difference in days is a whole number on
// one server and a fraction on another: a caller who wants days divides this
// and knows which it got.
func Seconds(later Expr[time.Time], earlier Expr[time.Time]) Expr[float64] {
	return Apply[float64](SecondsBetween, later.Term(), earlier.Term())
}
