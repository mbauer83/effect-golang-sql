// Package library keeps books in a database.
//
// It depends on the port and never on a driver, which is the point of there
// being a port: the program is written once and the test supplies sqlite while
// a deployment supplies whatever it has. Nothing here imports database/sql.
//
// One schema does three jobs again: it decodes a row, it names the columns, and
// it binds the arguments -- so the column list and the values cannot drift
// apart the way a hand-written pair eventually does.
//
// And it does not depend on a dialect either. Every statement below is said as
// what it asks and spelled by the Spelling it is given, so this is written
// once and a deployment on Postgres and a test on sqlite run the same code --
// including the parts the two spell differently, which is what a store that
// wrote its own SQL would have got wrong in one direction or the other.
package library

import (
	"errors"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

// Book is one row.
type Book struct {
	Title  string
	Author string
	Pages  int32
}

// BookSchema describes a book: the wire, the row, and the columns.
var BookSchema = schema.Struct[Book]("Book",
	schema.FieldOf("title", schema.Text().Constrained(schema.MinLength(1)),
		func(book Book) string { return book.Title },
		func(book *Book, title string) { book.Title = title }),
	schema.FieldOf("author", schema.Text().Constrained(schema.MinLength(1)),
		func(book Book) string { return book.Author },
		func(book *Book, author string) { book.Author = author }),
	schema.FieldOf("pages", schema.Int32().Constrained(schema.AtLeast[int32](1)),
		func(book Book) int32 { return book.Pages },
		func(book *Book, pages int32) { book.Pages = pages }),
).Documented("one book on the shelf")

type libraryEffect[A any] = effect.Effect[effect.Unit, sql.Fault, A]

// Schema is the statement that makes the table. A program that owns its
// schema says so in one place.
const Schema = `create table if not exists books (
	title  text    not null primary key,
	author text    not null,
	pages  integer not null
)`

// Create makes the table.
func Create(database sql.Querying) libraryEffect[sql.Outcome] {
	return sql.Execute[effect.Unit](database, Schema)
}

// Add inserts one book.
//
// The column list and the arguments come from the schema, so reordering the
// schema reorders both and neither can be left behind -- and the dialect
// spells what the statement binds, so neither has to be a question mark.
func Add(spelling sql.Spelling, database sql.Querying, book Book) libraryEffect[sql.Outcome] {
	arguments, err := sql.Arguments(BookSchema, book)
	if err != nil {
		return effect.For[effect.Unit, sql.Fault]().Fail[sql.Outcome](faultOf(err))
	}
	return sql.Run[effect.Unit](database, sql.Writing{
		Table:   books,
		Columns: sql.Columns(BookSchema),
		Values:  arguments,
	}.Statement(spelling))
}

// All streams every book, shortest first.
//
// A stream rather than a slice: the caller decides how many it reads, and a
// caller that reads three does not pay for the rest.
func All(spelling sql.Spelling, database sql.Querying) effect.Stream[effect.Unit, sql.Fault, Book] {
	return sql.Rows[effect.Unit](database, BookSchema, sql.Reading{
		Select: sql.Selected(sql.Columns(BookSchema)...),
		From:   sql.From(books),
		Ordered: []sql.Ordering{
			sql.Column[int32]("pages").Ascending(),
			sql.Column[string]("title").Ascending(),
		},
	}.Statement(spelling))
}

// ByTitle reads the one book with that title, or refuses because there is none.
func ByTitle(spelling sql.Spelling, database sql.Querying, title string) libraryEffect[Book] {
	return sql.Row[effect.Unit](database, BookSchema, sql.Reading{
		Select: sql.Selected(sql.Columns(BookSchema)...),
		From:   sql.From(books),
		Where:  sql.Equals("title", title),
	}.Statement(spelling))
}

// Restock adds several books as one transaction: either the shelf holds all of
// them or it holds none.
func Restock(
	spelling sql.Spelling,
	database sql.Beginning,
	shelved ...Book,
) libraryEffect[effect.Unit] {
	return sql.Transact(database,
		func(fault sql.Fault) sql.Fault { return fault },
		func(within sql.Querying) libraryEffect[effect.Unit] {
			return effect.ForEach(shelved, func(book Book) libraryEffect[sql.Outcome] {
				return Add(spelling, within, book)
			}).As(effect.Unit{})
		})
}

// Take removes the one book with that title, and hands it back.
//
// The read that finds it and the write that removes it are one transaction,
// which is the case a transaction exists for: two callers asking for the same
// book must not both be told they have it. That works because a transaction
// answers the same operations a database does, so ByTitle reads inside it
// without knowing it is inside one.
func Take(spelling sql.Spelling, database sql.Beginning, title string) libraryEffect[Book] {
	return sql.Transact(database,
		func(fault sql.Fault) sql.Fault { return fault },
		func(within sql.Querying) libraryEffect[Book] {
			// Direct style: two dependent steps read as two lines, where a
			// FlatMap would have put the second inside the first and made the
			// reading order the opposite of the doing order. No defer in the
			// body, which is the condition.
			return direct.Run(func(do *direct.Do[effect.Unit, sql.Fault]) Book {
				book := do.Await(ByTitle(spelling, within, title))
				do.Await(sql.Run[effect.Unit](within, sql.Removal{
					Table: books,
					Where: sql.Equals("title", title),
				}.Statement(spelling)))
				return book
			})
		})
}

// books is the one table this shelf is.
const books = "books"

func faultOf(err error) sql.Fault {
	var fault sql.Fault
	if errors.As(err, &fault) {
		return fault
	}
	return sql.Fault{Doing: "binding arguments", Err: err}
}
