package architecture

// The claims about this module's shape, rather than its behaviour.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The port knows nothing about the projections and the projections know nothing
// about the port. Only the migrator knows both, which is what it is for: evolve
// says what changed, ddl says how to spell it, sql runs statements, and none of
// the three reaches into another.
var mayImport = map[string][]string{
	"sql": {},
	// evolve sees the port for one field of one type: a rewriting says how the
	// rows it cannot describe are moved, and that is a function taking the
	// transaction the migration runs in. Saying it as statements instead would
	// mean saying it once per dialect and never being able to compute, so the
	// edge is worth it -- and it points at the port rather than at a driver.
	"evolve":  {"sql"},
	"ddl":     {"evolve", "sql"},
	"migrate": {"ddl", "evolve", "sql"},
}

func TestPackagesDependOnlyInward(t *testing.T) {
	root := moduleRoot(t)
	for pkg, allowed := range mayImport {
		permitted := map[string]bool{}
		for _, held := range allowed {
			permitted[held] = true
		}
		for _, source := range sourcesIn(t, pkg) {
			for _, line := range strings.Split(readSource(t, source), "\n") {
				held, found := ownImport(line)
				if !found || held == pkg || permitted[held] {
					continue
				}
				t.Errorf("%s imports %s, which it may not",
					display(t, source), held)
			}
		}
	}
	_ = root
}

// ownImport is an import of this module, as a package path within it.
func ownImport(line string) (string, bool) {
	const prefix = `"github.com/mbauer83/effect-golang-sql/`
	at := strings.Index(line, prefix)
	if at < 0 {
		return "", false
	}
	rest := line[at+len(prefix):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// TestNothingHereCarriesAThirdPartyDependency, here too.
//
// This module depends on a port and not on a driver, so a program that uses it
// chooses its own driver and acquires nothing else. The three drivers in go.mod
// are test-only: sqlite because it runs everywhere these tests run, and the
// other two because only they can say the statements are ones Postgres and
// MySQL accept.
func TestNothingHereCarriesAThirdPartyDependency(t *testing.T) {
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, relErr := filepath.Rel(moduleRoot(t), path)
		if relErr != nil {
			return relErr
		}
		if top, _, _ := strings.Cut(filepath.ToSlash(relative), "/"); top == "test" || top == "examples" {
			return nil
		}
		for _, line := range strings.Split(readSource(t, path), "\n") {
			if third, found := thirdPartyImport(line); found {
				t.Errorf("%s imports %q, and this module carries no dependencies",
					relative, third)
			}
		}
		return nil
	}
	if err := filepath.WalkDir(moduleRoot(t), walk); err != nil {
		t.Fatal(err)
	}
}

// thirdPartyImport reports an import that is neither the standard library nor
// this project. A standard-library path has no dot before its first slash.
func thirdPartyImport(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	start := strings.Index(trimmed, `"`)
	if start < 0 || !strings.HasSuffix(trimmed, `"`) {
		return "", false
	}
	path := strings.Trim(trimmed[start:], `"`)
	host, _, hasSlash := strings.Cut(path, "/")
	if !hasSlash || !strings.Contains(host, ".") {
		return "", false
	}
	if strings.HasPrefix(path, "github.com/mbauer83/") {
		return "", false
	}
	return path, true
}

// The module root is for project metadata and documentation. Every package is a
// directory, for the reason the runtime's is: a root full of source files is
// not a layout.
func TestModuleRootHoldsNoSource(t *testing.T) {
	entries, err := os.ReadDir(moduleRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			t.Errorf("%s is in the module root", entry.Name())
		}
	}
}

// A file that grows past these has stopped being about one thing. The soft
// limit is a review prompt and the hard one is a failure, so the check is a
// test rather than something a growing file can do by drifting past a review.
const (
	soft = 250
	hard = 350
)

func TestSourceFilesStayWithinTheirLineLimits(t *testing.T) {
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		lines := strings.Count(readSource(t, path), "\n")
		relative := display(t, path)
		switch {
		case lines > hard:
			t.Errorf("%s has %d lines, past the hard limit", relative, lines)
		case lines > soft:
			t.Errorf("%s has %d lines, past the soft limit; split it by domain role",
				relative, lines)
		}
		return nil
	}
	if err := filepath.WalkDir(moduleRoot(t), walk); err != nil {
		t.Fatal(err)
	}
}

// A document nobody can reach is a document nobody reads.
func TestEveryPublicDocumentIsLinkedFromTheReadme(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(moduleRoot(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		relative := filepath.ToSlash(display(t, path))
		if !strings.Contains(string(readme), relative) {
			t.Errorf("%s is not linked from README.md", relative)
		}
		return nil
	}
	if err := filepath.WalkDir(filepath.Join(moduleRoot(t), "docs"), walk); err != nil {
		t.Fatal(err)
	}
}
