# Migration reference

`schema/evolve` carries a description from one version to the next, and the
values with it. `ddl.Alter` turns the same steps into statements.

```go
var Pallets = evolve.Of("logistics.Pallet").
    Starting("1.0.0", PalletSchema.Structure()).
    Then("1.1.0",
        evolve.Renamed{From: "warehouse", To: "site"},
        evolve.Added{Field: structure.Field{
            Name:    "handling",
            Node:    schema.MaxLength(schema.Text(), 32).Structure(),
            Default: structure.DefaultTo{Value: dynamic.OfText("standard")},
        }},
    ).
    Then("2.0.0", …)

Pallets.Versions()                                    // in declared order
Pallets.At("2.0.0")                                   // the derived description
Pallets.Migrate("1.0.0", "2.0.0", value)              // the value, carried
ddl.Alter(ddl.Postgres, Pallets, "1.0.0", "2.0.0")    // the statements
ddl.Alter(ddl.Postgres, Pallets, "2.0.0", "1.0.0")    // and back
```

## Versions are named, not numbered

A position would renumber every later version whenever one was inserted, and it
would give a document tagged `2.1.0` nothing to match against but a convention —
where a name is what the document, the service that wrote it and the service
that reads it already agree on.

No scheme is imposed: `1.0.0` and `logistics.Pallet.v2` are both names, and the
order is the order they are declared in. `Versions()` hands that order back, and
a name used twice is refused, because two versions of one name is two things a
document tagged with it could mean.

## Declared, not diffed

