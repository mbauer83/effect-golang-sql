# DDL reference

`schema/ddl` projects an aggregate's description into the tables that hold it.

```go
statements, err := ddl.Create(ddl.Postgres, PalletSchema.Structure())
statements, err := ddl.Drop(ddl.MySQL, PalletSchema.Structure())
tables, err := ddl.Tables(ddl.Postgres, PalletSchema.Structure())  // as data
```

## An aggregate is not one table

An object with an **identity** is an entity and gets a table of its own. An
object without one is a **value** belonging to whatever holds it, and lives in
that thing's row. So a pallet with a list of items projects to two tables and
the foreign key between them, while the warehouse it sits in is a column.

That is the finding [§7.2](https://github.com/mbauer83/effect-golang-web/blob/main/architecture-plan.md#72-prior-art-a-previous-attempt-at-exactly-this)
records, and a table-per-declaration design would have got it wrong.

The tables come back **parent first**, which is the order the statements have to
run in: a child cannot reference a table that is not there yet. `Drop` reverses
it.

The child carries what it needs and nothing more:

- a reference column named for its parent and that parent's key — `Pallet_id`;
- a **cascading** foreign key, because a child entity has no life without its
  root and a row that outlived its parent would be unreachable;
- an **index** on it, because looking a parent's children up is a question the
  schema itself asks;
- a `position` column when the field was a **list**, because a list is ordered
  and a table is not — without it, the list read back would not be the list
  written.

Those last two are derived, so a description that already has a column of that
name is **refused** rather than getting two or one silently overwritten.

## The three dialects

`Postgres` and `MySQL` are what this is for. `MySQL` covers MariaDB, which
agrees about everything here. `SQLite` is a third, and a deliberate one: it runs
everywhere these tests run, so the derivation can be *executed* rather than
compared to expected strings.

They are not one dialect with different keywords:

| | Postgres | MySQL | SQLite |
|---|---|---|---|
| unbounded text | `TEXT` | `LONGTEXT` | `TEXT` |
| bounded text | `VARCHAR(n)` | `VARCHAR(n)` | `TEXT` |
| boolean | `BOOLEAN` | `TINYINT(1)` | `INTEGER` |
| bytes | `BYTEA` | `LONGBLOB` / `VARBINARY(n)` | `BLOB` |
| instant | `TIMESTAMPTZ` | `DATETIME(6)` | `TEXT` |
| document | `JSONB` | `JSON` | `TEXT` |
| generated key | `BIGINT GENERATED ALWAYS AS IDENTITY` | `BIGINT NOT NULL AUTO_INCREMENT` | `INTEGER` |
| now | `CURRENT_TIMESTAMP` | `CURRENT_TIMESTAMP(6)` | `(STRFTIME(…))` |
| a bound value | `$1`, `$2`, … | `?` | `?` |
| replacing a row | `ON CONFLICT (k) DO UPDATE SET c = EXCLUDED.c` | `AS incoming ON DUPLICATE KEY UPDATE c = incoming.c` | as Postgres |
| concatenating | `a \|\| b` | `CONCAT(a, b)` | `a \|\| b` |
| a substring | `SUBSTRING(a FROM b FOR c)` | `SUBSTRING(a, b, c)` | `SUBSTR(a, b, c)` |
| counting characters | `LENGTH(a)` | `CHAR_LENGTH(a)` | `LENGTH(a)` |
| joining a group | `string_agg(a, ',')` | `group_concat(a separator ',')` | `group_concat(a, ',')` |
| seconds between | `extract(epoch from (a - b))` | `timestampdiff(second, b, a)` | `((julianday(a) - julianday(b)) * 86400)` |
| a regular expression | `a ~ b` | `a regexp b` | **none** |

Everything from "a bound value" down is why a `Dialect` is also a
`sql.Spelling`: a query is stated once and spelled by the dialect that will run
it, so a store never writes a placeholder, an upsert clause, or a function one
of the three names differently. MySQL's row alias is what 8.0.19 and later
offer in place of the deprecated `values()`; the alternative to an upsert at all
is a read, a branch and a write, which is two round trips and a race between
them.

Three of those rows are load-bearing beyond tidiness. **`char_length`**,
because MySQL's `length` counts bytes: a store that had written `length` would
have been right on two servers and quietly wrong on the third for every string
that was not ASCII. **`timestampdiff`**, because MySQL takes the earlier moment
first, so the arguments are read the other way round — stated once in the
dialect rather than by every caller who has to remember which server it is
talking to. And **the empty cell**: SQLite has no regular expression unless the
program that opened the database registered one, so it answers nothing and a
query that asks for one is refused with the dialect named. A program that did
register one says so with `sql.Also`, and nothing in this module changes.

A dialect answers about the operations it does *differently*; everything
ordinary — `lower`, `trim`, `count`, `max`, `coalesce`, the six comparisons,
`like`, arithmetic, `over` — carries its own spelling, so a dialect answering
thirty questions in order to disagree about six is not what this asks for. See
[Operations](sql.md#operations-and-how-a-dialect-is-taught-one).

Some of those choices are load-bearing rather than stylistic. `timestamptz`
because an instant without a zone is a time nobody can place. `datetime(6)`
rather than MySQL's `timestamp`, which is bounded by 1970 and 2038 and rewritten
into the session's time zone — and with the precision spelled out, because
`current_timestamp` without it fills a `datetime(6)` to the second and loses the
microseconds silently. `engine=innodb default charset=utf8mb4` because MySQL's
defaults have changed between versions, and a schema that did not say would mean
different things on two servers. SQLite's identity is a plain `integer` because
a column of that type which is the sole primary key **is** an alias for the
rowid, so it is assigned when a row is inserted without one.

## What is refused, and why refusing beats guessing

| refused | because |
|---|---|
| an object with no identity | its rows could not be found again |
| an unnamed object | a name taken from the field holding it would change when that field did |
| a computed column with no default | a column with no value and no default is one no row can be written for, and an invented default is a rule nobody asked for |
| an identity with parts, or one that may be absent | an identity is one value, and one that may be absent identifies nothing |
| a derived column colliding with a declared one | two columns of one name, or one silently overwritten |
| **Postgres**: an unsigned 64-bit column | Postgres has none, and a decimal that held the values would not be an integer — a key that was fast would quietly stop being one |
| **MySQL**: an unbounded string *key* | MySQL rejects a TEXT column in a key specification outright, and a prefix length invented here would make two different keys equal whenever they agreed for that many characters |

The small unsigned types are **widened** rather than refused on Postgres —
`uint8` to `SMALLINT`, `uint32` to `BIGINT` — because every value still fits and
the column is still an integer. That is not approximating.

An integer whose constraints state a range is as **narrow** as that range: a
year stated as 0 to 9999 is a `SMALLINT` whatever Go type holds it, because the
column is sized by what the domain promised rather than by the Go type it
happens to be. It is never wider than the width declared, and a range stated
on an unsigned 64-bit integer that fits a signed width makes it storable on
Postgres too. The tightest bounds decide, whatever order they were stated in,
and a bound the narrowed type does not keep is checked.

A map of entities is stored as one document column rather than becoming a table,
because the key would need a column and the description does not say what to
call it. Inventing a name would put it in the schema forever.

## Defaults

`Computed` says a value is not the caller's; that is all a
[derived shape](https://github.com/mbauer83/effect-golang-schema/blob/main/docs/reference/variant.md) needs. A table needs the other half, so the
description says it:

```go
schema.FieldAt("storedAt", schema.Time(), at).Computed().WithDefaultNow()
schema.FieldAt("status", schema.Text(), at).WithDefault(dynamic.OfText("new"))
```

A closed set — a value or *now* — rather than a SQL string, because a string
would be one dialect's spelling inside a description meant to outlive the choice
of dialect. A generated identity needs none: the database's own key generation
is what produces it.

`on update current_timestamp` is **not** here. MySQL has it and Postgres needs a
trigger, so an `updated_at` that worked on one and silently did nothing on the
other would be worse than not offering it.

## A constraint the description states is a check

Each constraint the description states becomes a named `CHECK` on its column:
a bound (`"quantity" >= 1`), a length (`CHAR_LENGTH("name") >= 1`, or `LENGTH`
on SQLite), and a pattern where the dialect has regular expressions (`~` on
Postgres, `REGEXP` on MySQL). A constraint the description states is a rule
somebody asked for, so the table keeps it too: the schema keeps it on what this
program writes, and the check keeps it on whatever else writes -- a migration, a
console, another program.

A check is named `table_column_rule` -- `label_name_min_length` -- so a refusal
says which rule a row broke, and a name longer than Postgres takes is shortened
with a hash of the whole so it stays unique.

What a dialect cannot check is a comment, which is honest about not being
enforced: a format such as `uuid`, and a pattern on SQLite, whose `REGEXP` works
only when the application registers a function for it.

A bound the **column type already keeps** is neither checked nor commented: a
description states the range its width implies because JSON Schema has no
integer widths, but a column typed `INTEGER` keeps it in the type, and a
bounded `VARCHAR(n)` keeps its maximum length. Exact rather than a guess -- only
a bound that is precisely the type's own limit is skipped, so a narrower one the
author asked for is checked.

A column added by a migration carries its checks. A constraint that changes on a
column that already exists is not migrated yet: the checks are written when a
table or a column is made.

## References are foreign keys

A field whose schema is `schema.Ref(target, identity)` refers to another
aggregate, and its mapping says where that aggregate is stored
(`sql.Map(...).Referring(films)`). The column gets a foreign key to the
target's table and key column, and an index to join on:

```sql
FOREIGN KEY ("film") REFERENCES "film" ("tmdb_id")
```

- **Deleting the target is refused by default.** `schema.Cascade` deletes what
  refers to it as well, and `schema.SetNull` empties the reference -- refused
  where the reference is required, since it could not be emptied.
- **A reference whose target the mapping was not told about is refused**, with
  the message saying to name its mapping with `Referring`, rather than made into
  a column that refers to nothing.
- **SQLite enforces foreign keys only when the connection asks it to**
  (`PRAGMA foreign_keys = ON`, or `_pragma=foreign_keys(1)` in modernc's
  connection string). Postgres and MySQL always do.
- A list of references is not yet a join table: it is stored as a document
  column, and its foreign keys are not enforced.

`ddl.Join(from, to)` and `ddl.LeftJoin(from, to)` join two tables on the one
foreign key between them, in whichever direction it points, so a join's
condition is never written by hand; `ddl.KeyCondition` is the same condition
for sources a query has aliased. The condition's columns are always qualified,
because both ends of a key are often called the same. No key, or more than one,
is refused rather than guessed.

## Alter, and what it will not project

`ddl.Alter(dialect, history, from, to)` is the statements that carry an
aggregate from one version to another. It **refuses** a history whose steps
include a [recomputation](evolve.md#the-fifth-change-when-values-have-to-be-computed):
one of those moves rows with a Go function, so there is no statement list that
is the whole of it, and returning the structural half would be returning a
migration that silently does not migrate. `migrate` plans those.

## Not idempotent, deliberately

There is no `if not exists`. It would make the tables skippable and leave the
indexes failing on a second run, because MySQL has no such clause for an index —
a schema that half re-ran would be worse than one that did not. These statements
make a schema once. Changing an existing one is a **migration**, which
[§7.1](https://github.com/mbauer83/effect-golang-web/blob/main/architecture-plan.md#71-ddl-and-migrations-researched-not-built)
records the approach for and which is not built.

## What the tests establish

The statements are **run**, not compared to strings: against SQLite in the
ordinary suite, where the schema is then asked what it holds — the generated key
is assigned without being given, the default applies with nobody supplying it,
the foreign key cascades, and a child with no parent is refused.

Postgres and MySQL are gated on `EFFECT_GOLANG_POSTGRES_URL` and
`EFFECT_GOLANG_MYSQL_URL`, and run in CI against service containers. Those tests
**skip** where no database is reachable, and a skipped test is not evidence.
[`examples/warehouse`](../../examples/warehouse/warehouse.go) is the aggregate.
