package acceptance

// A long list kept by a repository. What matters is that saving five thousand
// elements is a few statements, that moving one writes one row, that
// appending without reading writes the list in a statement, and that a list
// kept in order through many insertions at one place stays in order when it
// runs out of room there.

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// largeResult is what saving a long list costs, and what one change to it
// does.
type largeResult struct {
	firstWrites, movedWrites, appendedWrites int64
	found                                    playlist
}

// keepALongPlaylist saves five thousand tracks, moves one, and appends one
// without reading, counting the statements each writes.
func keepALongPlaylist(t *testing.T, dialect ddl.Dialect, driver string, address string) {
	t.Helper()
	dropPlaylists, _ := ddl.Drop(dialect, playlists.Structure())
	dropTags, _ := ddl.Drop(dialect, tagRepository.Structure())
	createTags, _ := ddl.Create(dialect, tagRepository.Structure())
	createPlaylists, _ := ddl.Create(dialect, playlists.Structure())
	statements := append(append(append(dropPlaylists, dropTags...), createTags...), createPlaylists...)
	long := playlist{ID: 1, Name: "Long", Tags: []int64{}}
	for id := int64(1); id <= 5000; id++ {
		long.Tracks = append(long.Tracks, track{id, fmt.Sprintf("Track %d", id), 180})
	}
	// Track 4000 moved to the front.
	moved := long
	moved.Tracks = append([]track{long.Tracks[3999]}, append(append([]track(nil), long.Tracks[:3999]...), long.Tracks[4000:]...)...)
	appended := moved
	appended.Tracks = append(append([]track(nil), moved.Tracks...), track{5001, "Encore", 200})

	var writes atomic.Int64
	runtime, _ := effect.NewRuntime()
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[largeResult] {
		return sql.Open[effect.Unit](scope, driver, address).FlatMap(func(opened *sql.Database) sqlEffect[largeResult] {
			database := countingDatabase{Database: opened, writes: &writes}
			var result largeResult
			count := func(into *int64) func(effect.Unit) effect.Unit {
				return func(effect.Unit) effect.Unit { *into = writes.Swap(0); return effect.Unit{} }
			}
			return executeAll(opened, statements).
				FlatMap(func(effect.Unit) sqlEffect[effect.Unit] {
					writes.Store(0)
					return playlists.Save(long).Provide(sql.Session{Database: database, Dialect: dialect}).Map(count(&result.firstWrites))
				}).
				FlatMap(func(effect.Unit) sqlEffect[effect.Unit] {
					return playlists.Save(moved).Provide(sql.Session{Database: database, Dialect: dialect}).Map(count(&result.movedWrites))
				}).
				FlatMap(func(effect.Unit) sqlEffect[effect.Unit] {
					return playlists.SaveChanges(moved, appended).Provide(sql.Session{Database: database, Dialect: dialect}).Map(count(&result.appendedWrites))
				}).
				FlatMap(func(effect.Unit) sqlEffect[playlist] {
					return playlists.Find(1).Provide(sql.Session{Database: database, Dialect: dialect})
				}).
				Map(func(found playlist) largeResult { result.found = found; return result })
		})
	})
	within, giveUp := context.WithTimeout(context.Background(), 120*time.Second)
	defer giveUp()
	exit := runtime.Run(within, effect.Unit{}, program)
	result, ok := exit.Value()
	if !ok {
		t.Fatal(exit)
	}
	// The root, and five thousand tracks of five columns -- the playlist and
	// the position with their own -- 6,000 to a statement: two statements.
	if result.firstWrites != 2 {
		t.Errorf("expected five thousand tracks written in a few statements, got %d", result.firstWrites)
	}
	// One track moved: one row, one statement.
	if result.movedWrites != 1 {
		t.Errorf("expected a moved track to write one row, got %d statements", result.movedWrites)
	}
	// Appended without reading: where the others stand is not known, so the
	// list is written whole -- still one statement per 6,000 rows.
	if result.appendedWrites != 1 {
		t.Errorf("expected an appended list written whole in one statement, got %d", result.appendedWrites)
	}
	if described(result.found) != described(appended) {
		t.Errorf("expected the list as it was saved, got %d tracks, the first %v", len(result.found.Tracks), result.found.Tracks[:3])
	}
}

func TestALongListIsCheapToChangeOnSQLite(t *testing.T) {
	keepALongPlaylist(t, ddl.SQLite, "sqlite", "file:"+t.TempDir()+"/long.db?_pragma=foreign_keys(1)")
}

func TestALongListIsCheapToChangeOnPostgres(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_POSTGRES_URL") == "" {
		t.Skip("set EFFECT_GOLANG_POSTGRES_URL to run against a real postgres")
	}
	keepALongPlaylist(t, ddl.Postgres, "pgx", os.Getenv("EFFECT_GOLANG_POSTGRES_URL"))
}

func TestALongListIsCheapToChangeOnMySQL(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_MYSQL_URL") == "" {
		t.Skip("set EFFECT_GOLANG_MYSQL_URL to run against a real mysql")
	}
	keepALongPlaylist(t, ddl.MySQL, "mysql", os.Getenv("EFFECT_GOLANG_MYSQL_URL"))
}

func TestAListStaysInOrderWhenItRunsOutOfRoom(t *testing.T) {
	create, _ := ddl.Create(ddl.SQLite, tagRepository.Structure())
	more, _ := ddl.Create(ddl.SQLite, playlists.Structure())
	// Each new track straight after the first: every one halves the room the
	// one before left, until there is none and the list is numbered again.
	versions := []playlist{{ID: 1, Name: "Crowded", Tags: []int64{}, Tracks: []track{{1, "First", 1}, {2, "Last", 1}}}}
	for id := int64(3); id < 23; id++ {
		previous := versions[len(versions)-1]
		next := previous
		next.Tracks = append([]track{previous.Tracks[0], {id, fmt.Sprintf("Track %d", id), 1}}, previous.Tracks[1:]...)
		versions = append(versions, next)
	}
	runtime, _ := effect.NewRuntime()
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[[]string] {
		return sql.Open[effect.Unit](scope, "sqlite", "file:"+t.TempDir()+"/crowded.db?_pragma=foreign_keys(1)").
			FlatMap(func(database *sql.Database) sqlEffect[[]string] {
				return executeAll(database, append(create, more...)).
					FlatMap(func(effect.Unit) sqlEffect[[]string] {
						return effect.ForEach(versions, func(version playlist) sqlEffect[string] {
							return playlists.Save(version).Provide(sql.Session{Database: database, Dialect: ddl.SQLite}).
								FlatMap(func(effect.Unit) sqlEffect[playlist] {
									return playlists.Find(1).Provide(sql.Session{Database: database, Dialect: ddl.SQLite})
								}).
								Map(described)
						})
					})
			})
	})
	within, giveUp := context.WithTimeout(context.Background(), 30*time.Second)
	defer giveUp()
	found, ok := runtime.Run(within, effect.Unit{}, program).Value()
	if !ok {
		t.Fatal("expected every version saved and found")
	}
	for at, version := range versions {
		if found[at] != described(version) {
			t.Fatalf("version %d: expected\n\t%s\ngot\n\t%s", at, described(version), found[at])
		}
	}
}