The industry splits two ways, and [Atlas names them](https://atlasgo.io/concepts/declarative-vs-versioned):
**versioned**, a script per change, and **declarative**, a desired state
compared against whatever the database happens to hold — which Atlas is candid
is *non-deterministic*, because the plan depends on the state it finds.

This is neither. A step is a **declared** list of changes between two pinned
versions. Nothing is inferred, so a plan is the same every time, and a rename is
exact.

That last point is the whole argument. `drizzle-kit`, the most developed tool in
the TypeScript ecosystem, [cannot tell a rename from a drop-and-add](https://github.com/drizzle-team/drizzle-orm/issues/6053):
it asks interactively, the prompt has open bugs, its programmatic API throws
when a diff contains both a create and a delete, and choosing "renamed" emits
the rename while [dropping the accompanying type change](https://github.com/drizzle-team/drizzle-orm/issues/5499).
Declared, it is simply known — and the acceptance suite writes a row, migrates,
and reads the value back under its new name, which a drop-and-add would have
lost.

## Only version one is written

Every later version is **derived** by applying the steps. There is no second
declaration to disagree with them, which is the objection that would otherwise
apply: writing V1, V2 and a function between them means something has to check
that the function really takes one to the other.

That refinement comes from [Cambria](https://www.inkandswitch.com/cambria/),
which generates its type definitions from the lens definitions themselves.
Materialise a derived version with [`schemagen`](https://github.com/mbauer83/effect-golang-schema/tree/main/schemagen) if a Go type
for it is wanted, the way any other generated artifact is materialised.

## Four changes, and one of them is the point

```go
evolve.Added{Field: structure.Field{…}}
evolve.Removed{Name: "legacyCode"}
evolve.Renamed{From: "depot", To: "warehouse"}
evolve.Retyped{Name: "count", Node: schema.Int64().Structure()}
```

A closed set, so a projection switches over it and knows it has covered
everything — and so a step cannot contain something nobody taught the DDL to
write. `Renamed` is the member a diff cannot see.

`Added` refuses a field that is neither optional nor defaulted: the rows that
already exist have no value for it, and a database will not add such a column to
a table that is not empty. `Retyped` says what the new shape is and lets the
projection say what it costs, because whether a change of type is safe is the
database's business and differs by dialect.

## Both directions, from N−1 steps

Each change knows its inverse, so every one of the N² version pairs is answered
by composition — forward by concatenating the steps, backward by inverting each
change and **reversing twice over**: the steps in reverse, and the changes within
each step in reverse too. A step that renamed one field and added another has to
drop the addition before undoing the rename, or the inverse would be looking for
a field under a name it no longer has.

Nothing is precomputed. The plan imagined generating the N² typed compositions,
as the TypeScript prior art builds them at runtime — but that was needed there
because it had a Go-type equivalent *per version*. Here values cross as the
universal representation, typed at the edges and untyped in the middle, exactly
as the [protobuf](https://github.com/mbauer83/effect-golang-schema/blob/main/docs/reference/protobuf.md) codec is. So there is nothing to generate and
nothing to erase, and folding a handful of changes costs less than remembering
the answer.

## The fifth change: when values have to be computed

The four derive their own value migration, because moving a member needs no
function. **Computing** one does — a field split into two, two merged into one,
a count that was text becoming a number, metres becoming millimetres — and
there is no deriving that. So `Rewritten` carries the how:

```go
var splittingTheReference = evolve.Rewritten{
    Doing: "splitting the reference into a prefix and a serial",
    // What has to exist before the values move.
    Adding: []evolve.Change{
        evolve.Added{Field: …prefix…}, evolve.Added{Field: …serial…},
    },
    // What goes once they have.
    Dropping: []evolve.Change{evolve.Removed{Name: "reference"}},
    Forward: evolve.Rewrite{
        Value: splitReference,   // one value, in memory
        Rows:  splitRows,        // the rows a database already holds
    },
    Back: evolve.Rewrite{Value: joinReference, Rows: joinRows},
}

func splitRows(ctx context.Context, within sql.Querying) error {
    // Read, split in Go, write back -- and query, compute or call out to
    // something else if that is what the change needs.
}
```

**Two structural lists, not one.** A computation needs both ends present while
it runs: the targets have to exist before the values move, and the sources
cannot go until after. The first version of this had one list and ran the
statements last — so the split read a column that had already been dropped, and
SQLite silently treated `"reference"` as a *string literal* because the
identifier no longer resolved. Two lists make that unmakeable rather than
something an author has to remember. Going back reverses both lists and both
directions, so the same declaration reads correctly either way.

**`Rows` is a function, not a list of statements**, and that is the difference
that matters. It gets **the transaction the migration is running in**, so it can
read what the migration has already done, compute in Go, write back, and call
out to something else — a lookup service, a checksum, a unit conversion nobody
can express in SQL. What it writes is committed or rolled back with everything
else, where the database allows that.

As statements this split would have been *three* declarations of one change —
Postgres has `split_part`, MySQL `substring_index`, SQLite `substr` with
`instr` — each to be got right separately, and none of them able to do anything
a statement cannot. As a function it is one, and the plan's shape is identical
on every dialect.

The consequence: **`ddl.Alter` refuses a history containing a rewriting**,
because there is no statement list that is the whole of it. `migrate` walks the
same steps and runs the function between the two structural lists;
`migrate.Actions` is the dry run, and an action with no statement is one whose
effect can only be read in the code it names.

**`Back` may be empty**, which says the change cannot be undone — averaging two
columns into one loses which was which — and a migration that would need to go
back through it says so rather than doing half of it.

## What a down migration cannot do

**Invent data.** Dropping a column loses what was in it, so putting the column
back restores the shape and not the values — and the restored field is made
*optional*, because a required column with no values is one no row satisfies.

A rename's inverse is a rename, which is the one inverse in the set that loses
nothing at all.

## One declaration, two projections

The same changes produce the statements that move the table *and* the function
that moves a value. So a value carried forward in memory is a value the migrated
table accepts — a property that would otherwise be two things kept in step by
hand. The acceptance suite checks exactly that: migrate a value, migrate the
table, write the one into the other.

`Migrate` leaves a `Retyped` field's value alone. Converting it would mean
guessing how — a number to a string is a format nobody stated, a string to a
number is a parse that may fail — so what comes out is checked against the
target version's schema, which is where a value that no longer fits is reported.

## What Go cannot give this

**Compile-time totality.** A version is a *value* in Go, not a type, so the
compiler cannot check that a step accounts for every column of its target;
encoding a version's columns in types would need a type parameter per column and
be unusable. What is available is **assembly-time** totality: a `History`
carries a `Fault` and answers nothing until it is clear, the way an ambiguous
route and an endpoint declaration are checked. A test that asks `Fault` is a test
that has checked it. Scala or Haskell could do better here; Go cannot, and this
does not pretend otherwise.

**Drift reconciliation.** A declared chain cannot know that someone altered
production by hand — the one thing declarative diffing does better. The intended
answer keeps determinism: before applying N→N+1, assert the live database
matches the declaration at N and refuse rather than proceed. **That check is not
built.**

## Dialects

`Renamed`, `Added` and `Removed` are spelled the same by Postgres and MySQL.
`Retyped` is not, and the difference is not cosmetic: Postgres's
`alter column x type y` changes the type and leaves the rest of the definition
alone, where MySQL's `modify column x y …` restates the whole definition — so
anything left out of the restatement is lost. SQLite **refuses** it outright,
because its `ALTER TABLE` cannot do it: the way is a new table, a copy, a drop
and a rename, which is four statements and a decision about values that no
longer fit.

`Alter` covers the aggregate's **root table** only. A change to a nested entity
is a change to that entity's description, so it has a history of its own — a step
that silently reached into a child would be one whose effect depended on where
the change happened to be written.

## Running them: see `migrate`

`evolve` says what changed and `ddl` says how to spell it; neither touches a
database or remembers anything. [`migrate`](migrate.md) is the part that does —
the ledger, the ordering, the lock, and running twice being running once.

## Scope

- The **drift check** described above is not built.
- A **merge** is expressible with `Rewritten` and is not demonstrated; the
  example splits.

[`examples/warehouse`](../../examples/warehouse/history.go) is the history.
