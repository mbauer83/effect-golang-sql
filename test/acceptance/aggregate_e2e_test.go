package acceptance

// A whole aggregate kept by a repository, against real servers. What matters
// is that what is saved -- a list of entities, a single one, a list of
// references -- is what is found; that saving it changed writes only the rows
// that changed and deletes the ones gone; that saving it unchanged writes
// nothing; that a page of them is each whole; and that deleting one takes
// everything beneath it.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type tag struct {
	ID   int64
	Name string
}

type track struct {
	ID      int64
	Title   string
	Seconds int32
}

type cover struct {
	ID      int64
	Caption string
}

type playlist struct {
	ID     int64
	Name   string
	Tracks []track
	Cover  *cover
	Tags   []int64
}

var (
	tagID         = schema.FieldAt("id", schema.Int64(), func(value *tag) *int64 { return &value.ID }).Identity()
	tagSchema     = schema.Struct[tag]("tag", tagID, schema.FieldAt("name", schema.Text(), func(value *tag) *string { return &value.Name }))
	tags          = sql.Map(tagSchema)
	tagRepository = sql.NewRepository(tags, tagID.Shape())

	trackSchema = schema.Struct[track]("track",
		schema.FieldAt("id", schema.Int64(), func(value *track) *int64 { return &value.ID }).Identity(),
		schema.FieldAt("title", schema.Text(), func(value *track) *string { return &value.Title }),
		schema.FieldAt("seconds", schema.Int32(), func(value *track) *int32 { return &value.Seconds }))
	coverSchema = schema.Struct[cover]("cover",
		schema.FieldAt("id", schema.Int64(), func(value *cover) *int64 { return &value.ID }).Identity(),
		schema.FieldAt("caption", schema.Text(), func(value *cover) *string { return &value.Caption }))

	playlistID   = schema.FieldAt("id", schema.Int64(), func(value *playlist) *int64 { return &value.ID }).Identity()
	playlistName = schema.FieldAt("name", schema.Text(), func(value *playlist) *string { return &value.Name })
	playlists    = sql.NewRepository(sql.Map(schema.Struct[playlist]("playlist", playlistID, playlistName,
		schema.FieldAt("tracks", schema.List(trackSchema), func(value *playlist) *[]track { return &value.Tracks }),
		schema.FieldAt("cover", schema.Nullable(coverSchema), func(value *playlist) **cover { return &value.Cover }),
		schema.FieldAt("tags", schema.List(schema.Ref(tagSchema, tagID)), func(value *playlist) *[]int64 { return &value.Tags }),
	)).Referring(tags), playlistID.Shape())
)

// countingDatabase is a database that counts the statements that write.
type countingDatabase struct {
	*sql.Database
	writes *atomic.Int64
}

// Begin is a transaction whose writes are counted too.
func (database countingDatabase) Begin(ctx context.Context) (sql.Transaction, error) {
	transaction, err := database.Database.Begin(ctx)
	return countingTransaction{Transaction: transaction, writes: database.writes}, err
}

type countingTransaction struct {
	sql.Transaction
	writes *atomic.Int64
}

func (transaction countingTransaction) Execute(ctx context.Context, statement string, arguments []dynamic.Value) (sql.Outcome, error) {
	transaction.writes.Add(1)
	return transaction.Transaction.Execute(ctx, statement, arguments)
}

func (database countingDatabase) Execute(ctx context.Context, statement string, arguments []dynamic.Value) (sql.Outcome, error) {
	database.writes.Add(1)
	return database.Database.Execute(ctx, statement, arguments)
}

// described is a playlist as text, its cover by value.
func described(value playlist) string {
	shown := fmt.Sprintf("%d %s %v %v", value.ID, value.Name, value.Tracks, value.Tags)
	if value.Cover != nil {
		shown += fmt.Sprintf(" %+v", *value.Cover)
	}
	return shown
}

type aggregateResult struct {
	first, second   playlist
	changedWrites   int64
	unchangedWrites int64
	page            []playlist
	absence         bool
	leftBeneath     int64
}

type rowCount struct{ Count int64 }

var rowCountSchema = schema.Struct[rowCount]("", schema.FieldAt("count", schema.Int64(), func(value *rowCount) *int64 { return &value.Count }))

