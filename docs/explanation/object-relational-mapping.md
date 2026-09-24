# Object-relational mapping

A design. Its first two steps are built (section 15): `schema.Object` with
field handles, naming strategies, projections of an object, and a web surface's
naming. The rest is not built yet. It says how a domain type is described once and stored
without a second model of it. Section 14 records which decisions are settled
and which are still open.

## 1. What it is for

A film in the films application is declared four times today: the aggregate,
the struct its constructor takes, a row struct the storage schema decodes into,
and a struct the API encodes. Converters sit between each of them and the next,
and none of them adds information. They exist for two reasons. A schema can
only describe a struct it can write into field by field. And a column name and
a wire name are written separately from the domain's name, even when all three
are the same word.

The aims:

- **One declaration of a domain type.** The aggregate, and one schema of it
  that reads through the aggregate's own getters and builds values through its
  own constructor.
- **The default case costs nothing.** Every target uses the domain's names, in
  the case its policy states, and the domain's constraints.
- **Deviations are declared separately, and only for what deviates.** Each lives
  in the ring it belongs to, and names the fields it changes by typed handles,
  so renaming a field breaks the build rather than a query.
- **Every domain-valid value can be stored.** Storage adds no rules of its own
  about values.
- **Collections are queried and changed in the database.** Paging, filtering,
  search, insertion, removal and reordering run as indexed statements, and do
  not load a collection into memory.

## 2. The domain schema: built by construction

`Struct`/`FieldOf` builds a value from its zero value, one setter at a time. It
was chosen to avoid a constructor for every number of fields, and it has two
costs. A type with unexported fields cannot be described outside its package,
and a decoded value skips its constructor, so its invariants are checked
separately if at all.

Instead, a field is a typed handle: a name, a shape and a getter. An object is
its fields plus one constructor. The constructor receives the decoded values
and reads each through its handle:

```go
// FilmFields are a film's members, as every projection of one names them.
var FilmFields = struct {
	ID       schema.Field[Film, FilmID]
	Imdb     schema.Field[Film, ImdbID]
	Title    schema.Field[Film, string]
	Year     schema.Field[Film, int]
	Artwork  schema.Field[Film, Artwork]
	Overview schema.Field[Film, string]
	Runtime  schema.Field[Film, time.Duration]
	SyncedAt schema.Field[Film, time.Time]
	Shape    schema.Field[Film, Shape]
}{
	ID:       schema.FieldOf("id", FilmIDSchema, Film.Identity).Identity(),
	Imdb:     schema.FieldOf("imdb", ImdbIDSchema, Film.Imdb).Optional(),
	Title:    schema.FieldOf("title", schema.Text().Check(schema.MinLength(1), schema.MaxLength(500)), Film.Title),
	Year:     schema.FieldOf("year", schema.Int().Check(schema.AtLeast(0), schema.AtMost(9999)), Film.Year),
	Artwork:  schema.FieldOf("artwork", ArtworkSchema, Film.Artwork),
	Overview: schema.FieldOf("overview", schema.Text().Check(schema.MaxLength(4000)), Film.Overview),
	Runtime:  schema.FieldOf("runtime", schema.Duration().Check(schema.WholeMinutes), Film.Runtime),
	SyncedAt: schema.FieldOf("syncedAt", schema.Time(), Film.SyncedAt),
	Shape:    schema.FieldOf("shape", ShapeSchema, Film.Shape),
}

// FilmSchema is a film, decoded through Record: a value that decodes is a
// film this package would have made.
var FilmSchema = schema.Object("Film", func(v schema.Values) (Film, error) {
	f := FilmFields
	return Record(FilmDescription{
		ID: f.ID.Of(v), Imdb: f.Imdb.Of(v), Title: f.Title.Of(v), Year: f.Year.Of(v),
		Artwork: f.Artwork.Of(v), Overview: f.Overview.Of(v), Runtime: f.Runtime.Of(v),
		Shape: f.Shape.Of(v),
	}, f.SyncedAt.Of(v))
}, FilmFields.ID, FilmFields.Imdb, FilmFields.Title, FilmFields.Year, FilmFields.Artwork,
	FilmFields.Overview, FilmFields.Runtime, FilmFields.SyncedAt, FilmFields.Shape)
```

- `Of` is typed: the constructor cannot read a title as an int.
- If the constructor reads a field the object does not declare, `Object`
  refuses it when the schema is built. It runs the constructor once over empty
  values that record what is read.
