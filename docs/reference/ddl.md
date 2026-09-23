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
| unbounded text | `text` | `longtext` | `text` |
| bounded text | `varchar(n)` | `varchar(n)` | `text` |
| boolean | `boolean` | `tinyint(1)` | `integer` |
| bytes | `bytea` | `longblob` / `varbinary(n)` | `blob` |
| instant | `timestamptz` | `datetime(6)` | `text` |
| document | `jsonb` | `json` | `text` |
| generated key | `bigint generated always as identity` | `bigint not null auto_increment` | `integer` |
| now | `current_timestamp` | `current_timestamp(6)` | `(strftime(…))` |
| a bound value | `$1`, `$2`, … | `?` | `?` |
| replacing a row | `on conflict (k) do update set c = excluded.c` | `as offered on duplicate key update c = offered.c` | as Postgres |
| concatenating | `a \|\| b` | `concat(a, b)` | `a \|\| b` |
| a substring | `substring(a from b for c)` | `substring(a, b, c)` | `substr(a, b, c)` |
| counting characters | `length(a)` | `char_length(a)` | `length(a)` |
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
`uint8` to `smallint`, `uint32` to `bigint` — because every value still fits and
the column is still an integer. That is not approximating.

A map of entities is stored as one document column rather than becoming a table,
because the key would need a column and the description does not say what to
call it. Inventing a name would put it in the schema forever.

## Defaults

`Computed` says a value is not the caller's; that is all a
[derived shape](https://github.com/mbauer83/effect-golang-schema/blob/main/docs/reference/variant.md) needs. A table needs the other half, so the
description says it:

```go
schema.FieldOf("storedAt", schema.Time(), get, set).Computed().WithDefaultNow()
schema.FieldOf("status", schema.Text(), get, set).WithDefault(dynamic.OfText("new"))
```

A closed set — a value or *now* — rather than a SQL string, because a string
would be one dialect's spelling inside a description meant to outlive the choice
of dialect. A generated identity needs none: the database's own key generation
is what produces it.

`on update current_timestamp` is **not** here. MySQL has it and Postgres needs a
trigger, so an `updated_at` that worked on one and silently did nothing on the
other would be worse than not offering it.

## What DDL cannot state becomes a comment

Constraints and formats are emitted as comments. Two enforcements would be two
rules to keep in step, every dialect spells a `CHECK` differently, and the schema
layer already keeps these on the way in and on the way out — so the table says
what it cannot keep.

A bound the **column type already keeps** is left out: a description states the
range its width implies because JSON Schema has no integer widths, but a column
typed `integer` says it in the type, and restating it would be noise that looked
like a rule somebody chose. Exact rather than a guess — only a bound that is
precisely the width's own limit is skipped, so a narrower one the author asked
for survives.

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
