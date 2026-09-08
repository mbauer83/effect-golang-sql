# SQL reference

A row is a set of named values, which is an object — so a row is decoded by the
**same `Schema`** that decodes a request body, and this package needs no
description of its own. That is what the schema layer exposing its structure and
the universal representation existing were for.

```go
effect.Scoped(func(scope effect.Scope) effect.Effect[R, sql.Fault, A] {
    return sql.Open[R](scope, "sqlite", source).
        FlatMap(func(database *sql.Connected) effect.Effect[R, sql.Fault, A] { ... })
})
```

`Open` connects **and checks the connection**, because `database/sql`'s own Open
is lazy: a wrong address or a missing file would otherwise surface at the first
query rather than at start-up, which is the wrong end of the program. The scope
owns the closing.

## Reading

```go
func Query[R, A any](database Querying, shape Schema[A], statement string, arguments ...dynamic.Value) Stream[R, Fault, A]
func QueryRow[R, A any](…) Effect[R, Fault, A]
func Execute[R any](database Querying, statement string, arguments ...dynamic.Value) Effect[R, Fault, Outcome]
```

`Query` is a **stream**, so a large result set need not be held. The cursor is
acquired in the consumer's scope, so it is released when the consumer is
finished — including when it stopped early, failed or was cancelled. A consumer
that reads three rows of a million does not read the rest and does not hold
them.

`QueryRow` refuses **none and several alike**: neither is the answer to a
question phrased as one row, and a second row is noticed rather than quietly
ignored. A caller for whom none is fine wants `Query` and a look at what came
back.

A column the schema does not know is skipped and a missing required one is
reported, which is the tolerance a document gets and for the same reason: a
query written against a newer table should still read.

A `Fault` carries the **statement**, because a database error without the SQL
that caused it is nearly useless — and the statement is the program's own text
rather than a user's.

## Binding

The port has no way to pass a value except as an argument, so a statement built
by concatenation cannot be expressed through it. That is what "prepared
statements by default" means where it matters. Whether a driver prepares and
caches is the driver's business; a caching adapter is something to add when
measurement asks for one.

```go
names  := sql.Columns(BookSchema)             // "title", "author", "pages"
values, err := sql.Arguments(BookSchema, book) // in the same order
```

Both come from one description, so the column list and the argument list cannot
drift apart the way a hand-written pair eventually does. An absent optional
member binds as **null in its own position**, because leaving it out would shift
every argument after it and change which column each answered to.

## Transactions

```go
sql.Transact(database, failing, func(within sql.Querying) Effect[R, E, A] { … })
```

It commits when the work succeeds and rolls back when it fails or is
interrupted, which is `AcquireRelease` with an explicit commit on the way out
and nothing new. The transaction owns **its own scope**, so a caller cannot hold
one open past the effect that asked for it and cannot forget to end it.

The release rolls back unconditionally. After a commit that is the driver saying
the transaction is already over, which is the outcome that was wanted; before
one it is the only thing that keeps a cancelled transaction from being left open
holding its locks.

`failing` is how a fault of this package becomes the work's own failure. It is a
parameter rather than a fixed type because a repository's refusals are the
application's — *no such customer*, *the order is already paid* — and forcing
them into a database fault would have the layering backwards.

A transaction answers the same operations a database does, which is what makes
the read that decides a write part of the same transaction as the write:

```go
sql.Transact(database, itself, func(within sql.Querying) Effect[R, E, Book] {
    return ByTitle(within, title).FlatMap(func(book Book) Effect[R, E, Book] {
        return sql.Execute[R](within, `delete from books where title = ?`,
            dynamic.OfText(title)).As(book)
    })
})
```

`ByTitle` does not know it is inside one. That is the whole reason `Querying`
and `Beginning` are two interfaces rather than one: a repository is written
against the operations, and whether it runs on a database or in a transaction is
the caller's business. `examples/library`'s `Take` is this, and two callers
cannot both be told they have the same book.

A `Transaction` answers `Query` and `Execute` but **not** `Begin`: nested
transactions are a different feature with different semantics, and a type
offering one it does not have would be lying.

## The port, and one of the two untyped files

`Querying`, `Beginning`, `Cursor` and `Transaction` are the whole port.
`database/sql` is itself an abstraction over drivers, so a second one earns its
place only because pgx's native interface is not `database/sql` — an adapter for
either fits behind the same operations, and an application that depends on the
port depends on neither. `examples/library` never imports a driver; the tests
supply sqlite.

`sql/driver_values.go` is one of the two files in this module allowed a top type
-- the other is an AMQP field table -- and the architecture test names both. `database/sql` scans into `any` and a driver
hands one back, because a driver cannot know what a column holds until it reads
it. That is a genuine boundary rather than a shortcut, so it is confined to one
file whose whole subject is crossing it — and above that line everything works
in the universal representation, which has a case for each of the seven kinds a
driver may produce.

That claim is worth what its coverage is worth, and sqlite produces four of the
seven: it has no boolean of its own and never hands back a value outside the
contract. So the boundary is checked in both directions against a driver written
for the purpose, which produces all seven and, in one statement, something no
driver is allowed to produce — a value the boundary names rather than guesses at,
which is the reason it may hold an unnamed one at all. A `Fault` unwraps, so a
caller can still ask the driver's own error whether a constraint was violated.
