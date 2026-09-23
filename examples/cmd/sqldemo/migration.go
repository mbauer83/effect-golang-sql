package main

// Applying the pallet's history to a database, with rows in it.
//
// The interesting step is the one that cannot be statements: 3.1.0 splits a
// reference into a prefix and a serial by reading the rows and writing them
// back, in Go, inside the migration's own transaction. So the demo puts two
// pallets in at 3.0.0 and reads them out again afterwards -- the split is
// visible in the data or it did not happen.

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/migrate"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// migrationOutcome is what one run of the migrator has to report.
type migrationOutcome struct {
	structural migrate.Report
	rewrite    migrate.Report
	again      migrate.Report
	pallets    []dynamic.Value
}

// splitSchema is the projection the demo reads back. A description and not a struct,
// because three columns of a table in mid-history are not worth a Go type.
var splitSchema = schema.Struct[dynamic.Value]("Split",
	schema.DynamicField("prefix", schema.Text()),
	schema.DynamicField("serial", schema.Text()),
	schema.DynamicField("site", schema.Text()),
)

// runMigration applies the history to an empty database, stores two pallets,
// crosses the recomputation, and applies it a third time -- which does nothing,
// the property a migrator has to have.
//
// 3.1.0 and not the latest version, because 4.0.0 changes a column's type and
// SQLite cannot: that step is for a database this demo does not require.
func runMigration(runtime *effect.Runtime, workspace string) {
	source := "file:" + filepath.Join(workspace, "migration.db")
	structural := migrate.Plan{Dialect: ddl.SQLite, History: warehouse.Pallets, Target: "3.0.0"}
	rewrite := migrate.Plan{Dialect: ddl.SQLite, History: warehouse.Pallets, Target: "3.1.0"}

	program := effect.Scoped(func(scope effect.Scope) migrateEffect[migrationOutcome] {
		return effect.Gen(func(do *effect.Do[effect.Unit, migrate.Fault]) migrationOutcome {
			database := do.Await(open(scope, source))
			structuralReport := do.Await(migrate.Apply[effect.Unit](database, structural))
			do.Await(storePallet(database, "P-1", "Kiel"))
			do.Await(storePallet(database, "P-22", "Aarhus"))
			return migrationOutcome{
				structural: structuralReport,
				rewrite:    do.Await(migrate.Apply[effect.Unit](database, rewrite)),
				again:      do.Await(migrate.Apply[effect.Unit](database, rewrite)),
				pallets:    do.Await(effect.RunCollect(pallets(database))),
			}
		})
	})

	within, giveUp := context.WithTimeout(context.Background(), 20*time.Second)
	defer giveUp()

	exit := runtime.Run(within, effect.Unit{}, program)
	report, ok := exit.Value()
	if !ok {
		fail(fmt.Errorf("migrating: %v", exit))
	}
	fmt.Printf("\nmigrating: created=%v to %s, applied %v\n",
		report.structural.Created, report.structural.To, report.structural.Versions)
	fmt.Printf("  then the rewriting: to %s, applied %v\n",
		report.rewrite.To, report.rewrite.Versions)
	fmt.Printf("  again: nothing to do = %v\n", report.again.IsEmpty())
	for _, pallet := range report.pallets {
		fmt.Printf("  a pallet: %s\n", membersOf(pallet))
	}
}

// storePallet puts one pallet in, in the columns version 3.0.0 has.
func storePallet(database sql.Querier, reference string, site string) migrateEffect[sql.Outcome] {
	return sql.Execute[effect.Unit](database,
		`insert into "Pallet" ("reference", "site") values (`+ddl.SQLite.Placeholder(1)+`, `+ddl.SQLite.Placeholder(2)+`)`,
		dynamic.OfText(reference), dynamic.OfText(site),
	).MapError(func(fault sql.Fault) migrate.Fault {
		return migrate.Fault{Op: "store a pallet", Err: fault}
	})
}

// pallets reads what the split left, in the order the serials sort.
func pallets(database sql.Querier) effect.Stream[effect.Unit, migrate.Fault, dynamic.Value] {
	return effect.MapStreamError(
		sql.Query[effect.Unit](database, splitSchema,
			`select "prefix", "serial", "site" from "Pallet" order by "serial"`),
		func(fault sql.Fault) migrate.Fault {
			return migrate.Fault{Op: "read the pallets", Err: fault}
		},
	)
}
