package acceptance

// The shapes the migrator's tests bind in.
//
// Direct style throughout: what every one of those programs says is "do this,
// then that, then look" -- and as FlatMaps that reads inside-out, with each step
// nested in the one before it.

import (
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
	"github.com/mbauer83/effect-golang-sql/migrate"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// The three shapes these tests bind in, and the binder they bind with.
//
// Direct style throughout: what every one of these programs says is "do this,
// then that, then look" -- and as FlatMaps that reads inside-out, with each
// step nested in the one before it. The bodies hold no defer, which is the
// condition for using it: a defer here would run on an ordinary domain failure
// and not only on a panic.
type binder = effect.Do[effect.Unit, migrate.Fault]

func apply(body func(*binder) migrate.Report) migrateEffect[migrate.Report] {
	return effect.Gen(body)
}

func applyString(body func(*binder) string) migrateEffect[string] {
	return effect.Gen(body)
}

func applyCount(body func(*binder) int64) migrateEffect[int64] {
	return effect.Gen(body)
}

// countRows reads one number out of a table, with the port's fault adapted.
func countRows(database sql.Querier, table string) migrateEffect[warehouse.Tally] {
	return sql.QueryRow[effect.Unit](database, warehouse.TallySchema,
		`select count(*) as "count" from "`+table+`"`).
		MapError(func(fault sql.Fault) migrate.Fault {
			return migrate.Fault{Op: "counting", Err: fault}
		})
}
