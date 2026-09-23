package architecture

// What this module may not contain.
//
// The bug this exists to prevent happened four times in one package. The
// migrator built its statements with dialect.QuoteIdentifier for every
// identifier and a hardcoded question mark for every value -- the advisory
// lock, the ledger's read, its update and its insert. Postgres refuses all
// four, and nothing said so: the unit tests compared each statement to what its
// author expected it to say, and the end-to-end tests run against SQLite, for
// which a question mark is right.
//
// So the rule is lexical, because that is the shape of the claim: nothing here
// spells a placeholder. A statement says what it binds and the dialect spells
// it. The two dialect files that answer what a placeholder *is* are named,
// because that is where the knowledge belongs.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// spellers are the files allowed to name a placeholder: the dialects whose job
// is to answer what one looks like.
var spellers = map[string]string{
	"ddl/postgres.go": "Postgres numbers them",
	"ddl/mysql.go":    "MySQL does not",
	"ddl/sqlite.go":   "nor does SQLite",
}

func TestNothingHereSpellsAPlaceholder(t *testing.T) {
	root := moduleRoot(t)
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") || allowedToSpell(root, path) {
			return nil
		}
		for _, said := range literalsIn(t, path) {
			if spelled := placeholderIn(said); spelled != "" {
				t.Errorf("%s spells the placeholder %q in %q:\n\t"+
					"say the statement as a shape, or compose it with sql.Bind, "+
					"and let the dialect spell what it binds",
					display(t, path), spelled, said)
			}
		}
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		t.Fatal(err)
	}
}

func TestEveryFileAllowedToSpellOneDoes(t *testing.T) {
	// An exemption nobody uses is an exemption that has outlived its reason,
	// and one that stays is a hole somebody will put something in.
	root := moduleRoot(t)
	for named, why := range spellers {
		spells := false
		for _, said := range literalsIn(t, filepath.Join(root, named)) {
			if said == "?" || strings.HasPrefix(said, "$") {
				spells = true
			}
		}
		if !spells {
			t.Errorf("%s is allowed to spell a placeholder, for %q, and does not",
				named, why)
		}
	}
}

func allowedToSpell(root string, path string) bool {
	within, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	_, named := spellers[filepath.ToSlash(within)]
	return named
}

// placeholderIn is the placeholder a string spells, and nothing when it spells
// none.
//
// Both spellings, because the point is that neither belongs outside a dialect:
// a question mark is what MySQL and SQLite want and a numbered mark is what
// Postgres wants, so a statement carrying either has chosen a server.
func placeholderIn(said string) string {
	if !looksLikeAStatement(said) {
		return ""
	}
	if strings.Contains(said, "?") {
		return "?"
	}
	for ordinal := 1; ordinal <= 9; ordinal++ {
		numbered := "$" + strconv.Itoa(ordinal)
		if strings.Contains(said, numbered) {
			return numbered
		}
	}
	return ""
}

func looksLikeAStatement(said string) bool {
	for _, word := range []string{"select ", "insert ", "update ", "delete ", "where ", "values ", "values("} {
		if strings.Contains(strings.ToLower(said), word) {
			return true
		}
	}
	return false
}

// literalsIn is every string literal a file holds.
func literalsIn(t *testing.T, path string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	held := make([]string, 0, 16)
	ast.Inspect(parsed, func(node ast.Node) bool {
		literal, isLiteral := node.(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			return true
		}
		said, err := strconv.Unquote(literal.Value)
		if err != nil {
			said = literal.Value
		}
		held = append(held, said)
		return true
	})
	return held
}