- Encoding reads through the getters, so nothing beyond what the aggregate
  already exports needs exporting.
- Value objects (`Artwork`) are described the same way. A plain struct with
  exported fields and no rules keeps `Struct`/`FieldOf`, which stays for that
  case.

The handles are what every overlay and every query below refers to. A field is
named in Go in one place only.

## 3. Names

### 3.1 One name per field, cased per target

A field's name is written once, in lowerCamel (`syncedAt`). Each target applies
a naming policy, declared once for the target:

| Target | Policy | `syncedAt` becomes |
|---|---|---|
| SQL tables and columns | snake_case | `synced_at` |
| JSON (web) | camelCase | `syncedAt` |
| TypeScript | PascalCase for components, camelCase for keys | `Film.syncedAt` |
| Protobuf | snake_case | `synced_at` |

An object's name is cased by the same policy, so `Film` becomes the table
`film`. The library splits a name into words once, which is why `imdbID`,
`ImdbID` and `imdb_id` all split to `imdb, id`. The policy is set where the
target is assembled (`Routes.WithNaming(...)` for a web surface, and the table mapping for storage). A name
given explicitly in a mapping is used exactly as written.

### 3.2 A unit is part of the name

When a target stores a value as a bare number in some unit, the representation
adds the unit to the name as a whole word. `runtime`, stored as minutes, is the
column `runtime_minutes` and the JSON key `runtimeMinutes`. A bare `8160` is
ambiguous, and most tools never show a column's comment.

Changing the unit renames the column. That is what should happen: a change of
unit converts every row, so it has to be a visible migration (section 12).
Timestamps take no suffix, because a name like `syncedAt` already says what it
is.

## 4. The table mapping

The storage adapter maps a domain schema to tables. With no deviations, the
mapping is just the schema:

```go
var films = sql.Map(catalog.FilmSchema)
```

This gives the table `film` with the columns `id`, `imdb`, `title`, `year`,
`artwork_poster`, `artwork_backdrop`, `overview`, `runtime_minutes`,
`synced_at` and `shape`. Their types and constraints come from the domain
(section 10).

### 4.1 What a mapping may say

A mapping may give a table or column its name, say how a value is stored, and
choose between flattening and a document. It may not restrict which values are
valid (section 10.3).

```go
f := catalog.FilmFields
var films = sql.Map(catalog.FilmSchema).
	Table("films").                  // a table's name, exactly as written
	Column(f.ID, "tmdb_id").         // a column's name, exactly as written
	Represent(f.Runtime, sql.Minutes) // how a value is stored
```

### 4.2 Representations are lossless

A representation is a pair of conversions between a domain value and a column
value, plus the column's type. It must hold every value the domain allows, and
building a mapping refuses one that cannot:

- An integer column's type comes from the domain's range: the narrowest type
  that holds `AtLeast`..`AtMost`, and `bigint` when the domain states no range.
- A duration is stored in whole seconds by default. The domain has to say its
  durations are whole seconds (`schema.WholeSeconds`), or whole minutes, and so
  on. Otherwise the representation is refused as lossy. Films' runtime is
  declared in whole minutes, which makes `sql.Minutes` lossless.
- Time is a timestamp in UTC. A named string type is text. An enumeration is
  text, with a check of its members (section 10.2).
- A representation of the program's own is two functions and a column type.

### 4.3 Value objects are flattened

A value object with no identity (`Artwork`) is stored in columns prefixed with
its field's name (`artwork_poster`, `artwork_backdrop`). Each part can then be
filtered and indexed, and no JSON functions are needed. `AsDocument(f.Artwork)`
stores it in one document column instead (`jsonb` in Postgres, `text`
elsewhere), for a value that is always read whole.

## 5. Relations

Relations are declared in the domain schema, by the shape of a field.
Cardinality follows from the shape, and foreign keys follow from the relation.
The mapping only names tables and columns.

There are two constructs:

- **Embedding an entity schema**, meaning an object with an `Identity()` field,
  is *composition*. The child belongs to this aggregate and goes with it.
- **`schema.Ref(target.Fields.ID)`** is *association*: a reference to another
  aggregate by its identity. It takes the target's identity handle, so it is
  typed (`Schema[catalog.FilmID]`), and it knows which object it points at.

