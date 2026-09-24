package acceptance

// An aggregate three tables deep: an album, its discs, their songs. What
// matters is that the deepest level is read for its holders' holders without
// binding them -- a query of the level above, back to the root -- and that a
// change down there writes that row alone.

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type song struct {
	ID    int64
	Title string
}

type disc struct {
	ID    int64
	Songs []song
}

type album struct {
	ID    int64
	Title string
	Discs []disc
}

var (
	songSchema = schema.Struct[song]("song",
		schema.FieldAt("id", schema.Int64(), func(value *song) *int64 { return &value.ID }).Identity(),
		schema.FieldAt("title", schema.Text(), func(value *song) *string { return &value.Title }))
	discSchema = schema.Struct[disc]("disc",
		schema.FieldAt("id", schema.Int64(), func(value *disc) *int64 { return &value.ID }).Identity(),
		schema.FieldAt("songs", schema.List(songSchema), func(value *disc) *[]song { return &value.Songs }))
	albumID = schema.FieldAt("id", schema.Int64(), func(value *album) *int64 { return &value.ID }).Identity()
	albums  = sql.NewRepository(sql.Map(schema.Struct[album]("album", albumID,
		schema.FieldAt("title", schema.Text(), func(value *album) *string { return &value.Title }),
		schema.FieldAt("discs", schema.List(discSchema), func(value *album) *[]disc { return &value.Discs }))), albumID.Shape())
)

func keepAlbums(t *testing.T, dialect ddl.Dialect, driver string, address string) {
	t.Helper()
	drop, _ := ddl.Drop(dialect, albums.Structure())
	create, err := ddl.Create(dialect, albums.Structure())
	if err != nil {
		t.Fatal(err)
	}
	first := album{1, "Double", []disc{{1, []song{{1, "One"}, {2, "Two"}}}, {2, []song{{3, "Three"}, {4, "Four"}}}}}
	other := album{2, "Single", []disc{{3, []song{{5, "Five"}}}}}
	changed := album{1, "Double", []disc{{1, []song{{1, "One"}, {2, "Two"}}}, {2, []song{{3, "Three (live)"}, {4, "Four"}}}}}
	var writes atomic.Int64
	runtime, _ := effect.NewRuntime()
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[[3]string] {
		return sql.Open[effect.Unit](scope, driver, address).FlatMap(func(opened *sql.Database) sqlEffect[[3]string] {
			database := countingDatabase{Database: opened, writes: &writes}
			var result [3]string
			return executeAll(opened, append(drop, create...)).
				FlatMap(func(effect.Unit) sqlEffect[effect.Unit] {
					return albums.Save(first).Provide(sql.Session{Database: database, Dialect: dialect})
				}).
				FlatMap(func(effect.Unit) sqlEffect[effect.Unit] {
					return albums.Save(other).Provide(sql.Session{Database: database, Dialect: dialect})
				}).
				FlatMap(func(effect.Unit) sqlEffect[album] {
					return albums.Find(1).Provide(sql.Session{Database: database, Dialect: dialect})
				}).
				FlatMap(func(found album) sqlEffect[effect.Unit] {
					result[0] = fmt.Sprint(found)
					writes.Store(0)
					return albums.Save(changed).Provide(sql.Session{Database: database, Dialect: dialect})
				}).
				FlatMap(func(effect.Unit) sqlEffect[album] {
					result[1] = fmt.Sprint(writes.Load())
					return albums.Find(1).Provide(sql.Session{Database: database, Dialect: dialect})
				}).
				Map(func(found album) [3]string { result[2] = fmt.Sprint(found); return result })
		})
	})
	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()
	exit := runtime.Run(within, effect.Unit{}, program)
	result, ok := exit.Value()
	if !ok {
		t.Fatal(exit)
	}
	if result[0] != fmt.Sprint(first) || result[2] != fmt.Sprint(changed) {
		t.Errorf("expected each album found as saved, the other album's songs apart\n\t%s\n\t%s", result[0], result[2])
	}
	if result[1] != "1" {
		t.Errorf("expected one song retitled to write one statement, got %s", result[1])
	}
}

func TestAnAggregateThreeTablesDeepOnSQLite(t *testing.T) {
	keepAlbums(t, ddl.SQLite, "sqlite", "file:"+t.TempDir()+"/albums.db?_pragma=foreign_keys(1)")
}

func TestAnAggregateThreeTablesDeepOnPostgres(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_POSTGRES_URL") == "" {
		t.Skip("set EFFECT_GOLANG_POSTGRES_URL to run against a real postgres")
	}
	keepAlbums(t, ddl.Postgres, "pgx", os.Getenv("EFFECT_GOLANG_POSTGRES_URL"))
}

func TestAnAggregateThreeTablesDeepOnMySQL(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_MYSQL_URL") == "" {
		t.Skip("set EFFECT_GOLANG_MYSQL_URL to run against a real mysql")
	}
	keepAlbums(t, ddl.MySQL, "mysql", os.Getenv("EFFECT_GOLANG_MYSQL_URL"))
}
