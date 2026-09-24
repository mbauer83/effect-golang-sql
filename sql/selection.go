package sql

// How an expression becomes pieces of a statement.
//
// One function per shape and nothing per operation: an operation is written by
// the dialect, so this walks the tree and asks. Pieces rather than text,
// because a bound value's ordinal is decided by the Compose these end up in,
// and an expression that counted them would be an expression that could only
// be used once.

// Selection is one expression of what a query answers with, under the name
// it answers to.
type Selection struct {
	term  node
	alias string
	// kind is what the expression answered, kept so that a query read as a
	// source -- a common table expression, a derived table -- can check the
	// expressions taken from it the way a described table does.
	kind Kind
}

// SelectColumns is several plain columns, which is what a store reading its own
// table asks for: the projection's column list, in the order it decodes them.
func SelectColumns(names ...string) []Selection {
	selections := make([]Selection, 0, len(names))
	for _, name := range names {
		selections = append(selections, Selection{term: node{kind: aColumn, name: name}})
	}
	return selections
}

// SelectTerms is several erased expressions, each answering to whatever it is
// called -- which for a plain column is the column, and for anything else is
// nothing until As names it.
func SelectTerms(terms ...Term) []Selection {
	selections := make([]Selection, 0, len(terms))
	for _, term := range terms {
		selections = append(selections, Selection{term: term.node, kind: term.kind})
	}
	return selections
}

// Kind is what this selection answers, which is what a query read as a
// source says about the column.
func (selection Selection) Kind() Kind { return selection.kind }

// Name is what a selection answers to, which is its alias or, for a plain
// column, the column's own name.
func (selection Selection) Name() string {
	if selection.alias != "" {
		return selection.alias
	}
	return selection.term.name
}

// Ordering is one expression and the direction rows are read in.
type Ordering struct {
	term       node
	descending bool
	nulls      nullPlacement
}

// Window is which rows an expression is read over: the groups the rows are
// divided into, and the order within each.
//
// Two lists and nothing else for now. A frame -- how many rows either side of
// this one -- is a third field when it is wanted, not a different shape.
type Window struct {
	PartitionBy []Term
	OrderBy     []Ordering
}

func (window Window) refusal() error {
	why := make([]error, 0, len(window.PartitionBy)+len(window.OrderBy))
	for _, term := range window.PartitionBy {
		why = append(why, term.node.err)
	}
	for _, one := range window.OrderBy {
		why = append(why, one.term.err)
	}
	return errorsIn(why...)
}

func (window Window) parts(spelling Spelling) []Part {
	parts := []Part{Text("(")}
	if len(window.PartitionBy) > 0 {
		parts = append(parts, Text("PARTITION BY "))
		parts = append(parts, commaList(spelling, nodesOf(window.PartitionBy))...)
	}
	if len(window.OrderBy) > 0 {
		if len(window.PartitionBy) > 0 {
			parts = append(parts, Text(" "))
		}
		parts = append(parts, Text("ORDER BY "))
		parts = append(parts, orderParts(spelling, window.OrderBy)...)
	}
	return append(parts, Text(")"))
}

func (expr node) parts(spelling Spelling) []Part {
	switch expr.kind {
	case noExpression:
		return nil
	case aColumn:
		return []Part{Text(expr.qualifiedName(spelling))}
	case aValue:
		return []Part{Bind(expr.value)}
	case aSubquery:
		return append(append([]Part{Text("(")},
			expr.subquery.parts(spelling)...), Text(")"))
	case aRefusal:
		return []Part{Refusal(expr.err)}
	case noRowAtAll:
		return []Part{Text(noRows)}
	case aWindow:
		return renderApplication(spelling, Application{
			Operation: OverWindow,
			Arguments: [][]Part{
				expr.arguments[0].parts(spelling),
				expr.window.parts(spelling),
			},
		})
	default:
		over := make([][]Part, 0, len(expr.arguments))
		for _, argument := range expr.arguments {
			over = append(over, argument.parts(spelling))
		}
		return renderApplication(spelling, Application{
			Operation: expr.operation,
			Detail:    expr.detail,
			Arguments: over,
		})
	}
}

// qualifiedName is the column, prefixed by the source that holds it when the query
// says which.
func (expr node) qualifiedName(spelling Spelling) string {
	if expr.source == "" {
		return spelling.QuoteIdentifier(expr.name)
	}
	return spelling.QuoteIdentifier(expr.source) + "." + spelling.QuoteIdentifier(expr.name)
}

func (selection Selection) parts(spelling Spelling) []Part {
	parts := selection.term.parts(spelling)
	if selection.alias == "" || selection.isRedundantAlias() {
		return parts
	}
	return append(parts, Text(" AS "+spelling.QuoteIdentifier(selection.alias)))
}

// isRedundantAlias reports whether the alias says nothing: a plain column of the one
// source a query has, named what it is already called.
//
// Worth leaving out because a store naming its own columns names all of them,
// and a select list of "film_id" as "film_id" repeated seven times is a
// statement nobody can read for the one column that is not like that.
func (selection Selection) isRedundantAlias() bool {
	return selection.term.kind == aColumn &&
		selection.term.source == "" &&
		selection.term.name == selection.alias
}

// commaList is several expressions, comma separated.
func commaList(spelling Spelling, nodes []node) []Part {
	parts := make([]Part, 0, len(nodes)*2)
	for at, one := range nodes {
		if at > 0 {
			parts = append(parts, Text(", "))
		}
		parts = append(parts, one.parts(spelling)...)
	}
	return parts
}

// orderParts is several orderings, comma separated, each with its direction.
func orderParts(spelling Spelling, orderings []Ordering) []Part {
	parts := make([]Part, 0, len(orderings)*3)
	for at, one := range orderings {
		if at > 0 {
			parts = append(parts, Text(", "))
		}
		parts = append(parts, orderingParts(spelling, one)...)
	}
	return parts
}

func nodesOf(terms []Term) []node {
	nodes := make([]node, 0, len(terms))
	for _, one := range terms {
		nodes = append(nodes, one.node)
	}
	return nodes
}

func refusalsIn(terms []Term) []error {
	why := make([]error, 0, len(terms))
	for _, one := range terms {
		why = append(why, one.node.err)
	}
	return why
}