| Relation | Declared in the domain | Derived in SQL |
|---|---|---|
| 1-to-1, composition | `Field("preferences", PreferencesSchema, Account.Preferences)` (`.Optional()` for 0..1) | a child table keyed by the parent's key, with a cascading foreign key |
| 1-to-many, composition | `Field("copies", schema.List(CopySchema), Shelf.Copies)`, or a lazy collection (section 6) | a child table, with a cascading foreign key to the parent |
| many-to-1, association | `Field("film", schema.Ref(catalog.FilmFields.ID), Viewing.Film)` | a `film_id` column, a foreign key and an index; nullable if `.Optional()` |
| 1-to-1, association | as many-to-1, plus `.Unique()` | a foreign key with a unique index |
| many-to-many, no duplicates | `schema.Set(schema.Ref(catalog.FilmFields.ID))` | a join table with its primary key over both sides |
| many-to-many, ordered | `schema.List(schema.Ref(catalog.FilmFields.ID))` | a join table with an order key (section 6.3) |

Some consequences:

- **One-to-many between aggregates is declared on the "many" side, as
  many-to-1.** A film's viewings are not a field of `Film`. `Viewing` refers to
  its film, and "a film's viewings" is a read-model query (section 9).
  References point one way, and each foreign key is declared exactly once.
- **What deletion does is declared on the reference**, because it is a
  business rule: `.OnDelete(schema.Restrict)`, the default, or
  `schema.Cascade`, or `schema.SetNull` (refused unless the field is optional).
  Composition always cascades.
- **An identity from outside the system is not a `Ref`.** A TMDb or IMDb
  identity is a plain field of its own type. Every `Ref` points to an aggregate
  in this model, so every `Ref` has a table to point at.
- **An association with attributes** (a film on a list *since* a date, *with* a
  note) is composition whose child holds a reference: a child entity with a
  cascading foreign key to its owner and a restricted one to the film.
- **A composite identity** is referenced through a key handle
  (`schema.Key(fields...)`) and gives a composite foreign key.
- **A reference to the object's own type** ("sequel of") uses
  `schema.Suspend`, as recursion already does.
- **Refused:** a cycle of compositions, and an entity owned by two aggregates.
- **The mapping only names things:** `JoinTable(f.Films, "listed_film")` and
  `Column(f.Film, "tmdb_id")`.

## 6. Collections

A collection is a composition that is a list or a set: a shelf's copies, a
watchlist's films, a person's diary of viewings. It is either small enough to
be part of the aggregate's value, or it can grow without bound and is queried
and changed in the database, never loaded whole.

### 6.1 Eager and lazy collections

The domain chooses, because the choice is where the aggregate's consistency
boundary lies:

- **An eager collection** is a field of the aggregate, as in section 5. It is
  loaded and saved with the aggregate, using one query per child table for any
  number of owners, never one per owner. It suits collections the domain keeps
  small, such as a preference's list of languages.
- **A lazy collection** is declared with the aggregate but is never loaded into
  its value. Every operation on it is a query or a statement. This is what
  Doctrine calls an *extra lazy* collection and Hibernate an *extra-lazy* one,
  where `count`, `contains` and `slice` run in the database. It is not the
  *lazy* of an object graph that loads on first access: there are no proxies,
  and nothing loads the whole collection.
  ```go
  // WatchlistFilms are the films on a watchlist: a set, in the order the person put them.
  var WatchlistFilms = schema.Lazy(WatchlistFields.ID, "films",
  	schema.Set(schema.Ref(catalog.FilmFields.ID))).Ordered()

  // DiaryViewings are a person's viewings, in the order they happened.
  var DiaryViewings = schema.Lazy(AccountFields.ID, "viewings",
  	schema.List(ViewingSchema)).OrderedBy(ViewingFields.WatchedAt)
  ```
  Its elements are values of their own schema, each constructed and so
  validated on its own. The storage adapter gets a typed handle to it:
  ```go
  var watchlistFilms = sql.CollectionOf(lists, collection.WatchlistFilms) // sql.Collection[WatchlistID, catalog.FilmID]
  ```

### 6.2 Operations

Every operation on a lazy collection is scoped to one owner. The owner's key
leads every statement's `WHERE` clause and every index, so one person's list
cannot be reached through another's, even by an element's key.

