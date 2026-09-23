package sql

// What a query computes, and what type it computes.
//
// An expression carries the Go type of the value it stands for, so the
// compiler answers the questions a server would otherwise answer at the first
// request: a criterion cannot compare text to a moment, a substring cannot be
// taken of a number, two criteria cannot be joined with something that is not
// one. Criterion is an expression of truth values, which is what a criterion
// is in SQL -- so a boolean column is a criterion, with nothing said to make
// it one.
//
// The type parameter is a phantom: it constrains what can be built and is gone
// by the time anything is rendered. Underneath is one erased node with a
// closed set of *shapes* -- a column, a bound value, an operation over other
// nodes, a window, another query's answer -- over an open set of operations.
// That division is what lets an affordance be added without a shape being
// added, and a shape be rendered without knowing which affordances exist.
//
// Heterogeneity is erased where Go cannot carry it: a select list, a grouping
// and an order are lists of differently-typed expressions, so they hold the
// erased node. Everything that decides whether a query is well-formed happens
// before that.

import (
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// Expr is a value a query computes, of the Go type it computes.
type Expr[A any] struct {
	node node
}

// Criterion is an expression of truth: which rows a statement is about.
//
// An alias rather than its own type, so that a described boolean column is
// already one and a criterion is already an expression -- which is what it is
// in every server, and pretending otherwise would mean a second vocabulary for
// joining criteria and comparing them.
type Criterion = Expr[bool]

// node is one shape of expression, with its type gone.
type node struct {
	kind      shape
	source    string
	name      string
	value     dynamic.Value
	operation Operation
	detail    string
	arguments []node
	subquery  *SelectQuery
	window    *Window
	err       error
}

type shape int

const (
	// Nothing at all, which is the zero value: an unset criterion excludes no
	// rows and an unset expression is one a query never asks for.
	noExpression shape = iota
	aColumn
	aValue
	anApplication
	aWindow
	aSubquery
	aRefusal
	noRowAtAll
)

// Column is a column of whatever the query is reading, unqualified, read as
// the type the caller says it holds.
//
// Unchecked, because nothing was said about the source: for a query over one
// table whose columns the caller knows. A query built from a description asks
// the source instead, and gets both the name and the type checked.
func Column[A any](name string) Expr[A] {
	return Expr[A]{node: node{kind: aColumn, name: name}}
}

// DynamicParam is a value the statement binds, already in the universal
// representation.
func DynamicParam[A any](value dynamic.Value) Expr[A] {
	return Expr[A]{node: node{kind: aValue, value: value}}
}

// Param is a Go value the statement binds, as an expression of its own type.
//
// The type is the caller's own -- a FilmID, a UserID -- so a comparison
// against a column read as that type is checked by the compiler and a
// comparison against another column's type does not compile. Param and never
// written into the text, which is why there is no expression for a literal:
// the only things this module writes as text are the ones no server will bind,
// and a dialect writes those from an operation's detail.
func Param[A any](a A) Expr[A] {
	value, err := dynamicOf(a)
	if err != nil {
		return Expr[A]{node: node{kind: aRefusal, err: err}}
	}
	return Expr[A]{node: node{kind: aValue, value: value}}
}

// At is a value a page resumes at, which is Param with its type forgotten: a
// cursor's values are of as many types as the order has terms, and Go has no
// list that carries them.
func At[A any](a A) dynamic.Value {
	value, err := dynamicOf(a)
	if err != nil {
		return dynamic.Absent{}
	}
	return value
}

// Apply is an operation over expressions, answering the type the caller
// says it answers.
//
// The general constructor, and how an operation nobody here named is used. The
// named ones carry both the operation and the type, so a caller who uses them
// states neither.
func Apply[A any](operation Operation, over ...Term) Expr[A] {
	return ApplyWithDetail[A](operation, "", over...)
}

// ApplyWithDetail is Apply with what the use has to say rather than bind -- a
// separator, a format, a unit no server would take as a value.
func ApplyWithDetail[A any](operation Operation, detail string, over ...Term) Expr[A] {
	why := make([]error, 0, len(over))
	arguments := make([]node, 0, len(over))
	for _, term := range over {
		why = append(why, term.node.err)
		arguments = append(arguments, term.node)
	}
	return Expr[A]{node: node{
		kind:      anApplication,
		operation: operation,
		detail:    detail,
		arguments: arguments,
		err:       errorsIn(why...),
	}}
}

// Subquery is the one value another query answers with, as an expression of
// this one.
//
// A scalar subquery, and the reason it is here rather than left to a join is
// that two independent counts about one row are two questions: joined, the
// first multiplies the rows the second is counted over, and a group by that
// collapsed them again would be undoing the join it just did.
func Subquery[A any](query SelectQuery) Expr[A] {
	return Expr[A]{node: node{
		kind:     aSubquery,
		subquery: &query,
		err:      query.refusal(),
	}}
}

// Refuse is an expression that says why it could not be made, so that a name
// no source has does not become a statement a server has to reject.
func Refuse[A any](why error) Expr[A] {
	return Expr[A]{node: node{kind: aRefusal, err: why}}
}

// Term is an expression with its type forgotten.
//
// What the places a query holds several expressions of different types take: a
// select list, a grouping, an operation's arguments. Opaque, so the only way
// to have one is to have had a typed expression first -- the erasure is a
// step, not a hole.
type Term struct {
	node node
	kind Kind
}

// Term is this expression, erased -- keeping what kind of value it is, so that
// a source derived from a query still knows what its columns hold.
func (expr Expr[A]) Term() Term { return Term{node: expr.node, kind: kindOf[A]()} }

// Terms is several expressions of one type, erased.
func Terms[A any](exprs ...Expr[A]) []Term {
	terms := make([]Term, 0, len(exprs))
	for _, expr := range exprs {
		terms = append(terms, expr.Term())
	}
	return terms
}

// As is this expression under a name the rest of the query, and whatever
// decodes the row, calls it by.
func (expr Expr[A]) As(alias string) Selection {
	return Selection{term: expr.node, alias: alias, kind: kindOf[A]()}
}

// Ascending and Descending are this expression as an order to read rows in.
func (expr Expr[A]) Ascending() Ordering  { return Ordering{term: expr.node} }
func (expr Expr[A]) Descending() Ordering { return Ordering{term: expr.node, descending: true} }

// Over is this expression read over a window rather than over the whole group.
//
// What an aggregate becomes when the rows it is about are still wanted: a
// running total, a rank within a partition, each row beside its group's
// average. The aggregate collapses the group; the same aggregate over a window
// does not.
func (expr Expr[A]) Over(window Window) Expr[A] {
	return Expr[A]{node: node{
		kind:      aWindow,
		arguments: []node{expr.node},
		window:    &window,
		err:       errorsIn(expr.node.err, window.refusal()),
	}}
}

// Err is why this expression could not be made, and nothing when it could.
func (expr Expr[A]) Err() error { return expr.node.err }
