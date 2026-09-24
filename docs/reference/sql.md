# SQL reference

A row is a set of named values, which is an object — so a row is decoded by the
**same `Schema`** that decodes a request body, and this package needs no
description of its own. That is what the schema layer exposing its structure and
the universal representation existing were for.

```go
effect.Scoped(func(scope effect.Scope) effect.Effect[R, sql.Fault, A] {
    return sql.Open[R](scope, "sqlite", source).
        FlatMap(func(database *sql.Database) effect.Effect[R, sql.Fault, A] { ... })
})
```

`Open` connects **and checks the connection**, because `database/sql`'s own Open
is lazy: a wrong address or a missing file would otherwise surface at the first
query rather than at start-up, which is the wrong end of the program. The scope
owns the closing.

## A domain type as a table stores it

`sql.Map(domain)` is a domain schema as a table stores it. By default the table
and its columns are the domain's names in snake_case; a mapping states only
where the table differs, field by field, through the domain's own field
handles:

```go
var films = sql.Map(catalog.FilmSchema).
    Column(catalog.FilmFields.ID, "tmdb_id").   // one column named exactly
    Schema()                                    // what rows are read and written through
```

| Method | What it says |
|---|---|
| `Table(name)` | the table's name, exactly; otherwise the object's name in the mapping's strategy |
| `Column(field, name)` | one column's name, exactly |
| `Naming(strategy)` | the strategy for every name not given exactly; snake_case by default |
| `Represent(field, shape, to, from)` | how a value is stored |
| `AsDocument(field)` | a value object kept in one document column |
| `Referring(targets...)` | where the aggregates its references identify are stored, so each becomes a foreign key |
| `Schema()` | the schema rows go through, and the table is made from |

- **A row decodes through the domain's constructor.** The mapped schema is the
  domain's, so a row an older version wrote that breaks a rule is refused where
  it is read.
- **A value object is a column per member**, named after the field and the
  member -- `artwork_poster` -- and nullable when the value object may be
  absent, which is how an absent one reads back absent. `AsDocument` keeps it
  whole, for a value always read whole.
- **A mapping adds no rules about values.** Constraints are the domain's, and
  the table checks them (see [DDL](ddl.md)).

## Reading

```go
func Query[R, A any](database Querier, shape Schema[A], statement string, arguments ...dynamic.Value) Stream[R, Fault, A]
func QueryRow[R, A any](…) Effect[R, Fault, A]
func Execute[R any](database Querier, statement string, arguments ...dynamic.Value) Effect[R, Fault, Outcome]
```

`Query` is a **stream**, so a large result set need not be held. The cursor is
acquired in the consumer's scope, so it is released when the consumer is
finished — including when it stopped early, failed or was cancelled. A consumer
that reads three rows of a million does not read the rest and does not hold
them.

`QueryRow` refuses **none and several alike**: neither is the answer to a
question phrased as one row, and a second row is noticed rather than quietly
ignored.

Both refusals are named -- `ErrNoRows` and `ErrSeveralRows`, reached through a
`Fault` with `errors.Is` -- because asking for one row by its identity has
three answers and not two: it is there, it is not there, or the database could
not be reached. A caller that could not tell the last two apart would have to
treat a record nobody has saved and a database that is down as the same event,
and they do not call for the same thing. A caller for whom none is fine can
also use `Query` and look at what came back.

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
drift apart the way a hand-written pair eventually does — which is what
`InsertQuery` and `UpsertQuery` take. An absent optional member binds as **null in
its own position**, because leaving it out would shift every argument after it
and change which column each answered to.

The statements below go further: a value is bound *where it occurs*, so there
is no second list to keep in step at all, and no caller ever writes a
placeholder.

## Saying a statement

A statement's spelling is the dialect's, not the caller's. Postgres numbers the
values a statement binds and the other two do not; an upsert is `ON CONFLICT`
in two of them and `on duplicate key update` in the third; MySQL's `length`
counts bytes where the other two count characters. A store that wrote any of
those by hand would compile, pass its SQLite suite, and be refused — or, for
the third one, quietly answer wrongly for every string that is not ASCII — by
the server it was deployed against.

So a store says **what it asks** and the dialect says how.

