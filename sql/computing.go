package sql

// The operations this module names, as expressions.
//
// One line each, because each is Applying with the operation and the answered
// type filled in -- which is the point: a caller who uses these states
// neither, and a caller who needs one nobody named here uses Applying and
// states both.
//
// The types are what makes them worth having. Length answers a whole number
// whatever it is given, Seconds takes two moments and answers a number,
// Concatenated takes text and answers text -- so a query that adds a moment to
// a title does not compile, rather than composing something a server evaluates
// by coercing one side.

import "time"

// Counted is how many rows there are, which is count(*) and not a count of any
// column: a count of a column does not count the rows where it is null, and
// the two are asked by different questions.
func Counted() Expr[int64] { return Applying[int64](RowCount) }

// CountOf is how many rows have a value in that expression.
func CountOf[A any](of Expr[A]) Expr[int64] {
	return Applying[int64](ValueCount, of.Term())
}

// Largest and Smallest are the greatest and least value a group holds, which
// is of the same type as the values.
func Largest[A any](of Expr[A]) Expr[A]  { return Applying[A](Maximum, of.Term()) }
func Smallest[A any](of Expr[A]) Expr[A] { return Applying[A](Minimum, of.Term()) }

// Total is the sum of a group's values, of the same type as the values.
//
// Which operation that is depends on the values: a total of whole numbers is
// asked for differently, because two of the three servers answer it with a
// decimal and would otherwise hand back something a whole number cannot be
// decoded from. The choice is made here rather than by a caller, since the
// caller has already said the type.
func Total[A any](of Expr[A]) Expr[A] {
	if kindOf[A]() == OfWhole {
		return Applying[A](WholeTotal, of.Term())
	}
	return Applying[A](Sum, of.Term())
}

// Mean is the average of a group's values, which is a number even where the
// values are whole ones, because the average of two whole numbers is not one.
func Mean[A any](of Expr[A]) Expr[float64] {
	return Applying[float64](Average, of.Term())
}

// Joined is a group's values as one string, with that separator between them.
//
// The separator is said rather than bound, because two of the three dialects
// spell it as a keyword's argument and no server binds a keyword. So the
// dialect writes it, in the dialect's own quoting.
func Joined(of Expr[string], separator string) Expr[string] {
	return Detailing[string](JoinedValues, separator, of.Term())
}

// Concatenated is several pieces of text as one.
func Concatenated(pieces ...Expr[string]) Expr[string] {
	return Applying[string](Concatenation, Terms(pieces...)...)
}

// Substring is a part of some text: from a position, counted from one, for
// that many characters.
func Substring(of Expr[string], from Expr[int64], count Expr[int64]) Expr[string] {
	return Applying[string](SubstringOf, of.Term(), from.Term(), count.Term())
}

// Lowered, Uppered and Trimmed are text with its case changed or its ends
// removed.
func Lowered(of Expr[string]) Expr[string] { return Applying[string](LowerCase, of.Term()) }
func Uppered(of Expr[string]) Expr[string] { return Applying[string](UpperCase, of.Term()) }
func Trimmed(of Expr[string]) Expr[string] { return Applying[string](Trimming, of.Term()) }

// Length is how many characters some text has -- characters and not bytes,
// which is why one of the three dialects answers about it itself.
func Length(of Expr[string]) Expr[int64] {
	return Applying[int64](CharacterCount, of.Term())
}

// Coalesced is the first of those that has a value, which is how a nullable
// column becomes something a caller can order or compare by.
func Coalesced[A any](alternatives ...Expr[A]) Expr[A] {
	return Applying[A](Coalescence, Terms(alternatives...)...)
}

// The four arithmetic operations, over expressions of one numeric type.
func Plus[A any](left Expr[A], right Expr[A]) Expr[A] {
	return Applying[A](Addition, left.Term(), right.Term())
}

func Minus[A any](left Expr[A], right Expr[A]) Expr[A] {
	return Applying[A](Subtraction, left.Term(), right.Term())
}

func Times[A any](left Expr[A], right Expr[A]) Expr[A] {
	return Applying[A](Multiplication, left.Term(), right.Term())
}

func DividedBy[A any](left Expr[A], right Expr[A]) Expr[A] {
	return Applying[A](Division, left.Term(), right.Term())
}

// Seconds is how many seconds later one moment is than another.
//
// Seconds and only seconds, because a difference in days is a whole number on
// one server and a fraction on another: a caller who wants days divides this
// and knows which it got.
func Seconds(later Expr[time.Time], earlier Expr[time.Time]) Expr[float64] {
	return Applying[float64](SecondsBetween, later.Term(), earlier.Term())
}
