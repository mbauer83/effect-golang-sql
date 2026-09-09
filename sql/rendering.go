package sql

// How an expression becomes pieces of a statement.
//
// One function per shape and nothing per operation: an operation is written by
// the dialect, so this walks the tree and asks. Pieces rather than text,
// because a bound value's ordinal is decided by the Compose these end up in,
// and an expression that counted them would be an expression that could only
// be used once.

// Selection is one expression of what a reading answers with, under the name
// it answers to.
type Selection struct {
	term  node
	alias string
	// kind is what the expression answered, kept so that a reading read as a
	// source -- a named expression, a derived table -- can check the
	// expressions taken from it the way a described table does.
	kind Kind
}

// Selected is several plain columns, which is what a store reading its own
// table asks for: the projection's column list, in the order it decodes them.
func Selected(names ...string) []Selection {
	chosen := make([]Selection, 0, len(names))
	for _, name := range names {
		chosen = append(chosen, Selection{term: node{kind: aColumn, name: name}})
	}
	return chosen
}

// Selecting is several erased expressions, each answering to whatever it is
// called -- which for a plain column is the column, and for anything else is
// nothing until it is Named.
func Selecting(terms ...Term) []Selection {
	chosen := make([]Selection, 0, len(terms))
	for _, held := range terms {
		chosen = append(chosen, Selection{term: held.held, kind: held.kind})
	}
	return chosen
}

// Holds is what this selection answers, which is what a reading read as a
// source says about the column.
func (chosen Selection) Holds() Kind { return chosen.kind }

// Named is what a selection answers to, which is its alias or, for a plain
// column, the column's own name.
func (chosen Selection) Named() string {
	if chosen.alias != "" {
		return chosen.alias
	}
	return chosen.term.name
}

// Ordering is one expression and the direction rows are read in.
type Ordering struct {
	term       node
	descending bool
}

// Window is which rows an expression is read over: the groups the rows are
// divided into, and the order within each.
//
// Two lists and nothing else for now. A frame -- how many rows either side of
// this one -- is a third field when it is wanted, not a different shape.
type Window struct {
	Partitioned []Term
	Ordered     []Ordering
}

func (window Window) refused() error {
	why := make([]error, 0, len(window.Partitioned)+len(window.Ordered))
	for _, held := range window.Partitioned {
		why = append(why, held.held.refused)
	}
	for _, one := range window.Ordered {
		why = append(why, one.term.refused)
	}
	return errorsIn(why...)
}

func (window Window) parts(spelling Spelling) []Part {
	parts := []Part{Text("(")}
	if len(window.Partitioned) > 0 {
		parts = append(parts, Text("partition by "))
		parts = append(parts, listed(spelling, nodesOf(window.Partitioned))...)
	}
	if len(window.Ordered) > 0 {
		if len(window.Partitioned) > 0 {
			parts = append(parts, Text(" "))
		}
		parts = append(parts, Text("order by "))
		parts = append(parts, ordering(spelling, window.Ordered)...)
	}
	return append(parts, Text(")"))
}

func (held node) parts(spelling Spelling) []Part {
	switch held.kind {
	case unsaid:
		return nil
	case aColumn:
		return []Part{Text(held.qualified(spelling))}
	case aValue:
		return []Part{Bind(held.value)}
	case anAnswer:
		return append(append([]Part{Text("(")},
			held.answers.selection(spelling)...), Text(")"))
	case aRefusal:
		return []Part{Refused(held.refused)}
	case noRowAtAll:
		return []Part{Text(nothing)}
	case aWindowed:
		return applying(spelling, Applied{
			Operation: OverWindow,
			Over: [][]Part{
				held.over[0].parts(spelling),
				held.window.parts(spelling),
			},
		})
	default:
		over := make([][]Part, 0, len(held.over))
		for _, argument := range held.over {
			over = append(over, argument.parts(spelling))
		}
		return applying(spelling, Applied{
			Operation: held.operation,
			Detail:    held.detail,
			Over:      over,
		})
	}
}

// qualified is the column, prefixed by the source that holds it when the query
// says which.
func (held node) qualified(spelling Spelling) string {
	if held.source == "" {
		return spelling.Quoted(held.name)
	}
	return spelling.Quoted(held.source) + "." + spelling.Quoted(held.name)
}

func (chosen Selection) parts(spelling Spelling) []Part {
	parts := chosen.term.parts(spelling)
	if chosen.alias == "" || chosen.renames() {
		return parts
	}
	return append(parts, Text(" as "+spelling.Quoted(chosen.alias)))
}

// renames reports whether the alias says nothing: a plain column of the one
// source a reading has, named what it is already called.
//
// Worth leaving out because a store naming its own columns names all of them,
// and a select list of "film_id" as "film_id" repeated seven times is a
// statement nobody can read for the one column that is not like that.
func (chosen Selection) renames() bool {
	return chosen.term.kind == aColumn &&
		chosen.term.source == "" &&
		chosen.term.name == chosen.alias
}

// listed is several expressions, comma separated.
func listed(spelling Spelling, held []node) []Part {
	parts := make([]Part, 0, len(held)*2)
	for at, one := range held {
		if at > 0 {
			parts = append(parts, Text(", "))
		}
		parts = append(parts, one.parts(spelling)...)
	}
	return parts
}

// ordering is several orderings, comma separated, each with its direction.
func ordering(spelling Spelling, orderings []Ordering) []Part {
	parts := make([]Part, 0, len(orderings)*3)
	for at, one := range orderings {
		if at > 0 {
			parts = append(parts, Text(", "))
		}
		parts = append(parts, one.term.parts(spelling)...)
		if one.descending {
			parts = append(parts, Text(" desc"))
			continue
		}
		parts = append(parts, Text(" asc"))
	}
	return parts
}

func nodesOf(terms []Term) []node {
	held := make([]node, 0, len(terms))
	for _, one := range terms {
		held = append(held, one.held)
	}
	return held
}

func refusalsIn(terms []Term) []error {
	why := make([]error, 0, len(terms))
	for _, one := range terms {
		why = append(why, one.held.refused)
	}
	return why
}