```go
sql.SelectQuery{Select: …, From: …, Joins: …, Where: …, GroupBy: …, Having: …}
sql.InsertQuery{Table: …, Columns: …, Values: …}
sql.UpsertQuery{Table: …, Columns: …, Key: …, Values: …}
sql.DeleteQuery{Table: …, Where: …}

statement := query.Statement(dialect)        // a Statement
sql.Rows[R](database, CardSchema, statement) // or Row, or Run
```

`Statement` takes a `Spelling`, which every `ddl.Dialect` is — so a store holds
the dialect it was built with and never names one. The `Statement` it answers is the rendered
text and the values it binds, kept together: `Run`, `Rows` and `Row` take one,
so nothing outside this package ever takes the pair apart.

## Expressions carry their type

`Expr[A]` is a value a query computes, of the Go type it computes. The type
parameter is a phantom — it constrains what can be built and is gone before
anything is rendered — and what it buys is the check a server would otherwise
make at the first request:

```go
sql.Equal(sql.Of[string](films, "title"), sql.Param(int64(7)))     // does not compile
sql.Substring(sql.Of[time.Time](v, "watched_at"), …)               // does not compile
sql.Both(sql.Present(x), sql.Of[string](t, "title"))               // does not compile
```

`Criterion` is `Expr[bool]`, because that is what a criterion is in SQL — so a
described boolean column is already one, and joining criteria is joining
expressions rather than a second vocabulary.

The types are the **domain's own**, which is the point:

```go
sql.Equal(sql.Of[catalog.FilmID](films, "film_id"), sql.Param(film))
```

`catalog.FilmID` is an `int64`, so it binds as a whole number and the column
must hold one; a `UserID` in the same position does not compile. `Param` reads
the value through its type rather than asserting on it, so a domain's own
identity binds as the text or the number it is.

## Schema-driven sources

A `Source` built from a description knows its columns **and what each of them
holds**, so `Of` is checked twice: that the source has a column of that name,
and that what it holds is what this reads it as.

```go
tables, _ := ddl.Tables(dialect, collection.TrackingSchema.Structure())
trackings := tables[0].Source().As("t")

sql.Of[time.Time](trackings, "watchlisted_at")   // fine
sql.Of[time.Time](trackings, "watchlisted")      // refused: no such column, and here is what there is
sql.Of[int64](trackings, "watchlisted_at")       // refused: holds a moment, read as a whole number
```

That is the whole of what schema-driven means here: a member renamed or
retyped in the domain is a refusal where the query names it, rather than a
statement the server rejects at the first request. `sql.From("table")` with no
columns given checks nothing, which is honest about having been told nothing —
a query over a table this module has no description of is still a query.

`FromQuery(query)` and `With(name, query).Source()` are sources too, and
they know their columns from the inner query's selections — including what
each answered, because `As` keeps the expression's type.

## Which rows

```go
sql.Equal(left, right)      sql.NotEqual(left, right)
sql.Below(left, right)      sql.AtMost(left, right)
sql.Above(left, right)      sql.AtLeast(left, right)
sql.Among(of, values...)    sql.AmongValues(of, goValues...)
sql.Present(of)             sql.Absent(of)
sql.Like(of, pattern)       sql.Regexp(of, pattern)
sql.Both(criteria...)       sql.Either(criteria...)      sql.Not(criterion)
sql.All()                   sql.None()
sql.ColumnEquals(column, value) // the one shorthand: a row by its identity
```

Both sides of a comparison are `Expr[A]` of the *same* `A`, which is what makes
a comparison across types a compile error. `Among` with nothing in it is a
criterion no row satisfies rather than `in ()`, which none of the three accept:
a filter that turned out empty gives the empty answer instead of a syntax
error. `Both` drops a member that excludes nothing, so a query can and its own
criterion into whatever a caller supplied without asking whether the caller
supplied one — and a junction inside a junction is bracketed, so `and` binding
tighter than `or` never decides a meaning.

## What a query computes

