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

// crossed is what one run of the migrator has to report.
type crossed struct {
	structural migrate.Report
	rewriting  migrate.Report
	again      migrate.Report
	held       []dynamic.Value
}

// split is the projection the demo reads back. A description and not a struct,
// because three columns of a table in mid-history are not worth a Go type.
var split = schema.Struct[dynamic.Value]("Split",
	schema.DescribedField("prefix", schema.Text()),
	schema.DescribedField("serial", schema.Text()),
	schema.DescribedField("site", schema.Text()),
)

// runMigrating applies the history to an empty database, stores two pallets,
// crosses the rewriting, and applies it a third time -- which does nothing,
// the property a migrator has to have.
//
// 3.1.0 and not the latest version, because 4.0.0 changes a column's type and
// SQLite cannot: that step is for a database this demo does not require.
func runMigrating(runtime *effect.Runtime, workspace string) {
	source := "file:" + filepath.Join(workspace, "migrating.db")
	structural := migrate.Plan{Dialect: ddl.SQLite, History: warehouse.Pallets, Target: "3.0.0"}
	rewriting := migrate.Plan{Dialect: ddl.SQLite, History: warehouse.Pallets, Target: "3.1.0"}

	program := effect.Scoped(func(scope effect.Scope) moving[crossed] {
		return effect.Gen(func(do *effect.Do[effect.Unit, migrate.Fault]) crossed {
			database := do.Await(opened(scope, source))
			structuralReport := do.Await(migrate.Apply[effect.Unit](database, structural))
			do.Await(stored(database, "P-1", "Kiel"))
			do.Await(stored(database, "P-22", "Aarhus"))
			return crossed{
				structural: structuralReport,
				rewriting:  do.Await(migrate.Apply[effect.Unit](database, rewriting)),
				again:      do.Await(migrate.Apply[effect.Unit](database, rewriting)),
				held:       do.Await(effect.RunCollect(pallets(database))),
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
		report.structural.Created, report.structural.To, report.structural.Applied)
	fmt.Printf("  then the rewriting: to %s, applied %v\n",
		report.rewriting.To, report.rewriting.Applied)
	fmt.Printf("  again: nothing to do = %v\n", report.again.Nothing())
	for _, pallet := range report.held {
		fmt.Printf("  a pallet: %s\n", membersOf(pallet))
	}
}

// stored puts one pallet in, in the columns version 3.0.0 has.
func stored(database sql.Querying, reference string, site string) moving[sql.Outcome] {
	return sql.Execute[effect.Unit](database,
		`insert into "Pallet" ("reference", "site") values (`+ddl.SQLite.Placeholder(1)+`, `+ddl.SQLite.Placeholder(2)+`)`,
		dynamic.OfText(reference), dynamic.OfText(site),
	).MapError(func(fault sql.Fault) migrate.Fault {
		return migrate.Fault{Doing: "storing a pallet", Err: fault}
	})
}

// pallets reads what the split left, in the order the serials sort.
func pallets(database sql.Querying) effect.Stream[effect.Unit, migrate.Fault, dynamic.Value] {
	return effect.MapStreamError(
		sql.Query[effect.Unit](database, split,
			`select "prefix", "serial", "site" from "Pallet" order by "serial"`),
		func(fault sql.Fault) migrate.Fault {
			return migrate.Fault{Doing: "reading the pallets", Err: fault}
		},
	)
}
