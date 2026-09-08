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
	"github.com/mbauer83/effect-golang/experimental/direct"
)

// The three shapes these tests bind in, and the binder they bind with.
//
// Direct style throughout: what every one of these programs says is "do this,
// then that, then look" -- and as FlatMaps that reads inside-out, with each
// step nested in the one before it. The bodies hold no defer, which is the
// condition for using it: a defer here would run on an ordinary domain failure
// and not only on a panic.
type binder = direct.Binder[effect.Unit, migrate.Fault]

func applying(body func(*binder) migrate.Report) moving[migrate.Report] {
	return direct.Run(body)
}

func applyingString(body func(*binder) string) moving[string] {
	return direct.Run(body)
}

func applyingCount(body func(*binder) int64) moving[int64] {
	return direct.Run(body)
}

// counting reads one number out of a table, with the port's fault adapted.
func counting(database sql.Querying, table string) moving[warehouse.Counted] {
	return sql.QueryRow[effect.Unit](database, warehouse.CountedSchema,
		`select count(*) as "count" from "`+table+`"`).
		MapError(func(fault sql.Fault) migrate.Fault {
			return migrate.Fault{Doing: "counting", Err: fault}
		})
}