```go
sql.Count()              sql.CountOf(of)
sql.Max(of)              sql.Min(of)          sql.Sum(of)       sql.Avg(of)
sql.StringAgg(of, ", ")  // one string per group
sql.Concat(…)            sql.Substring(of, from, count)
sql.Lower(of)            sql.Upper(of)        sql.Trim(of)      sql.Length(of)
sql.Coalesce(…)          sql.Seconds(later, earlier)
sql.Plus(l, r)           sql.Minus(l, r)      sql.Times(l, r)   sql.Div(l, r)      
sql.Subquery[A](query)   // the one value another query answers with
```

The types are what make them worth having: `Length` answers a whole number
whatever text it is given, `Seconds` takes two moments and answers a number,
`Avg` answers a number even where the values are whole ones. `Seconds` is in
seconds and only seconds, because a difference in days is a whole number on
one server and a fraction on another.

## Joining, grouping, and what a group is filtered by

```go
sql.SelectQuery{
    Select: []sql.Selection{
        sql.Of[int64](items, "Pallet_id").As("pallet"),
        sql.Count().As("lines"),
        sql.Sum(sql.Of[int32](items, "quantity")).As("quantity"),
    },
    From:    items,
    Joins:   []sql.Join{sql.LeftJoin(pallets, sql.Equal(…))},
    GroupBy: sql.Terms(sql.Of[int64](items, "Pallet_id")),
    Having:  sql.AtLeast(sql.Count(), sql.Param(int64(2))),
}
```

`InnerJoin` keeps the rows that match on both sides; `LeftJoin` keeps every row
already there, matched or not — a left outer join, which is the one people
reach for and the one that changes an answer. A shelf read by joining what
somebody owns to what they have watched loses every disc they have not
watched, silently.

There is no right outer join, because it is `LeftJoin` written the other way
round; and no full outer, because MySQL has none and a specification that
emitted one would compose a statement one of the three servers cannot run.

`GroupBy` and `Having` are two fields because they are two decisions, and the
second is the one that is easy to get wrong: a criterion over an aggregate
belongs in `Having` and one over a column belongs in `Where`. A server will say
so — but only for the direction that is illegal. A column criterion put in
`Having` is legal, runs, and reads every row of every group before discarding
it.

## Windows

```go
sql.Count().Over(sql.Window{
    PartitionBy: sql.Terms(sql.Of[string](v, "tracking_id")),
    OrderBy:     []sql.Ordering{sql.Of[time.Time](v, "watched_at").Ascending()},
}).As("so_far")
```

An aggregate collapses the group; the same aggregate over a window does not —
which is what a running total, a rank within a partition, or each row beside
its group's average needs. A frame is the next field to add, not a different
shape.

## Common table expressions and derived tables

```go
totals := sql.With("totals", sql.SelectQuery{…})
sql.SelectQuery{
    With:  []sql.CTE{totals},
    From:  totals.Source().As("s"),
    Where: sql.Above(sql.Of[int64](totals.Source(), "lines"), sql.Param(int64(1))),
}
```

The arrangement a computed value forces: a name given in a select list cannot
be used in the clause that computes it, so the value is computed by one query
and filtered or paged by the one reading it. `query.As("name")` is the
same thing as a derived table when the expression is read once and needs no
name of its own.

## Paging

A cursor is not a criterion a caller assembles. `SelectQuery.After` is a position
in the order the query already states, and the comparison is derived from
both:

```go
sql.SelectQuery{
    Select:  rows.Columns(),
    From:    rows,
    OrderBy: []sql.Ordering{
        sql.Of[time.Time](rows, "watchlisted_at").Descending(),
        sql.Of[catalog.FilmID](rows, "film_id").Descending(),
    },
    After: []dynamic.Value{sql.At(lastSeenAt), sql.At(lastSeenFilm)},
    Limit: 40,
}
// … WHERE "watchlisted_at" < $1 OR ("watchlisted_at" = $2 AND "film_id" < $3)
```

One statement of the order, so a page cannot be read one way and cut another.
The comparison is lexicographic because that is the only form that is correct:
one on the leading column alone repeats the rows that tie with the page
boundary, and one on both columns unconditionally skips rows past it. Neither
shows up unless the fixture ties, which is why the test that covers it has two
rows sharing a timestamp.

### A listing, read a page at a time

A `Listing` is a table, a read model or one owner's collection, with what it
offers a reader declared once:

