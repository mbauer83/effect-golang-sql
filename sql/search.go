package sql

// Finding a listing's rows by text.
//
// A search is declared on the listing that offers it, under a name a reader
// asks for it by, and each kind is served by the index its dialect has for
// it: a prefix by an ordinary index on the text lowercased, words by the
// dialect's full-text index, a fragment by Postgres's trigram index. A kind a
// dialect has no index for is refused rather than answered by reading every
// row. ddl.CreateSearches makes what each needs; a search asked for is a
// criterion, so it narrows a page, a count or another criterion alike.

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// MatchKind is how a search matches its text.
type MatchKind int

const (
	// PrefixMatch is the rows whose column begins with the text, in any case.
	PrefixMatch MatchKind = iota + 1
	// FullTextMatch is the rows whose columns hold every word of the text, as
	// the dialect's full-text search tells words apart.
	FullTextMatch
	// SubstringMatch is the rows whose column holds the text anywhere, in any
	// case.
	SubstringMatch
)

func (kind MatchKind) String() string {
	switch kind {
	case PrefixMatch:
		return "prefix"
	case FullTextMatch:
		return "full-text"
	case SubstringMatch:
		return "substring"
	default:
		return "unknown"
	}
}

// Search is one way a listing's rows are found by text: its name, how it
// matches, and the columns it searches.
type Search struct {
	Name    string
	Kind    MatchKind
	Columns []string
	// Language is the text search configuration Postgres splits words by;
	// "simple", which only lowercases, unless it says otherwise.
	Language string
}

// NewSearch is a search of that kind over those columns. A prefix or a
// substring is of one column, and full text of one or more.
func NewSearch(name string, kind MatchKind, columns ...string) Search {
	return Search{Name: name, Kind: kind, Columns: columns, Language: "simple"}
}

// WithLanguage is the full-text search splitting words as that Postgres
// configuration does: "english" stems them, so "ships" finds "ship".
func (search Search) WithLanguage(language string) Search {
	search.Language = language
	return search
}

// check is why the search cannot be made, or nil.
func (search Search) check() error {
	switch {
	case search.Name == "":
		return errors.New("sql: a search is named")
	case search.Kind == FullTextMatch && len(search.Columns) == 0:
		return fmt.Errorf("sql: the search %q is of one or more columns", search.Name)
	case search.Kind != FullTextMatch && len(search.Columns) != 1:
		return fmt.Errorf("sql: the %s search %q is of one column", search.Kind, search.Name)
	}
	return nil
}

// SearchSpelling is a dialect that finds rows by text: how it writes a search
// of a table, whose columns a query names through qualifier. It answers false
// for a kind it has no index for.
//
// The text arrives bound, as the arguments of the syntax's application: for a
// prefix, the least and the first past what it admits; for full text, the
// words, lowercased and separated by one space; for a substring, a LIKE
// pattern, escaped with a backslash.
type SearchSpelling interface {
	SearchSyntax(search Search, table string, qualifier string) (Syntax, bool)
}

// WithSearch offers searches of the listing's rows, each under its name.
func (listing Listing[A]) WithSearch(searches ...Search) Listing[A] {
	for _, search := range searches {
		if err := search.check(); err != nil {
			listing.fault = err
		}
	}
	listing.searches = append(append([]Search(nil), listing.searches...), searches...)
	return listing
}

// Searches are the searches the listing offers, for making what they need.
func (listing Listing[A]) Searches() []Search {
	return append([]Search(nil), listing.searches...)
}

// Match is the rows the named search finds for text. Text with nothing to
// search by -- empty, or with no word in it -- is every row, as an empty
// search box is.
func (listing Listing[A]) Match(name string, text string) Criterion {
	var search Search
	found := false
	for _, offered := range listing.searches {
		if offered.Name == name {
			search, found = offered, true
		}
	}
	switch {
	case !found:
		return Refuse[bool](fmt.Errorf("%w: no search is called %q", ErrPageQuery, name))
	case listing.source.table == "":
		return Refuse[bool](fmt.Errorf("sql: the search %q is of a table, and the listing reads a query", name))
	}
	arguments, searchable := searchArguments(search.Kind, text)
	if !searchable {
		return True()
	}
	qualifier := listing.source.alias
	if qualifier == "" {
		qualifier = listing.source.table
	}
	table := listing.source.table
	operation := Declare(search.Kind.String() + " search " + search.Name).
		WithDefault(func(spelling Spelling, application Application) []Part {
			searching, can := searchSpellingOf(spelling)
			if !can {
				return []Part{Refusal(fmt.Errorf("sql: %s cannot search by text", spelling.Name()))}
			}
			syntax, can := searching.SearchSyntax(search, table, qualifier)
			if !can {
				return []Part{Refusal(fmt.Errorf("sql: %s has no index for a %s search, and %q is one",
					spelling.Name(), search.Kind, search.Name))}
			}
			return syntax(spelling, application)
		})
	return Apply[bool](operation, arguments...)
}

// searchArguments are the text as a search of that kind binds it, and whether
// there is anything to search by.
func searchArguments(kind MatchKind, text string) ([]Term, bool) {
	switch kind {
	case PrefixMatch:
		if text == "" {
			return nil, false
		}
		// The highest code point, so every text beginning with this one sorts
		// below it, under the byte order its column is compared in.
		return []Term{Param(text).Term(), Param(text + "\U0010FFFF").Term()}, true
	case FullTextMatch:
		words := strings.FieldsFunc(strings.ToLower(text), func(character rune) bool {
			return !unicode.IsLetter(character) && !unicode.IsDigit(character)
		})
		if len(words) == 0 {
			return nil, false
		}
		return []Term{Param(strings.Join(words, " ")).Term()}, true
	default:
		if text == "" {
			return nil, false
		}
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
		return []Term{Param("%" + escaped + "%").Term()}, true
	}
}

// searchSpellingOf is the dialect's searching, through any Also it is wrapped
// in.
func searchSpellingOf(spelling Spelling) (SearchSpelling, bool) {
	for {
		if searching, can := spelling.(SearchSpelling); can {
			return searching, true
		}
		wrapped, isExtension := spelling.(extension)
		if !isExtension {
			return nil, false
		}
		spelling = wrapped.Spelling
	}
}
