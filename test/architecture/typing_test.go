package architecture

// Nothing here holds a value it cannot name -- with one exception, on the
// record and checked to stay where it says it is.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// untypedBoundaries are the files where a top type is allowed.
//
// One, and it is a real boundary rather than a shortcut: database/sql scans
// into a top type and a driver hands one back, because a driver cannot know
// what a column holds until it reads it. Everything above that line works in
// the universal representation, which has a case for each of the seven kinds a
// driver may produce.
var untypedBoundaries = map[string]string{
	"sql/driver_values.go": "a driver's values",
}

func TestNoDescriptionEscapesIntoATopType(t *testing.T) {
	typeParameters := regexp.MustCompile(`\[[\w,\s]*any[\w,\s]*\]`)
	topType := regexp.MustCompile(`\binterface\{\}|\bany\b`)

	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") || exempt(path) {
			return nil
		}
		for number, line := range strings.Split(readSource(t, path), "\n") {
			code, _, _ := strings.Cut(line, "//")
			code = typeParameters.ReplaceAllString(code, "")
			if topType.MatchString(code) {
				t.Errorf("%s:%d uses a top type: %s",
					display(t, path), number+1, strings.TrimSpace(line))
			}
		}
		return nil
	}
	if err := filepath.WalkDir(moduleRoot(t), walk); err != nil {
		t.Fatal(err)
	}
}

func exempt(path string) bool {
	for boundary := range untypedBoundaries {
		if strings.HasSuffix(filepath.ToSlash(path), boundary) {
			return true
		}
	}
	return false
}

// The exemption has to stay one file, and it has to stay used. A boundary that
// moved would take its exemption with it silently; one no longer needed would
// leave a licence nobody was exercising.
func TestEveryUntypedBoundaryIsWhereItSaysItIs(t *testing.T) {
	for boundary, subject := range untypedBoundaries {
		source, err := os.ReadFile(filepath.Join(moduleRoot(t), boundary))
		if err != nil {
			t.Errorf("%s is exempt from the top-type ban, for %s, and does not exist: %v",
				boundary, subject, err)
			continue
		}
		if !strings.Contains(string(source), " any)") && !strings.Contains(string(source), "]any") {
			t.Errorf("%s is exempt from the top-type ban, for %s, and does not use one",
				boundary, subject)
		}
	}
}
