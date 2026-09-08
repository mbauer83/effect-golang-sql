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
	"time"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/library"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/migrate"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/experimental/direct"
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
		return direct.Run(func(bind *direct.Binder[effect.Unit, sql.Fault]) []library.Book {
			database := direct.Bind(bind, sql.Open[effect.Unit](scope, "sqlite", source))
			direct.Bind(bind, library.Create(database))
			direct.Bind(bind, library.Restock(database,
				library.Book{Title: "Zionomicon", Author: "De Goes", Pages: 632},
				library.Book{Title: "Short", Author: "A", Pages: 90}))
			return direct.Bind(bind, effect.RunCollect(library.All(database)))
		})
	})

	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	held, ok := exit.Value()
	if !ok {
		fail(fmt.Errorf("library: %v", exit))
	}
	fmt.Printf("library: %d books, shortest first\n", len(held))
	for _, book := range held {
		fmt.Printf("  %-12s %-10s %d pages\n", book.Title, book.Author, book.Pages)
	}
}

// runMigrating applies the pallet's history to an empty database, twice -- the
// second time doing nothing, which is the property a migrator has to have.
func runMigrating(runtime *effect.Runtime, workspace string) {
	source := "file:" + filepath.Join(workspace, "migrating.db")
	plan := migrate.Plan{
		Dialect: ddl.SQLite,
		History: warehouse.Pallets,
		Target:  "3.0.0",
	}

	program := effect.Scoped(func(scope effect.Scope) moving[[2]migrate.Report] {
		return direct.Run(func(bind *direct.Binder[effect.Unit, migrate.Fault]) [2]migrate.Report {
			database := direct.Bind(bind, opened(scope, source))
			first := direct.Bind(bind, migrate.Apply[effect.Unit](database, plan))
			second := direct.Bind(bind, migrate.Apply[effect.Unit](database, plan))
			return [2]migrate.Report{first, second}
		})
	})

	within, giveUp := context.WithTimeout(context.Background(), 20*time.Second)
	defer giveUp()

	exit := runtime.Run(within, effect.Unit{}, program)
	reports, ok := exit.Value()
	if !ok {
		fail(fmt.Errorf("migrating: %v", exit))
	}
	fmt.Printf("\nmigrating: created=%v to %s, applied %v\n",
		reports[0].Created, reports[0].To, reports[0].Applied)
	fmt.Printf("  again: nothing to do = %v\n", reports[1].Nothing())
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
