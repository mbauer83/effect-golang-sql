// Command sqldemo runs this module's examples as programs.
//
// It exists so the examples are demonstrably runnable and not only test
// fixtures. Each is also composed by an end-to-end test, so the two cannot
// drift apart. SQLite is the database, because it needs nothing installed.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/library"
	"github.com/mbauer83/effect-golang-sql/migrate"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

func main() {
	workspace, err := os.MkdirTemp("", "sqldemo")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(workspace)

	runtime, err := effect.NewRuntime(effect.WithDebugTracking())
	if err != nil {
		fail(err)
	}
	defer reportShutdown(runtime)

	runLibrary(runtime, workspace)
	runWarehouse()
	runEvolving()
	runMigrating(runtime, workspace)
}

// runLibrary keeps books in a database it never names a driver for.
//
// Direct style, because the sequence is four dependent steps and writing it as
// FlatMaps meant four levels of nesting to say "then". The body holds no defer,
// which is the condition: in direct style a defer runs on an ordinary domain
// failure and not only on a panic.
func runLibrary(runtime *effect.Runtime, workspace string) {
	source := "file:" + filepath.Join(workspace, "library.db")

	program := effect.Scoped(func(scope effect.Scope) shelving[[]library.Book] {
		return effect.Gen(func(do *effect.Do[effect.Unit, sql.Fault]) []library.Book {
			database := do.Await(sql.Open[effect.Unit](scope, "sqlite", source))
			do.Await(library.Create(database))
			// The dialect is given rather than assumed, which is the whole
			// reason this program runs unchanged against another server: the
			// shelf says what it asks and this says which server is asked.
			do.Await(library.Restock(ddl.SQLite, database,
				library.Book{Title: "Zionomicon", Author: "De Goes", Pages: 632},
				library.Book{Title: "Short", Author: "A", Pages: 90}))
			return do.Await(effect.RunCollect(library.All(ddl.SQLite, database)))
		})
	})

	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	value, ok := exit.Value()
	if !ok {
		fail(fmt.Errorf("library: %v", exit))
	}
	fmt.Printf("library: %d books, shortest first\n", len(value))
	for _, book := range value {
		fmt.Printf("  %-12s %-10s %d pages\n", book.Title, book.Author, book.Pages)
	}
}

// The two channels these programs work in, named so a signature says what it
// is rather than repeating itself.
type shelving[A any] = effect.Effect[effect.Unit, sql.Fault, A]
type moving[A any] = effect.Effect[effect.Unit, migrate.Fault, A]

// opened is sql.Open with its fault adapted, which is the one thing the
// migrator's channel needs of the port's.
func opened(scope effect.Scope, source string) moving[*sql.Connected] {
	return sql.Open[effect.Unit](scope, "sqlite", source).
		MapError(func(fault sql.Fault) migrate.Fault {
			return migrate.Fault{Doing: "opening", Err: fault}
		})
}

func reportShutdown(runtime *effect.Runtime) {
	remaining := runtime.LiveWork()
	cleanup := runtime.Close(context.Background())
	fmt.Printf("shutdown: %d fibers and %d resources still owned at Close\n",
		remaining.Fibers, remaining.Resources)
	if !cleanup.IsEmpty() {
		fmt.Printf("shutdown cleanup: %s\n", cleanup)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "sqldemo: %v\n", err)
	os.Exit(1)
}
