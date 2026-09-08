// Package library keeps books in a database.
//
// It depends on the port and never on a driver, which is the point of there
// being a port: the program is written once and the test supplies sqlite while
// a deployment supplies whatever it has. Nothing here imports database/sql.
//
// One schema does three jobs again: it decodes a row, it names the columns, and
// it binds the arguments -- so the column list and the values cannot drift
// apart the way a hand-written pair eventually does.
package library

import (
	"errors"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// Book is one row.
type Book struct {
	Title  string
	Author string
	Pages  int32
}

// BookSchema describes a book: the wire, the row, and the columns.
var BookSchema = schema.Struct[Book]("Book",
	schema.FieldOf("title", schema.MinLength(schema.Text(), 1),
		func(book Book) string { return book.Title },
		func(book *Book, title string) { book.Title = title }),
	schema.FieldOf("author", schema.MinLength(schema.Text(), 1),
		func(book Book) string { return book.Author },
		func(book *Book, author string) { book.Author = author }),
	schema.FieldOf("pages", schema.AtLeast(schema.Int32(), 1),
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
// schema reorders both and neither can be left behind.
func Add(database sql.Querying, book Book) libraryEffect[sql.Outcome] {
	arguments, err := sql.Arguments(BookSchema, book)
	if err != nil {
		return effect.For[effect.Unit, sql.Fault]().Fail[sql.Outcome](faultOf(err))
	}
	return sql.Execute[effect.Unit](database, insertInto("books", BookSchema), arguments...)
}

// All streams every book, shortest first.
//
// A stream rather than a slice: the caller decides how many it reads, and a
// caller that reads three does not pay for the rest.
func All(database sql.Querying) effect.Stream[effect.Unit, sql.Fault, Book] {
	return sql.Query[effect.Unit](database, BookSchema,
		`select title, author, pages from books order by pages, title`)
}

// ByTitle reads the one book with that title, or refuses because there is none.
func ByTitle(database sql.Querying, title string) libraryEffect[Book] {
	return sql.QueryRow[effect.Unit](database, BookSchema,
		`select title, author, pages from books where title = ?`,
		dynamic.OfText(title))
}

// Restock adds several books as one transaction: either the shelf holds all of
// them or it holds none.
func Restock(database sql.Beginning, books ...Book) libraryEffect[effect.Unit] {
	return sql.Transact(database,
		func(fault sql.Fault) sql.Fault { return fault },
		func(within sql.Querying) libraryEffect[effect.Unit] {
			return effect.ForEach(books, func(book Book) libraryEffect[sql.Outcome] {
				return Add(within, book)
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
func Take(database sql.Beginning, title string) libraryEffect[Book] {
	return sql.Transact(database,
		func(fault sql.Fault) sql.Fault { return fault },
		func(within sql.Querying) libraryEffect[Book] {
			return ByTitle(within, title).FlatMap(func(book Book) libraryEffect[Book] {
				return sql.Execute[effect.Unit](within,
					`delete from books where title = ?`, dynamic.OfText(title)).
					As(book)
			})
		})
}

// insertInto writes the statement from the schema's own column names.
func insertInto[A any](table string, shape schema.Schema[A]) string {
	names := sql.Columns(shape)
	places := make([]string, len(names))
	for index := range places {
		places[index] = "?"
	}
	return `insert into ` + table + ` (` + strings.Join(names, ", ") + `) values (` +
		strings.Join(places, ", ") + `)`
}

func faultOf(err error) sql.Fault {
	var fault sql.Fault
	if errors.As(err, &fault) {
		return fault
	}
	return sql.Fault{Doing: "binding arguments", Err: err}
}
