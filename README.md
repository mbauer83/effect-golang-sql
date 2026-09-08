# effect-golang-sql

Tables, statements and migrations, read off a description.

```go
// The tables a description implies, for three dialects.
statements, err := ddl.Create(ddl.Postgres, PalletSchema.Structure())

// What it has been, and what it takes to get from one version to another.
var Pallets = evolve.Of("logistics.Pallet").
    Starting("1.0.0", PalletSchema.Structure()).
    Then("1.1.0", evolve.Renamed{From: "warehouse", To: "site"})

// Applied to a database, once.
report, err := migrate.Apply[Env](database, migrate.Plan{
    Dialect: ddl.Postgres, History: Pallets, Lock: migrate.PostgresAdvisory,
})
```

Built on [effect-golang-schema](https://github.com/mbauer83/effect-golang-schema):
one description decodes a row, names the columns, binds the arguments, becomes
the tables and carries the migrations. A column that changed would change all of
them together, which is the whole reason there is one description rather than
five.

**This module depends on a port, not on a driver.** Nothing here imports one, so
a program using it chooses its own and acquires nothing else. The three drivers
in `go.mod` are test-only.

| Area | State |
|---|---|
| [SQL: statements, rows, transactions](docs/reference/sql.md) | usable |
| [DDL: Postgres, MySQL/MariaDB, SQLite](docs/reference/ddl.md) | usable; the two asked-for dialects are executed in CI only |
| [Migrations: declared steps, both directions](docs/reference/evolve.md) | usable |
| [Migrator: ledger, ordering, advisory lock](docs/reference/migrate.md) | usable; no drift check |

## Layout

```text
sql/                        statements, rows decoded by a Schema, transactions
ddl/                        the tables an aggregate is, for three dialects
evolve/                     the steps between versions, and the values across them
migrate/                    applying a history to a database, once
examples/library/           a repository over the port, driver-free
examples/warehouse/         an aggregate, the tables it becomes, and its history
examples/cmd/sqldemo/       the examples as a runnable command
test/unit/                  behaviour of the public API
test/acceptance/            the example programs, against a real database
test/architecture/          the claims about this module's shape
docs/reference/             what each part is and why it is that way
```

Dependencies point one way and an architecture test checks it: `evolve` says
what changed, `ddl` says how to spell it, `sql` runs statements, and only
`migrate` knows all three.

## The one untyped file

`sql/driver_values.go` is the single place this module holds a value it cannot
name. `database/sql` scans into a top type and a driver hands one back, because
a driver cannot know what a column holds until it reads it. Everything above
that line works in the universal representation, and an architecture test checks
that the exemption stays where it says it is and stays used.

## Running the examples

```sh
go run ./examples/cmd/sqldemo
```

The example suites run against SQLite, which needs nothing installed. Postgres
and MySQL are gated on `EFFECT_GOLANG_POSTGRES_URL` and
`EFFECT_GOLANG_MYSQL_URL`, and run in CI against service containers.

## Development

Neither `effect-golang` nor `effect-golang-schema` is published yet, so `go.mod`
resolves both from sibling working copies:

```text
workspace/
  effect-golang/            the runtime
  effect-golang-schema/     descriptions
  effect-golang-sql/        this module
  effect-golang-web/        transports, on both
```

A `replace` is ignored by anything that depends on *this* module, so it is a
development arrangement and not a distribution one. Replace both with version
requirements once they are tagged.