| Operation | Statement |
|---|---|
| `Query(owner, query)` | one indexed select, filtered, sorted and paged as in section 7 |
| `Count(owner, filter)` | one indexed count |
| `Has(owner, element)` | one indexed lookup |
| `Insert(owner, element, sql.Last)` (or `First`, `Before(x)`, `After(x)`) | one insert |
| `Move(owner, element, sql.Before(x))` | one update of one row (6.3) |
| `Remove(owner, element)` | one delete |
| `RemoveWhere(owner, filter)` | one delete |
| `InsertAll`, `ReplaceAll` | one statement, or one transaction |

- **A refusal is typed.** Inserting a duplicate into a set is the domain's
  "already listed" refusal, recognised from the unique index. It is not a
  driver error, and it needs no separate lookup first.
- **Rules the domain can declare are enforced by the database in the same
  statement:** uniqueness within the collection, the element's constraints and
  foreign keys. `MaxItems(n)` on a lazy collection is kept in a count column on
  the owner, updated and checked in the same transaction. That is constant work,
  not a count of every element.
- **A rule that needs more than that** is a domain operation that takes the
  collection as a port. This is films' existing rule: the domain requires
  nothing and takes its ports as arguments.
  ```go
  func (list Watchlist) Add(film catalog.FilmID, films collection.FilmQuery) answer.Of[collection.Insertion]
  ```
  It asks only what it needs (`Has`, `Count`, a page) and returns the change
  for the adapter to apply.

### 6.3 Order chosen by the person

A collection is ordered in one of two ways:

- **By one of its elements' fields** (`OrderedBy(ViewingFields.WatchedAt)`).
  No position is stored. The order is that field, with the element's key to
  break ties.
- **By the person** (`Ordered()`). Each element has an order key, which the
  mapping maintains.

An order key is a variable-length string that sorts between its neighbours
(fractional indexing). Inserting between two elements, or moving one there,
writes one key between theirs, which is one row whatever the collection's size.
Keys stay short in normal use. When an owner's longest key passes a limit, that
owner's keys are rewritten evenly in one transaction. That touches only one
owner's rows, and rarely. The column is compared byte by byte (`C` collation in
Postgres, `BINARY` in SQLite), so every dialect sorts it the same. Two
concurrent inserts at the same place get distinct keys, because a key carries
a random tail, and the unique index over (owner, key) is the backstop.

## 7. Queries: filter, sort, search and page

Every collection is queried the same way: a repository's table (the films), a
read model (a diary's rows), and a lazy collection (one watchlist's films,
scoped to its owner). A query is a filter, a search, a sort and a page:

```go
f := catalog.FilmFields
query := sql.Query(
	f.Year.AtLeast(2000),
	f.Title.Search(sql.Prefix, "mat"),
).SortBy(f.Year.Desc(), f.Title.Asc()).After(cursor).Size(50)

films.Query(query)                   // a repository
watchlistFilms.Query(owner, query)   // a lazy collection
diaryRows.Query(query)               // a read model
```

### 7.1 Filters

Filters are typed criteria on the target's fields, built from the field
handles. On a referenced aggregate's fields they go through a join derived from
the reference:

```go
collection.ListingFields.Film.To(catalog.FilmFields.Year).AtLeast(2000)
```

### 7.2 Sorts

- **What a query target may be sorted by is declared,** and each declared sort
  is backed by an index (7.5). An API can therefore offer a choice of sorts
  without offering a slow one.
- **A sort always ends with the target's key.** The library appends it when it
  is not already last, so the order is total and a page boundary is exact.
- **Directions can be mixed.** `sql.After` already builds the full
  lexicographic comparison (`a > x OR (a = x AND b < y)`), so descending year
  then ascending title pages correctly.
- **A nullable sort field states where its nulls go** (`NullsLast()` or
  `NullsFirst()`), and the keyset comparison takes them into account. That is
  new: `sql.After` does not handle nulls yet, and without it a page boundary on
  a null would skip or repeat rows.
- **Sorting by a referenced aggregate's field** (a watchlist by film title)
  needs a join, and no index on one table can serve a sort by another's column.
  On a repository that is refused. On a lazy collection it can be declared
  `Unindexed()`, which accepts sorting one owner's rows per page. That is work
  bounded by one list's size, and the declaration makes it explicit.

### 7.3 Pages

There are two ways to ask for a page, and both are offered, because they serve
different features:

- **Keyset** (also called the seek method, or cursor pagination): "the next 50
  rows after this one, in this order". A page continues after the last row's
  sort values and key. It suits feeds, infinite scrolling, and next/previous.
