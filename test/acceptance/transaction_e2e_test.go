package acceptance

// Transactions against a real database. What is checked here is the outcome a
// caller sees; that the release itself rolls back is checked directly in the
// unit suite, because a database's locking is not a reliable witness to it.

import (
	"errors"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/examples/library"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

func TestATransactionHoldsAllOfItOrNoneOfIt(t *testing.T) {
	// The second book repeats the first's title, which the primary key
	// refuses. What proves the rollback is not that the shelf is empty -- an
	// uncommitted row is invisible either way -- but that the transaction is
	// over: a rolled-back one has let go of its write lock, so adding the same
	// title afterwards succeeds, and one left open would refuse it.
	repeated := []library.Book{
		{Title: "Twice", Author: "A", Pages: 100},
		{Title: "Twice", Author: "B", Pages: 200},
	}
	held := succeeded(t, shelved(t, func(database *sql.Connected) shelving[[]library.Book] {
		return library.Restock(database, repeated...).
			CatchAll(func(sql.Fault) shelving[effect.Unit] {
				return effect.For[effect.Unit, sql.Fault]().Succeed(effect.Unit{})
			}).
			FlatMap(func(effect.Unit) shelving[sql.Outcome] {
				return library.Add(database,
					library.Book{Title: "Twice", Author: "C", Pages: 300})
			}).
			FlatMap(func(sql.Outcome) shelving[[]library.Book] {
				return effect.RunCollect(library.All(database))
			})
	}))

	if len(held) != 1 || held[0].Author != "C" {
		t.Fatalf("expected only the book added after the rollback, got %#v", held)
	}
}

func TestATransactionThatSucceedsIsCommitted(t *testing.T) {
	held := succeeded(t, shelved(t, func(database *sql.Connected) shelving[[]library.Book] {
		return library.Restock(database,
			library.Book{Title: "One", Author: "A", Pages: 1},
			library.Book{Title: "Two", Author: "B", Pages: 2}).
			FlatMap(func(effect.Unit) shelving[[]library.Book] {
				return effect.RunCollect(library.All(database))
			})
	}))

	if len(held) != 2 {
		t.Fatalf("expected both books committed, got %#v", held)
	}
}

func TestARowTheSchemaRefusesIsReportedWithItsColumn(t *testing.T) {
	// The schema's rules hold over a row as they hold over a document: a book
	// with no pages is not a book, whatever the column type permits.
	exit := shelved(t, func(database *sql.Connected) shelving[library.Book] {
		return sql.Execute[effect.Unit](database,
			`insert into books (title, author, pages) values (?, ?, ?)`,
			dynamic.OfText("Blank"), dynamic.OfText("A"), dynamic.OfInteger(0)).
			FlatMap(func(sql.Outcome) shelving[library.Book] {
				return library.ByTitle(database, "Blank")
			})
	})

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the row to be refused, got %+v", exit)
	}
	if failures := cause.Failures(); len(failures) != 1 ||
		failures[0].Doing != "decoding a row" {
		t.Fatalf("expected the stage named, got %+v", cause)
	}
}

func TestAStatementThatReturnsSeveralRowsIsRefusedWhenOneWasAsked(t *testing.T) {
	// Neither none nor several is the answer to a question phrased as one row,
	// so a second row is noticed rather than quietly ignored.
	exit := shelved(t, func(database *sql.Connected) shelving[library.Book] {
		return library.Restock(database,
			library.Book{Title: "One", Author: "A", Pages: 1},
			library.Book{Title: "Two", Author: "A", Pages: 2}).
			FlatMap(func(effect.Unit) shelving[library.Book] {
				return sql.QueryRow[effect.Unit](database, library.BookSchema,
					`select title, author, pages from books where author = ?`,
					dynamic.OfText("A"))
			})
	})

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected several rows to be refused, got %+v", exit)
	}
	if failures := cause.Failures(); len(failures) != 1 ||
		!strings.Contains(failures[0].Error(), "more than one row") {
		t.Fatalf("expected the reason to say so, got %+v", cause)
	}
}

// noted has an optional member, so that an absent one can be seen to bind as
// null in its own position rather than shifting the ones after it.
type noted struct {
	Title string
	Note  *string
	Pages int32
}

var notedSchema = schema.Struct[noted]("Noted",
	schema.FieldOf("title", schema.Text(),
		func(value noted) string { return value.Title },
		func(value *noted, title string) { value.Title = title }),
	schema.OptionalFieldOf("note", schema.Text(),
		func(value noted) (string, bool) {
			if value.Note == nil {
				return "", false
			}
			return *value.Note, true
		},
		func(value *noted, note string) { value.Note = &note }),
	schema.FieldOf("pages", schema.Int32(),
		func(value noted) int32 { return value.Pages },
		func(value *noted, pages int32) { value.Pages = pages }),
)

func TestAnAbsentOptionalMemberBindsAsNullInItsOwnPosition(t *testing.T) {
	// Leaving it out would shift every argument after it, so the column each
	// one answered to would change. A column not being given a value is what
	// null is for.
	arguments, err := sql.Arguments(notedSchema, noted{Title: "Untitled", Pages: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(arguments) != 3 {
		t.Fatalf("expected one argument per column, got %d", len(arguments))
	}
	if _, absent := arguments[1].(dynamic.Absent); !absent {
		t.Fatalf("expected the absent member bound as null, got %#v", arguments[1])
	}
	if arguments[2] != dynamic.OfInteger(7) {
		t.Fatalf("expected the page count still third, got %#v", arguments[2])
	}
	if names := strings.Join(sql.Columns(notedSchema), ","); names != "title,note,pages" {
		t.Fatalf("expected the columns in declared order, got %s", names)
	}
}

func TestAReadInsideATransactionDecidesTheWriteBesideIt(t *testing.T) {
	// Take reads the book and then removes it, in one transaction. That works
	// because a transaction answers the same operations a database does, so
	// ByTitle reads inside it without knowing it is inside one -- which is the
	// reason the port has Querying and Beginning as two interfaces.
	taken := succeeded(t, shelved(t, func(database *sql.Connected) shelving[library.Book] {
		return library.Add(database, library.Book{Title: "Lent", Author: "A", Pages: 120}).
			FlatMap(func(sql.Outcome) shelving[library.Book] {
				return library.Take(database, "Lent")
			})
	}))
	if taken.Author != "A" || taken.Pages != 120 {
		t.Fatalf("expected the book that was taken, got %#v", taken)
	}

	// Twice is once: the second attempt finds nothing, so its transaction rolls
	// back and the shelf is as the first left it.
	held := succeeded(t, shelved(t, func(database *sql.Connected) shelving[[]library.Book] {
		return library.Add(database, library.Book{Title: "Lent", Author: "A", Pages: 120}).
			FlatMap(func(sql.Outcome) shelving[library.Book] {
				return library.Take(database, "Lent")
			}).
			FlatMap(func(library.Book) shelving[[]library.Book] {
				return library.Take(database, "Lent").
					FlatMap(func(library.Book) shelving[[]library.Book] {
						return effect.For[effect.Unit, sql.Fault]().
							Fail[[]library.Book](sql.Fault{Doing: "taking it twice", Err: errTwice})
					}).
					CatchAll(func(fault sql.Fault) shelving[[]library.Book] {
						if fault.Doing == "taking it twice" {
							t.Error("expected the second take to find nothing")
						}
						return effect.RunCollect(library.All(database))
					})
			})
	}))
	if len(held) != 0 {
		t.Fatalf("expected an empty shelf, got %#v", held)
	}
}

var errTwice = errors.New("the same book was taken twice")
