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
func runLibrary(runtime *effect.Runtime, workspace string) {
	source := "file:" + filepath.Join(workspace, "library.db")
	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, sql.Fault, []library.Book] {
		return sql.Open[effect.Unit](scope, "sqlite", source).
			FlatMap(func(database *sql.Connected) effect.Effect[effect.Unit, sql.Fault, []library.Book] {
				return library.Create(database).
					FlatMap(func(sql.Outcome) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
						return library.Restock(database,
							library.Book{Title: "Zionomicon", Author: "De Goes", Pages: 632},
							library.Book{Title: "Short", Author: "A", Pages: 90})
					}).
					FlatMap(func(effect.Unit) effect.Effect[effect.Unit, sql.Fault, []library.Book] {
						return effect.RunCollect(library.All(database))
					})
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

	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, migrate.Fault, [2]migrate.Report] {
		return sql.Open[effect.Unit](scope, "sqlite", source).
			MapError(func(fault sql.Fault) migrate.Fault {
				return migrate.Fault{Doing: "opening", Err: fault}
			}).
			FlatMap(func(database *sql.Connected) effect.Effect[effect.Unit, migrate.Fault, [2]migrate.Report] {
				return migrate.Apply[effect.Unit](database, plan).
					FlatMap(func(first migrate.Report) effect.Effect[effect.Unit, migrate.Fault, [2]migrate.Report] {
						return migrate.Apply[effect.Unit](database, plan).
							Map(func(second migrate.Report) [2]migrate.Report {
								return [2]migrate.Report{first, second}
							})
					})
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