```go
listing := sql.NewListing(films.Schema(), table.Source(), "tmdb_id").
    Sort("title", sql.Of[string](source, "title").Ascending()).   // the first is the default
    Sort("recent", sql.Of[int64](source, "year").Descending()).
    PageSize(40, 120).                                             // 40 unless asked, never more than 120
    DeepestPage(1000).                                             // for a table that grows without bound
    Within(sql.Equal(sql.Of[string](source, "owner"), sql.Param(owner)))

page := listing.Page[Env](database, dialect, sql.PageQuery{Sort: "recent", After: cursor, Size: 50})
```

- **Every sort ends with the key**, which the listing appends, so no two rows
  tie and a page boundary is exact.
- **Keyset or numbered.** `After` and `Before` continue from a cursor: the same
  cost for every page, and no row repeated or skipped while rows are written.
  `Number` is a numbered page, read with a deferred join -- the rows before it
  are passed over as index entries, not rows -- and limited by `DeepestPage`.
- **Every page carries cursors** (`Next`, `Previous`), however it was read, so a
  reader can jump to page 37 and continue by keyset from there.
- **A cursor is opaque and belongs to one query.** It records the sort and a
  fingerprint of the filter, and one handed back under another is refused
  (`ErrPageCursor`), so a client starts again rather than reading pages that
  match nothing on its screen.
- **What a listing does not offer is refused** (`ErrPageQuery`): an unknown sort,
  a page larger than the largest, a page deeper than the deepest, or two
  positions at once -- each a client's mistake, named.
- **Counts are separate.** `Count` reads every row it counts; `CountUpTo` stops at
  a number, for "more than a thousand".
- **A sort orders by columns**, since those are what a cursor records, and not
  yet by a nullable one: a cursor cannot hold a null.

## Operations, and how a dialect is taught one

This is the extensible half, and it has to be: the set of operations a server
offers is that server's, it grows between versions, and no interface written
here could name them all.

An **operation is a value**, not a name — opaque, compared by identity, so a
dialect answers about it with a switch and there is no string a typo can turn
into an operation nobody offers.

```go
type Operation struct{ … }                 // opaque, comparable
func Declare(name string) Operation        // an operation this module does not name
func (Operation) WithDefault(Syntax) Operation  // a spelling every dialect is taken to use

type Syntax func(spelling Spelling, application Application) []Part
type Application struct{ Operation Operation; Detail string; Arguments [][]Part }

type Operations interface {
    Syntax(operation Operation) (Syntax, bool)
}
```

Most operations are **ordinary** — `lower(x)` is `lower(x)` on every server
anybody has shipped — so they carry the spelling and a dialect answers nothing.
A few are not, and those carry none: a dialect that does not answer about them
refuses, and the refusal names the dialect and the operation.

A dialect's answer is a line, because the shapes an answer takes are values
too:

```go
func (postgres) Syntax(operation sql.Operation) (sql.Syntax, bool) {
    switch operation {
    case sql.Concatenation:     return sql.Operator(" || "), true
    case sql.SubstringOf:       return sql.Phrase("SUBSTRING(", " FROM ", " FOR ", ")"), true
    case sql.StringAggregation: return sql.DetailPhrase("string_agg(", ", %s)"), true
    case sql.SecondsBetween:    return sql.Phrase("extract(epoch from (", " - ", "))"), true
    case sql.ExpressionMatch:   return sql.Infix(" ~ "), true
    default:                    return nil, false
    }
}
```

| shape | writes |
|---|---|
| `Function("lower")` | `lower(a)` |
| `Operator(" \|\| ")` | `(a \|\| b)` |
| `Infix(" = ")` | `a = b` — a comparison, unbracketed |
| `Phrase("substr(", ", ", ", ", ")")` | `substr(a, b, c)`, and any other syntax |
| `DetailPhrase("string_agg(", ", %s)")` | the use's own detail, in this dialect's quoting |
| `Flip(syntax)` | the same, arguments the other way round |
| `ListOperator(" in ", "(", ", ", ")")` | `a in (b, c)` |
| `Weave(before, between, after)` | the general one |

The arguments arrive as **pieces** rather than as text, because an argument may
bind a value and the ordinal a dialect gives it is decided by the `Compose` the
operation ends up in — so a dialect splices pieces and never counts them.

