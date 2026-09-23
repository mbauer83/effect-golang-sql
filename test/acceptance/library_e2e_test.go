package acceptance

// The library against a real database.
//
// sqlite is the driver here because it is pure Go and needs no server, so this
// runs everywhere the tests run. The library itself never sees it: it depends
// on the port, which is the whole point of there being one.

import (
	"context"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/library"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type libraryEffect[A any] = effect.Effect[effect.Unit, sql.Fault, A]

// withLibrary opens a database of its own, makes the table, and runs the work.
//
// One database per test, in a file the test owns, because tests that share a
// database share its state and then fail in an order-dependent way.
func withLibrary[A any](t *testing.T, work func(*sql.Database) libraryEffect[A]) effect.Exit[sql.Fault, A] {
	t.Helper()
	runtime, err := effect.NewRuntime(effect.WithDebugTracking())
	if err != nil {
		t.Fatal(err)
	}
	source := "file:" + t.TempDir() + "/library.db"

	program := effect.Scoped(func(scope effect.Scope) libraryEffect[A] {
		return sql.Open[effect.Unit](scope, "sqlite", source).
			FlatMap(func(database *sql.Database) libraryEffect[A] {
				return library.Create(database).
					FlatMap(func(sql.Outcome) libraryEffect[A] { return work(database) })
			})
	})

	// With a deadline: a transaction left open holds a write lock, and a test
	// that hung waiting for one would take the suite with it.
	within, giveUp := context.WithTimeout(context.Background(), 10*time.Second)
	defer giveUp()

	exit := runtime.Run(within, effect.Unit{}, program)
	if live := runtime.LiveWork(); !live.IsEmpty() {
		t.Fatalf("the program left work behind: %#v", live)
	}
	return exit
}

func mustSucceed[A any](t *testing.T, exit effect.Exit[sql.Fault, A]) A {
	t.Helper()
	value, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	return value
}

func TestARowIsDecodedByTheSchemaThatDescribesTheType(t *testing.T) {
	// A row is a set of named values, which is an object -- so the schema that
	// would decode a request body decodes a row, and this package needed no
	// description of its own.
	found := mustSucceed(t, withLibrary(t, func(database *sql.Database) libraryEffect[library.Book] {
		return library.Add(ddl.SQLite, database, library.Book{Title: "Zionomicon", Author: "De Goes", Pages: 632}).
			FlatMap(func(sql.Outcome) libraryEffect[library.Book] {
				return library.ByTitle(ddl.SQLite, database, "Zionomicon")
			})
	}))

	if found != (library.Book{Title: "Zionomicon", Author: "De Goes", Pages: 632}) {
		t.Fatalf("unexpected book: %#v", found)
	}
}

func TestAResultSetIsAStreamAndACallerMayStopEarly(t *testing.T) {
	books := []library.Book{
		{Title: "Long", Author: "A", Pages: 900},
		{Title: "Short", Author: "B", Pages: 90},
		{Title: "Middling", Author: "C", Pages: 400},
	}
	shortest := mustSucceed(t, withLibrary(t, func(database *sql.Database) libraryEffect[[]library.Book] {
		return library.Restock(ddl.SQLite, database, books...).
			FlatMap(func(effect.Unit) libraryEffect[[]library.Book] {
				// Two of three: the cursor is released when the consumer is
				// finished, which is what makes stopping early safe.
				return effect.RunCollect(library.All(ddl.SQLite, database).TakeStream(2))
			})
	}))

	if len(shortest) != 2 || shortest[0].Title != "Short" || shortest[1].Title != "Middling" {
		t.Fatalf("unexpected books: %#v", shortest)
	}
}

func TestAMissingRowIsRefusedRatherThanReturnedEmpty(t *testing.T) {
	exit := withLibrary(t, func(database *sql.Database) libraryEffect[library.Book] {
		return library.ByTitle(ddl.SQLite, database, "Absent")
	})

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected a missing row to be refused, got %+v", exit)
	}
	failures := cause.Failures()
	if len(failures) != 1 || failures[0].Op != "reading one row" {
		t.Fatalf("expected the stage named, got %+v", cause)
	}
	if failures[0].Statement == "" {
		t.Fatal("expected the statement carried: a database fault without it is nearly useless")
	}
}
