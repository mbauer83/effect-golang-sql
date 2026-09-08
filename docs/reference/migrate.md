# Migrator reference

`migrate` applies an aggregate's [history](evolve.md) to a database, once.

```go
report, err := migrate.Apply[Env](database, migrate.Plan{
    Dialect: ddl.Postgres,
    History: warehouse.Pallets,
    Target:  "3.0.0",                    // empty means the latest
    Ledger:  "schema_version",           // empty means that
    Lock:    migrate.PostgresAdvisory,   // nil means no coordination
})

migrate.Current[Env](database, plan)     // where a database is, without moving it
```

A `Plan` rather than five positional arguments, because four of them would be
strings and interfaces easy to hand over in the wrong order — and because the
optional two then have somewhere to be optional.

## What it does

1. Makes the ledger if it is not there — the one table this module creates *if
   absent*, because it has to exist before anything can be read about what
   exists.
2. Takes the lock, if the plan has one.
3. Reads which version the database holds of this aggregate.
4. **Nothing at all** if that is the target. Running twice is running once.
5. **Creates** the tables as of the target if the database holds none — a
   database that starts at `3.0.0` has not skipped anything, it simply never had
   `1.0.0` to alter.
6. Otherwise **steps** through every version between, recording each as it
   completes.

Either direction. A target earlier than the recorded version runs the inverses,
which is a thing to do knowingly: some of them cannot restore what they dropped.

`Report` says what happened — `From`, `To`, the `Applied` path, whether it
`Created`, and `Nothing()` for the common case of there being nothing to do.

## One transaction, and what that is worth per dialect

Everything happens inside one transaction: the lock, the ledger read, the
statements, and each version's record.

- **Postgres** and **SQLite** have transactional DDL, so the whole migration is
  atomic. A failure at the fourth step leaves a database still at the version it
  started from, and the ledger still saying so.
- **MySQL** commits its DDL as it goes and cannot offer that. What it does offer
  is that the lock is held throughout and the ledger says which step was the
  last to finish — so running again continues from there rather than from the
  beginning. This is the most a migrator can do where the database will not help
  it, and pretending otherwise would be worse than saying it.

That is also why each version is recorded separately rather than once at the
end: where DDL rolls back the distinction does not matter, and where it does not,
the ledger is the difference between continuing and starting over.

## The ledger

One row per aggregate, holding the version its tables are at:

| aggregate | version |
|---|---|
| `logistics.Pallet` | `3.0.0` |

**Not one row per step.** A step is derived from the history and the history is
code, so what a database has to remember is where it got to, not what the steps
were. That is also what keeps a history editable: inserting a version between
two released ones is a code change and not a rewriting of somebody's ledger.

The table name is configurable, because a database may already have one of that
name, or a convention of its own, or two applications sharing a schema that each
want their own. Its rows are read through `RecordedSchema`, by the same
machinery every other row is.

## Coordinating several instances

Every replica starting at once and all of them altering the same table is the
ordinary failure this closes. `Lock` is one statement to take an advisory lock
and one to give it back, taken on the same connection the migration runs on —
which is what the transaction provides — and nothing else.

```go
migrate.PostgresAdvisory   // pg_advisory_xact_lock, freed when the transaction ends
migrate.MySQLNamed         // get_lock / release_lock, ten-second wait
nil                        // no coordination
```

Postgres's is transaction-scoped, so the database frees it however the
transaction ends — one fewer thing to get wrong than releasing it by hand. The
key is a hash of the aggregate's name, written out rather than taken from
`hash/fnv` because the value has to be stable across releases of this package:
two instances that hashed the same name differently would take two different
locks and both proceed.

MySQL's is session-scoped, which turns out to be what is needed: its DDL commits
as it goes, so a transaction-scoped lock would be freed by the first `ALTER` and
the next instance could walk in behind it. It waits ten seconds and then returns
*no* rather than failing, which is reported as not having got the lock.

SQLite needs none — it has one writer.

**Deliberately small.** Leader election, leases and heartbeats are a much larger
problem and somebody else's. An advisory lock guards this migration against
another instance of the same migration and nothing else: it does not stop
somebody altering the table by hand, and it is not a substitute for one process
being responsible.

## What is not here

- **The drift check.** Nothing asserts that the live database actually matches
  the declaration at the recorded version, so a schema altered by hand is a
  schema this will migrate from a state it does not describe. That is the one
  thing declarative diffing does better, and the intended answer — compare and
  refuse — is recorded and unbuilt.
- **Choosing the dialect for you.** The dialect has to be the one the database
  actually is, and that is the single thing here nothing can check.

## What the tests establish

Against a real database: the first migration creates and records; a second run
does nothing; a third steps from where the second got to, across two evolutions,
recording each version on the way; the ledger reads on its own; going backwards
works and reports its path; a step SQLite cannot perform leaves the ledger still
saying something true; and a configured ledger name is the table that gets
written.

Postgres and MySQL are exercised in CI against service containers.
[`examples/warehouse`](../../examples/warehouse/history.go) is the history.
