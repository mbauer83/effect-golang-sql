package acceptance

// A history of a mapping, run against real servers. What matters is that a
// column renamed in the mapping is renamed in the table, with its checks; that
// the history agrees with the mapping as the program declares it at each
// version; and that a rule the domain tightens is a check the server holds the
// existing rows to -- refused while one breaks it, made once none does.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/evolve"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type part struct {
	ID   int64
	Code string
}

// partSchema is the domain as each version declares it: the code needs one
// character, and later three.
func partSchema(least int) (schema.Schema[part], schema.Field[part, string]) {
	code := schema.FieldAt("code", schema.Text().Check(schema.MinLength(least)), func(value *part) *string { return &value.Code })
	return schema.Struct[part]("part",
		schema.FieldAt("id", schema.Int64(), func(value *part) *int64 { return &value.ID }).Identity(), code), code
}

// partMappings are the mapping at each version: as the domain names it, then
// with the code stored as sku, then with the domain's rule tightened.
func partMappings() [3]sql.Mapping[part] {
	first, _ := partSchema(1)
	second, code := partSchema(1)
	third, tightened := partSchema(3)
	return [3]sql.Mapping[part]{
		sql.Map(first),
		sql.Map(second).Column(code, "sku"),
		sql.Map(third).Column(tightened, "sku"),
	}
}

func partHistory(mappings [3]sql.Mapping[part]) evolve.History {
	return evolve.Of("part").Start("1", mappings[0].Schema().Structure()).
		Then("2", evolve.Rename{From: "code", To: "sku"}).
		Then("3", evolve.Retype{Name: "sku", Node: schema.Text().Check(schema.MinLength(3)).Structure()})
}

func TestAHistoryAgreesWithTheMappingAtEachVersion(t *testing.T) {
	mappings := partMappings()
	history := partHistory(mappings)
	if err := history.Fault(); err != nil {
		t.Fatal(err)
	}
	if err := history.Validate(mappings[2].Schema().Structure()); err != nil {
		t.Errorf("expected the latest version to be the mapping as declared, got %v", err)
	}
	if err := history.Validate(mappings[1].Schema().Structure()); err == nil {
		t.Error("expected a mapping the history has moved past to be told apart from it")
	}
	renamed, err := ddl.Alter(ddl.Postgres, history, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(renamed[0], `RENAME COLUMN "code" TO "sku"`) ||
		!strings.Contains(strings.Join(renamed, "\n"), `RENAME CONSTRAINT "part_code_min_length" TO "part_sku_min_length"`) {
		t.Errorf("expected the column renamed with its check, got %v", renamed)
	}
}

func TestATightenedRuleHoldsTheRowsOnPostgres(t *testing.T) {
	tightenedAgainst(t, ddl.Postgres, "pgx", os.Getenv("EFFECT_GOLANG_POSTGRES_URL"))
}

func TestATightenedRuleHoldsTheRowsOnMySQL(t *testing.T) {
	tightenedAgainst(t, ddl.MySQL, "mysql", os.Getenv("EFFECT_GOLANG_MYSQL_URL"))
}

func tightenedAgainst(t *testing.T, dialect ddl.Dialect, driver string, address string) {
	t.Helper()
	if address == "" {
		t.Skipf("set the %s address to run a migration against it", dialect.Name())
	}
	history := partHistory(partMappings())
	first, _ := history.At("1")
	create, err := ddl.Create(dialect, first)
	if err != nil {
		t.Fatal(err)
	}
	drop, _ := ddl.Drop(dialect, first)
	renamed, err := ddl.Alter(dialect, history, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	tightened, err := ddl.Alter(dialect, history, "2", "3")
	if err != nil {
		t.Fatal(err)
	}
	table := dialect.QuoteIdentifier("part")
	insert := func(id string, code string) string {
		return "INSERT INTO " + table + " (" + dialect.QuoteIdentifier("id") + ", " + dialect.QuoteIdentifier("sku") +
			") VALUES (" + id + ", '" + code + "')"
	}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct{ refusedWhileBroken, madeOnceNot, refusedAfter bool }
	attempt := func(database *sql.Database, statements []string) sqlEffect[bool] {
		return executeAll(database, statements).As(true).
			CatchAll(func(sql.Fault) sqlEffect[bool] { return effect.Succeed[effect.Unit, sql.Fault](false) })
	}
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[outcome] {
		return sql.Open[effect.Unit](scope, driver, address).
			FlatMap(func(database *sql.Database) sqlEffect[outcome] {
				var result outcome
				return executeAll(database, append(append(append(drop, create...), renamed...), insert("1", "ab"))).
					FlatMap(func(effect.Unit) sqlEffect[bool] { return attempt(database, tightened) }).
					FlatMap(func(made bool) sqlEffect[bool] {
						result.refusedWhileBroken = !made
						return attempt(database, append([]string{"DELETE FROM " + table}, tightened...))
					}).
					FlatMap(func(made bool) sqlEffect[bool] {
						result.madeOnceNot = made
						return attempt(database, []string{insert("2", "ab")})
					}).
					Map(func(written bool) outcome { result.refusedAfter = !written; return result })
			})
	})
	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()
	exit := runtime.Run(within, effect.Unit{}, program)
	result, ok := exit.Value()
	if !ok {
		t.Fatal(exit)
	}
	if !result.refusedWhileBroken {
		t.Error("expected the tightened rule refused while a row breaks it")
	}
	if !result.madeOnceNot {
		t.Error("expected the tightened rule made once no row breaks it")
	}
	if !result.refusedAfter {
		t.Error("expected a row that breaks the tightened rule refused afterwards")
	}
}