- **Numbered**: "page 37 of 50 rows". It uses `OFFSET`, and suits page links,
  "page 12 of 40", and administrative tables.

```go
films.Query(query.After(cursor).Size(50))  // keyset
films.Query(query.Page(37).Size(50))       // numbered
```

**Page size.** A query target declares its default and largest page size, once:

```go
var films = sql.Map(catalog.FilmSchema).PageSize(40, 120) // 40 unless asked, never more than 120
```

- A query without `Size` gets the default, and a larger size is a client error
  that names the largest. Films' hand-written `Window.Clamp` goes away.
- It is called `Size` rather than `Limit`, because a page size is what it is.
  `LIMIT` is the SQL that implements it, and a query that is not paged (the top
  ten) keeps `Limit`.
- For a numbered page, the size is part of the page's identity: page 37 of 25
  rows is not page 37 of 50. So page links carry the size, and page numbers
  start at 1. A cursor does not depend on the size: "after this row" holds for
  any page size, so a client can change the size mid-scroll.

They differ in cost and in stability:

| | Keyset | Numbered |
|---|---|---|
| Cost of a page | the same for every page: seek to the last row in the index, read the page | grows with the page number: the database reads and discards every row before it |
| While rows are inserted or deleted | no row repeated or skipped | rows shift: a row can appear on two pages, or on none |
| Jump to page *n* | no; next, previous, or seek to a value | yes |

The library narrows both gaps:

- **A numbered page is fetched with a deferred join.** The skipping happens in
  a subquery that selects only keys, which the declared sort's index answers
  without reading the table. Only the page's own rows are then read:
  ```sql
  SELECT film.* FROM film
  JOIN (SELECT id FROM film WHERE … ORDER BY year DESC, id DESC LIMIT 50 OFFSET 1800) AS page
    USING (id)
  ORDER BY year DESC, id DESC
  ```
  Skipping is still proportional to the offset, but it skips index entries,
  not rows.
- **Every page carries cursors, however it was fetched.** A numbered page
  returns the cursors of its first and last rows. So an application can jump to
  page 37 and then page onwards from there by keyset, without repeating or
  skipping rows.
- **A target may limit the depth of numbered pages** (`MaxPage(n)`). A table that
  can grow without bound then cannot be asked to skip millions of rows. A page
  past the limit is a client error that names the limit.

Common to both:

- **The sort is total.** The library appends the target's key (7.2), so a page
  boundary is exact under either mode.
- **The cursor is the library's.** It is an opaque encoding of a row's sort
  values, tagged with the sort and the filter it was made under. A cursor used
  with a different sort or filter is refused, and the client starts again from
  the first page. Films' hand-written `cursor.go` goes away.
- **Both directions.** `Before(cursor)` reads the reverse order and returns the
  page the right way round.
- **A total is a separate call,** because counting reads every matching row:
  - `Count(filter)` is exact.
  - `CountUpTo(filter, n)` stops at `n`, for "more than 1,000", and costs at
    most `n` index entries.

  Numbered pages usually want a count, to draw page links. Keyset pages usually
  do not.

### 7.4 Search

Search comes in three kinds, each using the dialect's native index:

| Kind | Postgres | SQLite | MySQL |
|---|---|---|---|
| `sql.Prefix` | btree on a generated lowercased column | the same | the same |
| `sql.FullText` | a generated `tsvector` with a GIN index | an FTS5 table kept by triggers | a `FULLTEXT` index |
| `sql.Substring` | a `pg_trgm` GIN index | refused | refused |

A kind a dialect cannot serve from an index is refused when the query target is
built. It never falls back to scanning the table.

### 7.5 Every declared query is read by an index its target names

The index that serves a query best is a judgement -- how many rows each column
tells apart, whether the index should carry the columns a page reads so the
query never touches the table, whether one index serves several queries -- so
a query target names the indexes it is read by (`IndexedBy`). An index is its
key columns, whether it is unique, and the columns it includes besides the key;
one index value may serve several targets. `IndexedBy(sql.DerivedIndex)` asks
for the index the declaration implies -- the owner's key or other columns the
scope fixes, then the sort's, then the key -- as a default for a target whose
best index is that one.

`ddl.Explain` asks a server how it would read a page, so a test can say that
each declared sort is read by an index and does not scan the table, except for
sorts declared `Unindexed()`.

### 7.6 The API follows from the declaration

