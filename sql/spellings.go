package sql

// The shapes a dialect's answer takes, so that saying how an operation is
// written is a line rather than a function.
//
// Each of these is a Syntax, which means each is a value a dialect hands
// back. Text appears in them because this is where text belongs: a dialect is
// the one thing in this module that knows what its server's syntax looks like,
// and everything above it works in operations, terms and criteria.

import (
	"fmt"
	"strings"
)

// Weave is an answer whose arguments are separated alike: the text before
// them, between each pair, and after them. Variadic, so one answer covers a
// concatenation of two and of five.
func Weave(before string, between string, after string) Syntax {
	return func(_ Spelling, application Application) []Part {
		parts := []Part{Text(before)}
		for at, argument := range application.Arguments {
			if at > 0 {
				parts = append(parts, Text(between))
			}
			parts = append(parts, argument...)
		}
		return append(parts, Text(after))
	}
}

// Function is the commonest answer: the operation is a function of its
// arguments.
func Function(name string) Syntax { return Weave(name+"(", ", ", ")") }

// Operator is the other common one: the operation is an operator between them,
// bracketed, because an operator inside another one is what precedence
// decides.
func Operator(operator string) Syntax { return Weave("(", operator, ")") }

// Infix is a comparison, which is Between without the brackets: a criterion
// is bracketed by whatever composes it with another criterion, so bracketing
// it here would bracket it twice.
func Infix(operator string) Syntax { return Weave("", operator, "") }

// Phrase is an answer whose arguments sit in a phrase, each separated by its
// own words -- substring(x from 2 for 3), and everything else a dialect spells
// as syntax rather than as a call.
//
// The pieces are one more than the arguments. Given the wrong number it
// refuses, because a phrase that did not fit its arguments would compose
// something almost right.
func Phrase(pieces ...string) Syntax {
	return func(_ Spelling, application Application) []Part {
		if len(pieces) != len(application.Arguments)+1 {
			return []Part{Refusal(fmt.Errorf(
				"sql: %s is written as a phrase of %d pieces and was given %d arguments",
				application.Operation.String(), len(pieces), len(application.Arguments)))}
		}
		parts := []Part{Text(pieces[0])}
		for at, argument := range application.Arguments {
			parts = append(parts, argument...)
			parts = append(parts, Text(pieces[at+1]))
		}
		return parts
	}
}

// DetailPhrase is a phrase that needs the use's own detail written as this
// dialect's literal -- a separator, a format, a unit that is a string. Every
// occurrence of a per-cent s in a piece becomes it.
func DetailPhrase(pieces ...string) Syntax {
	return func(spelling Spelling, application Application) []Part {
		phrase := make([]string, 0, len(pieces))
		for _, piece := range pieces {
			phrase = append(phrase, strings.ReplaceAll(piece, "%s", spelling.QuoteLiteral(application.Detail)))
		}
		return Phrase(phrase...)(spelling, application)
	}
}

// Flip is an answer that reads its arguments the other way round, which is
// what a dialect needs when it takes a difference as "from, to" and the
// operation is stated as "this less that".
func Flip(syntax Syntax) Syntax {
	return func(spelling Spelling, application Application) []Part {
		reversed := make([][]Part, 0, len(application.Arguments))
		for at := len(application.Arguments) - 1; at >= 0; at-- {
			reversed = append(reversed, application.Arguments[at])
		}
		application.Arguments = reversed
		return syntax(spelling, application)
	}
}

// ListOperator is an answer whose first argument stands apart from the rest, which
// is the shape of membership and of anything else that asks one thing about a
// list of others.
func ListOperator(between string, before string, separator string, after string) Syntax {
	return func(_ Spelling, application Application) []Part {
		if len(application.Arguments) == 0 {
			return []Part{Refusal(fmt.Errorf("sql: %s was given nothing to be about",
				application.Operation.String()))}
		}
		parts := append([]Part{}, application.Arguments[0]...)
		parts = append(parts, Text(between), Text(before))
		for at, argument := range application.Arguments[1:] {
			if at > 0 {
				parts = append(parts, Text(separator))
			}
			parts = append(parts, argument...)
		}
		return append(parts, Text(after))
	}
}
