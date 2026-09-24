package acceptance

// A repository against real servers. What matters is that an aggregate saved
// is the aggregate first, value objects and all; that saving it again
// replaces it; that one deleted is not first; and that its listing pages it.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type binding struct {
	Cover string
	Pages int32
}

type volume struct {
	ID      int64
	Title   string
	Binding binding
}

var volumeFields = struct {
	ID      schema.Field[volume, int64]
	Title   schema.Field[volume, string]
	Binding schema.Field[volume, binding]
}{
	ID:    schema.FieldAt("id", schema.Int64(), func(value *volume) *int64 { return &value.ID }).Identity(),
	Title: schema.FieldAt("title", schema.Text().Check(schema.MinLength(1), schema.MaxLength(200)), func(value *volume) *string { return &value.Title }),
	Binding: schema.FieldAt("binding", schema.Struct[binding]("binding",
		schema.FieldAt("cover", schema.Text().Check(schema.MaxLength(20)), func(value *binding) *string { return &value.Cover }),
		schema.FieldAt("pages", schema.Int32().Check(schema.AtLeast[int32](1)), func(value *binding) *int32 { return &value.Pages })),
		func(value *volume) *binding { return &value.Binding }),
}

var volumes = sql.NewRepository(
	sql.Map(schema.Struct[volume]("volume", volumeFields.ID, volumeFields.Title, volumeFields.Binding)).Column(volumeFields.ID, "volume_id"),
	volumeFields.ID)

type volumeOutcome struct {
	first       volume
	replacement volume
	absence     bool
	firstPage   []string
}

func keepVolumes(t *testing.T, dialect ddl.Dialect, driver string, address string) {
	t.Helper()
	structure := sql.Map(schema.Struct[volume]("volume", volumeFields.ID, volumeFields.Title, volumeFields.Binding)).
		Column(volumeFields.ID, "volume_id").Schema().Structure()
	drop, _ := ddl.Drop(dialect, structure)
	create, err := ddl.Create(dialect, structure)
	if err != nil {
		t.Fatal(err)
	}
	listing := volumes.Listing().Sort("title", volumes.Of(volumeFields.Title).Ascending()).PageSize(2, 10)
	runtime, _ := effect.NewRuntime()
	save := func(database *sql.Database, value volume) sqlEffect[sql.Outcome] {
		return volumes.Save[effect.Unit](database, dialect, value)
	}
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[volumeOutcome] {
		return sql.Open[effect.Unit](scope, driver, address).FlatMap(func(database *sql.Database) sqlEffect[volumeOutcome] {
			var result volumeOutcome
			return executeAll(database, append(drop, create...)).
				FlatMap(func(effect.Unit) sqlEffect[sql.Outcome] {
					return save(database, volume{1, "Solaris", binding{"cloth", 204}})
				}).
				FlatMap(func(sql.Outcome) sqlEffect[sql.Outcome] {
					return save(database, volume{2, "Dune", binding{"paper", 412}})
				}).
				FlatMap(func(sql.Outcome) sqlEffect[sql.Outcome] {
					return save(database, volume{3, "Emma", binding{"paper", 474}})
				}).
				FlatMap(func(sql.Outcome) sqlEffect[volume] { return volumes.Find[effect.Unit](database, dialect, 1) }).
				FlatMap(func(first volume) sqlEffect[sql.Outcome] {
					result.first = first
					return save(database, volume{1, "Solaris", binding{"paper", 204}})
				}).
				FlatMap(func(sql.Outcome) sqlEffect[volume] { return volumes.Find[effect.Unit](database, dialect, 1) }).
				FlatMap(func(replacement volume) sqlEffect[sql.Outcome] {
					result.replacement = replacement
					return volumes.Delete[effect.Unit](database, dialect, 3)
				}).
				FlatMap(func(sql.Outcome) sqlEffect[bool] {
					return volumes.Find[effect.Unit](database, dialect, 3).As(false).
						CatchAll(func(fault sql.Fault) sqlEffect[bool] {
							return effect.Succeed[effect.Unit, sql.Fault](errors.Is(fault, sql.ErrNoRows))
						})
				}).
				FlatMap(func(absence bool) sqlEffect[sql.Page[volume]] {
					result.absence = absence
					return listing.Page[effect.Unit](database, dialect, sql.PageQuery{})
				}).
				Map(func(page sql.Page[volume]) volumeOutcome {
					for _, item := range page.Items {
						result.firstPage = append(result.firstPage, item.Title)
					}
					return result
				})
		})
	})
	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()
	exit := runtime.Run(within, effect.Unit{}, program)
	result, ok := exit.Value()
	if !ok {
		t.Fatal(exit)
	}
	if result.first != (volume{1, "Solaris", binding{"cloth", 204}}) {
		t.Errorf("expected the aggregate saved to be the one first, got %+v", result.first)
	}
	if result.replacement.Binding.Cover != "paper" {
		t.Errorf("expected saving again to replace it, got %+v", result.replacement)
	}
	if !result.absence {
		t.Error("expected a deleted aggregate not first")
	}
	if len(result.firstPage) != 2 || result.firstPage[0] != "Dune" || result.firstPage[1] != "Solaris" {
		t.Errorf("expected the first page by title, got %v", result.firstPage)
	}
}

func TestARepositoryKeepsAggregatesOnSQLite(t *testing.T) {
	keepVolumes(t, ddl.SQLite, "sqlite", "file:"+t.TempDir()+"/volumeOutcome.db")
}

func TestARepositoryKeepsAggregatesOnPostgres(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_POSTGRES_URL") == "" {
		t.Skip("set EFFECT_GOLANG_POSTGRES_URL to run against a real postgres")
	}
	keepVolumes(t, ddl.Postgres, "pgx", os.Getenv("EFFECT_GOLANG_POSTGRES_URL"))
}

func TestARepositoryKeepsAggregatesOnMySQL(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_MYSQL_URL") == "" {
		t.Skip("set EFFECT_GOLANG_MYSQL_URL to run against a real mysql")
	}
	keepVolumes(t, ddl.MySQL, "mysql", os.Getenv("EFFECT_GOLANG_MYSQL_URL"))
}
