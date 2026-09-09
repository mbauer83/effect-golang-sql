package sql

// The shapes a dialect's answer takes, so that saying how an operation is
// written is a line rather than a function.
//
// Each of these is a Written, which means each is a value a dialect hands
// back. Text appears in them because this is where text belongs: a dialect is
// the one thing in this module that knows what its server's syntax looks like,
// and everything above it works in operations, terms and criteria.

import (
	"fmt"
	"strings"
)

// Weaving is an answer whose arguments are separated alike: the text before
// them, between each pair, and after them. Variadic, so one answer covers a
// concatenation of two and of five.
func Weaving(before string, between string, after string) Written {
	return func(_ Spelling, applied Applied) []Part {
		parts := []Part{Text(before)}
		for at, argument := range applied.Over {
			if at > 0 {
				parts = append(parts, Text(between))
			}
			parts = append(parts, argument...)
		}
		return append(parts, Text(after))
	}
}

// Calling is the commonest answer: the operation is a function of its
// arguments.
func Calling(name string) Written { return Weaving(name+"(", ", ", ")") }

// Between is the other common one: the operation is an operator between them,
// bracketed, because an operator inside another one is what precedence
// decides.
func Between(operator string) Written { return Weaving("(", operator, ")") }

// Relating is a comparison, which is Between without the brackets: a criterion
// is bracketed by whatever composes it with another criterion, so bracketing
// it here would bracket it twice.
func Relating(operator string) Written { return Weaving("", operator, "") }

// Phrased is an answer whose arguments sit in a phrase, each separated by its
// own words -- substring(x from 2 for 3), and everything else a dialect spells
// as syntax rather than as a call.
//
// The pieces are one more than the arguments. Given the wrong number it
// refuses, because a phrase that did not fit its arguments would compose
// something almost right.
func Phrased(pieces ...string) Written {
	return func(_ Spelling, applied Applied) []Part {
		if len(pieces) != len(applied.Over)+1 {
			return []Part{Refused(fmt.Errorf(
				"sql: %s is written as a phrase of %d pieces and was given %d arguments",
				applied.Operation.Named(), len(pieces), len(applied.Over)))}
		}
		parts := []Part{Text(pieces[0])}
		for at, argument := range applied.Over {
			parts = append(parts, argument...)
			parts = append(parts, Text(pieces[at+1]))
		}
		return parts
	}
}

// Detailed is a phrase that needs the use's own detail written as this
// dialect's literal -- a separator, a format, a unit that is a string. Every
// occurrence of a per-cent s in a piece becomes it.
func Detailed(pieces ...string) Written {
	return func(spelling Spelling, applied Applied) []Part {
		said := make([]string, 0, len(pieces))
		for _, piece := range pieces {
			said = append(said, strings.ReplaceAll(piece, "%s", spelling.Text(applied.Detail)))
		}
		return Phrased(said...)(spelling, applied)
	}
}

// Flipped is an answer that reads its arguments the other way round, which is
// what a dialect needs when it takes a difference as "from, to" and the
// operation is stated as "this less that".
func Flipped(written Written) Written {
	return func(spelling Spelling, applied Applied) []Part {
		reversed := make([][]Part, 0, len(applied.Over))
		for at := len(applied.Over) - 1; at >= 0; at-- {
			reversed = append(reversed, applied.Over[at])
		}
		applied.Over = reversed
		return written(spelling, applied)
	}
}

// Leading is an answer whose first argument stands apart from the rest, which
// is the shape of membership and of anything else that asks one thing about a
// list of others.
func Leading(between string, before string, separator string, after string) Written {
	return func(_ Spelling, applied Applied) []Part {
		if len(applied.Over) == 0 {
			return []Part{Refused(fmt.Errorf("sql: %s was given nothing to be about",
				applied.Operation.Named()))}
		}
		parts := append([]Part{}, applied.Over[0]...)
		parts = append(parts, Text(between), Text(before))
		for at, argument := range applied.Over[1:] {
			if at > 0 {
				parts = append(parts, Text(separator))
			}
			parts = append(parts, argument...)
		}
		return append(parts, Text(after))
	}
}