func keepPlaylists(t *testing.T, dialect ddl.Dialect, driver string, address string) {
	t.Helper()
	dropPlaylists, _ := ddl.Drop(dialect, playlists.Structure())
	dropTags, _ := ddl.Drop(dialect, tagRepository.Structure())
	createTags, err := ddl.Create(dialect, tagRepository.Structure())
	if err != nil {
		t.Fatal(err)
	}
	createPlaylists, err := ddl.Create(dialect, playlists.Structure())
	if err != nil {
		t.Fatal(err)
	}
	statements := append(append(append(dropPlaylists, dropTags...), createTags...), createPlaylists...)
	listing := playlists.Listing().Sort("name", playlists.Of(playlistName).Ascending())

	first := playlist{ID: 1, Name: "Morning",
		Tracks: []track{{1, "Intro", 90}, {2, "Rise", 200}, {3, "Shine", 180}},
		Cover:  &cover{1, "sunrise"}, Tags: []int64{1, 2}}
	// Track 2 gone, 3 retitled and moved first, the cover gone, a tag swapped.
	second := playlist{ID: 1, Name: "Morning",
		Tracks: []track{{3, "Shine On", 180}, {1, "Intro", 90}},
		Tags:   []int64{2, 3}}
	other := playlist{ID: 2, Name: "Evening", Tracks: []track{{1, "Dusk", 240}}, Cover: &cover{1, "sunset"}, Tags: []int64{3}}

	var writes atomic.Int64
	runtime, _ := effect.NewRuntime()
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[aggregateResult] {
		return sql.Open[effect.Unit](scope, driver, address).FlatMap(func(opened *sql.Database) sqlEffect[aggregateResult] {
			database := countingDatabase{Database: opened, writes: &writes}
			var result aggregateResult
			save := func(value playlist) sqlEffect[effect.Unit] {
				return playlists.Save[effect.Unit](database, dialect, value)
			}
			find := func(id int64) sqlEffect[playlist] { return playlists.Find[effect.Unit](database, dialect, id) }
			return executeAll(opened, statements).
				FlatMap(func(effect.Unit) sqlEffect[[]effect.Unit] {
					return effect.ForEach([]tag{{1, "calm"}, {2, "bright"}, {3, "slow"}}, func(each tag) sqlEffect[effect.Unit] {
						return tagRepository.Save[effect.Unit](opened, dialect, each)
					})
				}).
				FlatMap(func([]effect.Unit) sqlEffect[effect.Unit] { return save(first) }).
				FlatMap(func(effect.Unit) sqlEffect[effect.Unit] { return save(other) }).
				FlatMap(func(effect.Unit) sqlEffect[playlist] { return find(1) }).
				FlatMap(func(found playlist) sqlEffect[effect.Unit] {
					result.first = found
					writes.Store(0)
					return save(second)
				}).
				FlatMap(func(effect.Unit) sqlEffect[playlist] {
					result.changedWrites = writes.Load()
					return find(1)
				}).
				FlatMap(func(found playlist) sqlEffect[effect.Unit] {
					result.second = found
					writes.Store(0)
					return save(second)
				}).
				FlatMap(func(effect.Unit) sqlEffect[sql.Page[playlist]] {
					result.unchangedWrites = writes.Load()
					return listing.Page[effect.Unit](database, dialect, sql.PageQuery{})
				}).
				FlatMap(func(page sql.Page[playlist]) sqlEffect[sql.Outcome] {
					result.page = page.Items
					return playlists.Delete[effect.Unit](database, dialect, 1)
				}).
				FlatMap(func(sql.Outcome) sqlEffect[bool] {
					return find(1).As(false).CatchAll(func(fault sql.Fault) sqlEffect[bool] {
						return effect.Succeed[effect.Unit, sql.Fault](errors.Is(fault, sql.ErrNoRows))
					})
				}).
				FlatMap(func(absence bool) sqlEffect[rowCount] {
					result.absence = absence
					q := dialect.QuoteIdentifier
					return sql.QueryRow[effect.Unit](opened, rowCountSchema, fmt.Sprintf(
						"SELECT (SELECT COUNT(*) FROM %s WHERE %s = 1) + (SELECT COUNT(*) FROM %s WHERE %s = 1) + (SELECT COUNT(*) FROM %s WHERE %s = 1) AS %s",
						q("track"), q("playlist_id"), q("cover"), q("playlist_id"), q("playlist_tags"), q("playlist_id"), q("count")))
				}).
				Map(func(left rowCount) aggregateResult { result.leftBeneath = left.Count; return result })
		})
	})
	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()
	exit := runtime.Run(within, effect.Unit{}, program)
	result, ok := exit.Value()
	if !ok {
		t.Fatal(exit)
	}
	if described(result.first) != described(first) {
		t.Errorf("expected the aggregate saved to be found\n\t%+v\ngot\n\t%+v", first, result.first)
	}
	if described(result.second) != described(second) {
		t.Errorf("expected the changed aggregate found\n\t%+v\ngot\n\t%+v", second, result.second)
	}
	// The root unchanged. Deleted, a statement per table: track 2, the
	// cover, tag 1. Written, a statement per table: track 3, retitled and
	// moved before track 1, which keeps its place; tag 3 after tag 2, which
	// keeps its place.
	if result.changedWrites != 5 {
		t.Errorf("expected the rows that changed written and no others, in five statements; got %d", result.changedWrites)
	}
	if result.unchangedWrites != 0 {
		t.Errorf("expected an unchanged aggregate to write nothing, got %d", result.unchangedWrites)
	}
	if len(result.page) != 2 || described(result.page[0]) != described(other) || described(result.page[1]) != described(second) {
		t.Errorf("expected a page of whole aggregates by name, got %+v", result.page)
	}
	if !result.absence || result.leftBeneath != 0 {
		t.Errorf("expected a deleted aggregate gone with everything beneath it; found: %v, rows left: %d", !result.absence, result.leftBeneath)
	}
}

func TestARepositoryKeepsAWholeAggregateOnSQLite(t *testing.T) {
	keepPlaylists(t, ddl.SQLite, "sqlite", "file:"+t.TempDir()+"/playlists.db?_pragma=foreign_keys(1)")
}

func TestARepositoryKeepsAWholeAggregateOnPostgres(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_POSTGRES_URL") == "" {
		t.Skip("set EFFECT_GOLANG_POSTGRES_URL to run against a real postgres")
	}
	keepPlaylists(t, ddl.Postgres, "pgx", os.Getenv("EFFECT_GOLANG_POSTGRES_URL"))
}

func TestARepositoryKeepsAWholeAggregateOnMySQL(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_MYSQL_URL") == "" {
		t.Skip("set EFFECT_GOLANG_MYSQL_URL to run against a real mysql")
	}
	keepPlaylists(t, ddl.MySQL, "mysql", os.Getenv("EFFECT_GOLANG_MYSQL_URL"))
}
