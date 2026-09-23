# effect-golang-sql

Tables, statements and migrations, read off a description.

```go
// The tables a description implies, for three dialects.
statements, err := ddl.Create(ddl.Postgres, PalletSchema.Structure())

// What it has been, and what it takes to get from one version to another.
var Pallets = evolve.Of("logistics.Pallet").
    Start("1.0.0", PalletSchema.Structure()).
    Then("1.1.0", evolve.Rename{From: "warehouse", To: "site"})

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
| [SQL: a typed query specification, rows, transactions](docs/reference/sql.md) | usable |
| [DDL: Postgres, MySQL/MariaDB, SQLite](docs/reference/ddl.md) | usable; the two asked-for dialects are executed in CI only |
| [Migrations: declared steps, both directions](docs/reference/evolve.md) | usable |
| [Migrator: ledger, ordering, advisory lock](docs/reference/migrate.md) | usable; no drift check |

## Layout

```text
sql/                        a typed query specification, rows decoded by a
                            Schema, transactions
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
what changed, `sql` says what a statement *asks*, `ddl` says how a server
spells it, and only `migrate` knows all three. `ddl` depends on `sql` because
every dialect is a `sql.Spelling`: a query is stated once and spelled by the
server it will run on, so nothing above this line writes a placeholder, an
upsert clause, or a function one of the three names differently.

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

`go.mod` requires the runtime and the descriptions by version, so what a
consumer resolves is what this module was built against. Working on several at
once is a workspace's job:

```text
workspace/
  go.work                   where the modules being worked on are
  effect-golang/            the runtime
  effect-golang-schema/     descriptions
  effect-golang-sql/        this module
  effect-golang-web/        transports, on both
```

```sh
cd workspace
go work init ./effect-golang ./effect-golang-schema ./effect-golang-sql ./effect-golang-web
```

The `go.work` file is not checked in to any of them: it belongs to whoever has
several checked out at once, which is why it lives above all four. A `replace`
cannot do this job. It is ignored by anything that depends on the module
carrying it, so it says nothing to a consumer and only ever describes one
person's layout -- and it hides the requirement a consumer will actually
resolve.

The four modules are versioned together and tagged in dependency order:
[RELEASING.md](https://github.com/mbauer83/effect-golang/blob/main/RELEASING.md).