Because a target's filters, sorts and searches are declared, the web layer can
derive its query parameters from them, for example
`?year.atLeast=2000&sort=-year,title&after=…&size=50`, or `page=37` in place of
`after`. It validates them,
refuses an undeclared sort or filter as a client error, and documents them in
OpenAPI and in the generated TypeScript. A client cannot ask for a query the
server did not index.

## 8. Calculated fields

There are four kinds, and each has its own place:

1. **Derived by the domain and not stored** (`IsStale`, `IsWhole`). These are
   methods on the aggregate, not in the schema.
2. **Supplied by the database** (a generated key, a default timestamp). The
   field is marked `Computed()` in the domain schema. The mapping leaves it out
   of inserts and reads it back.
3. **Stored for querying, derived from other columns** (a lowercased title, a
   search vector). These are generated columns (`GENERATED ALWAYS AS (...)
   STORED` in Postgres, MySQL and SQLite), declared by the read model that
   searches with them (section 7.4). The domain does not know they exist, and
   nothing writes them.
4. **Calculated at query time** (a count of viewings, an average rating). These
   are a read model's fields, bound to aggregate expressions (section 9).

## 9. Read models: joins across aggregates

Data that spans aggregates is read, not loaded into an aggregate. A read model
is a schema of what is shown, plus a query over the mappings whose joins come
from the declared references:

```go
var diaryRows = sql.Select(viewings).
	Join(viewings.Ref(ViewingFields.Film)).  // ON viewing.film_id = film.tmdb_id, from section 5
	Fields(DiaryRowSchema,
		DiaryRowFields.Title.From(films.Column(catalog.FilmFields.Title)),
		DiaryRowFields.WatchedAt.From(viewings.Column(ViewingFields.WatchedAt)),
		DiaryRowFields.Viewings.From(sql.Count(viewings.Column(ViewingFields.ID))))
```

- `Join(ref)` derives the `ON` clause from the reference. `LeftJoin(ref)` does
  the same for an optional reference. A join through a join table is
  `Join(lists.Through(f.Films))`, with both keys derived.
- The existing query builder (`sql.InnerJoin`, `sql.LeftJoin`, CTEs) stays
  underneath, for anything the derived joins do not express.

## 10. Constraints

### 10.1 The domain is the only source of rules about values

Constraints are rules about a value wherever it goes, so they belong to the
domain schema. They are `MinLength`, `MaxLength`, `AtLeast`, `AtMost`,
`Pattern`, `OneOf`, `WholeSeconds`/`WholeMinutes`, required versus
`Optional()`, `Identity()`, and `Unique()`. `Unique()` is new: uniqueness across
all values ("one account per email") is a domain rule that only storage can
enforce.

### 10.2 The mapping derives SQL from them

| Domain | SQL |
|---|---|
| required / `Optional()` | `NOT NULL` / nullable |
| `Identity()` | primary key |
| `Unique()` | unique index |
| `MaxLength(n)` | `varchar(n)` where the dialect distinguishes it, plus a check |
| `MinLength(n)` | a check on the length |
| `AtLeast` / `AtMost` | the narrowest integer type that holds the range, plus a check |
| `OneOf(...)` | a check of membership |
| `Pattern(re)` | a check where the dialect has regular expressions (Postgres `~`, MySQL `REGEXP`). SQLite's `REGEXP` works only when the application registers a function for it, so there it is a comment |
| `MinItems` / `MaxItems` | a count on the owner (6.2); otherwise a comment |

This reverses a decision the library made. Today every constraint except a
maximum length is a comment, because an invented `CHECK` is "a rule nobody
asked for". A constraint the domain states is one somebody asked for, so the
reason no longer holds. Enforcing it twice is deliberate: the schema enforces
it on what this program writes, and the database on whatever else writes, such
as a migration, a console or another service. `Enforce(sql.NotesOnly)` keeps
today's behaviour.

### 10.3 The mapping adds no rules about values

A mapping cannot restrict which values are valid. If it could, a value the
domain constructed could fail to store, and correctness by construction would
stop at the storage adapter. So:

- **A dialect that needs a bound the domain does not give** is refused when the
  mapping is built, with a message naming the field. For example, MySQL cannot
  index or uniquely constrain unbounded text, so the refusal reads "`title`
  needs a maximum length to be unique in MySQL". The bound then becomes a
  domain rule, checked when a value is constructed.
