package sql

// The tables an aggregate is kept in, as a dialect lays them out.
//
// Which tables an aggregate becomes is DDL's to say -- the same derivation
// makes them -- so a repository asks the dialect, as a search does, rather
// than deriving them a second time and drifting from what was made.

import (
	"fmt"
	"sync"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// TableLayout is one table of an aggregate: its name, its columns, its key,
// the column that keeps a list's order, and, for a table beneath the root,
// where its rows belong.
type TableLayout struct {
	Name     string
	Columns  []ColumnType
	Key      []string
	Position string
	Parent   *ParentLayout
}

// ParentLayout is where a table's rows belong: the holder's table, the columns
// referring to the holder, the holder's key columns they refer to, and the
// member of the holder the rows are -- one entity when Single, references when
// Element names the column holding each.
type ParentLayout struct {
	Table   string
	Columns []string
	Targets []string
	Field   string
	Single  bool
	Element string
}

// LayoutSpelling is a dialect that says which tables it keeps an aggregate
// in, root first and every holder before what it holds.
type LayoutSpelling interface {
	Layout(node structure.Node) ([]TableLayout, error)
}

// layouts remembers an aggregate's tables per dialect, since they are the same
// every time they are asked for.
type layouts struct {
	node  structure.Node
	guard sync.Mutex
	held  map[string][]TableLayout
}

func (known *layouts) of(spelling Spelling) ([]TableLayout, error) {
	known.guard.Lock()
	defer known.guard.Unlock()
	if held, found := known.held[spelling.Name()]; found {
		return held, nil
	}
	laying, can := layoutSpellingOf(spelling)
	if !can {
		return nil, fmt.Errorf("sql: %s cannot say which tables an aggregate is kept in", spelling.Name())
	}
	laid, err := laying.Layout(known.node)
	if err != nil {
		return nil, err
	}
	if known.held == nil {
		known.held = map[string][]TableLayout{}
	}
	known.held[spelling.Name()] = laid
	return laid, nil
}

// layoutSpellingOf is the dialect's layout, through any Also it is wrapped in.
func layoutSpellingOf(spelling Spelling) (LayoutSpelling, bool) {
	for {
		if laying, can := spelling.(LayoutSpelling); can {
			return laying, true
		}
		wrapped, isExtension := spelling.(extension)
		if !isExtension {
			return nil, false
		}
		spelling = wrapped.Spelling
	}
}