Two seams, and neither needs anything in this module to change:

```go
// An operation this module does not name.
var Soundex = sql.Declare("soundex").WithDefault(sql.Function("soundex"))
sql.Apply[string](Soundex, title.Term())

// A dialect that can do one it did not claim: SQLite has no regular
// expression unless the program that opened the database registered one.
dialect := sql.Also(ddl.SQLite, map[sql.Operation]sql.Syntax{
    sql.ExpressionMatch: sql.Infix(" regexp "),
})
```

## Refusals

A query that cannot be composed carries **why**, and the runners will not send
it:

```go
statement := query.Statement(dialect)
statement.Err()       // a column no source has, a kind that disagrees, an
                      // operation this dialect cannot perform, a query with
                      // nothing selected or nowhere to read from
```

Carried rather than returned, because the mistakes are about the query's
*shape* — decided once — while a statement is composed on every request. So a
store composes as it always did, and `Run`, `Rows` and `Row` fail with a fault
naming what was wrong. There is no path by which a refused statement reaches a
server, and a test says so by handing one to a database that records
everything it is asked and finding it was asked nothing.

## What is deliberately absent

Recursive expressions, window frames, set operations, `distinct`, and anything
a server spells as a statement rather than as a query — `explain`, `vacuum`,
locking clauses. Each is a field or an operation when it is wanted rather than
a different shape, which is what the specification being a value is for.

A statement outside the specification altogether is written with `Compose`,
where a caller writes the text and still never writes a placeholder — and
where the parts it does not want to write by hand are the specification's:

```go
sql.Compose(dialect, append(
    []sql.Part{sql.Text(`SELECT COUNT(*) FROM "film_viewing" WHERE `)},
    sql.Condition(dialect, sql.Both(
        sql.ColumnEquals("user_id", user),
        sql.Above(sql.Of[time.Time](v, "watched_at"), sql.Param(since)),
    ))...,
)...)
```

`Condition` renders a criterion and `Computation` renders an expression, so a
hand-written statement still gets the dialect's own spelling of an operation
and the tested spelling of a keyset. `Compose` counts the ordinals across every
piece, so a statement's second value is its second wherever in the statement it
was written — which is the other half of what this removes: text and values as
two lists that agree until somebody inserts a condition in the middle.

## Transactions

```go
sql.Transact(database, mapFault, func(within sql.Querier) Effect[R, E, A] { … })
```

It commits when the work succeeds and rolls back when it fails or is
interrupted, which is `AcquireRelease` with an explicit commit on the way out
and nothing new. The transaction owns **its own scope**, so a caller cannot hold
one open past the effect that asked for it and cannot forget to end it.

The release rolls back unconditionally. After a commit that is the driver saying
the transaction is already over, which is the outcome that was wanted; before
one it is the only thing that keeps a cancelled transaction from being left open
holding its locks.

`mapFault` is how a fault of this package becomes the work's own failure. It is a
parameter rather than a fixed type because a repository's refusals are the
application's — *no such customer*, *the order is already paid* — and forcing
them into a database fault would have the layering backwards.

A transaction answers the same operations a database does, which is what makes
the read that decides a write part of the same transaction as the write:

```go
sql.Transact(database, itself, func(within sql.Querier) Effect[R, E, Book] {
    return ByTitle(within, title).FlatMap(func(book Book) Effect[R, E, Book] {
        return sql.Run[R](within, sql.DeleteQuery{
            Table: "books",
            Where: sql.ColumnEquals("title", title),
        }.Statement(dialect)).As(book)
    })
})
```

`ByTitle` does not know it is inside one. That is the whole reason `Querier`
and `Beginner` are two interfaces rather than one: a repository is written
against the operations, and whether it runs on a database or in a transaction is
the caller's business. `examples/library`'s `Take` is this, and two callers
cannot both be told they have the same book.

A `Transaction` answers `Query` and `Execute` but **not** `Begin`: nested
transactions are a different feature with different semantics, and a type
offering one it does not have would be lying.

## The port, and one of the two untyped files

`Querier`, `Beginner`, `Cursor` and `Transaction` are the whole port.
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