- **A representation that cannot hold every domain value** is refused (4.2).
- What a mapping does declare doesn't restrict values: names, representations,
  flattening, and document columns.

## 11. Indexes

Indexes come from two places:

- **The domain implies** the indexes for primary keys, `Unique()`, foreign keys,
  and lazy collections' owner-and-order keys.
- **Read models name** the indexes their queries are read by (7.5), declared or
  asked for as `DerivedIndex`, and the generated columns search needs (7.4).

The migrator collects both into the DDL. An index exists because the domain or
a named query needs it, and it goes when that need does.

## 12. Migrations

The version history kept by the migration tool (`evolve`) records the mapping
as well as the schema. So:

- Renaming a column in the mapping, or a field in the domain, becomes `RENAME
  COLUMN` rather than dropping and adding a column, when the history shows it
  was the same field.
- A change of representation, which renames the column (3.2), is refused
  unless the migration states how existing rows convert.
- A constraint the domain tightens becomes a migration that checks the existing
  rows before adding the constraint.
- An index a read model stops needing is dropped.

## 13. The film, end to end

Declared once, in `domain/catalog`: the aggregate, `FilmFields` and
`FilmSchema` (section 2).

Stored, in `adapter/store`, with only what deviates:

```go
var films = sql.Map(catalog.FilmSchema).Table("films").Column(catalog.FilmFields.ID, "tmdb_id").
	Represent(catalog.FilmFields.Runtime, sql.Minutes)
```

Served, in `transport/api`, with only what deviates:

```go
f := catalog.FilmFields
var FilmShape = catalog.FilmSchema.
	Omit(f.SyncedAt, f.Shape).
	Represent(f.PosterPath, schema.Text(), artworkURL(posterSize), artworkPath(posterSize)).
	Rename(f.PosterPath, "poster").
	Represent(f.BackdropPath, schema.Text(), artworkURL(backdropSize), artworkPath(backdropSize)).
	Rename(f.BackdropPath, "backdrop")
```

The generated TypeScript follows from `FilmShape` under the camelCase policy.

## 14. Decisions

Settled:

- Values are built through their constructor, using field handles (section 2).
- There is one name per field, with a naming policy per target. JSON is
  camelCase (section 3.1).
- A unit is part of a stored name, written as a whole word (section 3.2).
- Deviations are overlays in their own ring, and name fields by handles
  (section 4).
- Value objects are flattened (section 4.3).
- Durations are stored in whole seconds by default, and representations are
  lossless (section 4.2).
- Relations are declared in the domain, and cardinality follows from shape.
  Deletion behaviour is declared on the reference (section 5).
- `Unique()` belongs in the domain. Search indexes belong in read models
  (sections 10 and 11).
- The mapping adds no rules about values (section 10.3).
- Database checks are derived from domain constraints (section 10.2).

Proposed in this revision (sections 6 and 7):

- Collections are eager or lazy, chosen by the domain. A lazy collection is
  never loaded whole: every operation is a query or a statement.
- Person-ordered collections use fractional order keys.
- Every collection is queried the same way (repositories, read models and lazy
  collections): typed filters, declared sorts, keyset and numbered pages,
  library cursors on every page, and totals as a separate, optionally capped
  count.
- A nullable sort field states where its nulls go, and the keyset comparison
  takes them into account.
- Search comes in three kinds, and a kind a dialect cannot index is refused.
- Every query shape is declared, the library derives its indexes, and an
  `EXPLAIN` test checks them.
- A collection rule the database cannot enforce is a domain operation that takes
  the collection as a port.

## 15. Order of work

1. `schema.Object` with field handles, and naming policies. Port the film's
   schema and measure it.
2. `schema projections (`Omit`, `Rename`, `Represent`)` with omit, rename and map, and the JSON policy. Port the film's
   API shape and regenerate the TypeScript.
3. `sql.Map` with names, lossless representations, flattening and derived
   constraints (sections 4 and 10). Port the film's table.
4. References, join tables and derived joins (sections 5 and 9).
5. Queries for every collection, and lazy collections: filters, declared sorts,
   keyset and numbered pages, operations, order keys, and declared query shapes with
   derived indexes (sections 6 and 7). Port the films repository, the
   watchlist and the diary, which replaces films' hand-written cursors.
6. Search kinds and generated columns (sections 7.4 and 8).
7. The versioned mapping in `evolve` (section 12).

Steps 1 and 2 are the agreed spike.
