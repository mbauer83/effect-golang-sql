package acceptance

// A repository's writes against real servers, where a unique key besides the
// identity is involved. What matters is that a row whose unique value another
// aggregate holds is refused and leaves that aggregate as it was -- which an
// upsert on MySQL would not -- that an aggregate is found by a criterion, and
// that one whose identity the database generates is inserted.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type subscriber struct {
	ID    int64
	Email string
	Name  string
}

type badge struct {
	ID   int64
	Code string
}

type club struct {
	ID     int64
	Badges []badge
}

type note struct {
	ID   int64
	Text string
}

var (
	subscriberID    = schema.FieldAt("id", schema.Int64(), func(value *subscriber) *int64 { return &value.ID }).Identity()
	subscriberEmail = schema.FieldAt("email", schema.Text().Check(schema.MaxLength(100)), func(value *subscriber) *string { return &value.Email }).Unique()
	subscribers     = sql.NewRepository(sql.Map(schema.Struct[subscriber]("subscriber", subscriberID, subscriberEmail,
		schema.FieldAt("name", schema.Text(), func(value *subscriber) *string { return &value.Name }))), subscriberID)

	clubID = schema.FieldAt("id", schema.Int64(), func(value *club) *int64 { return &value.ID }).Identity()
	clubs  = sql.NewRepository(sql.Map(schema.Struct[club]("club", clubID,
		schema.FieldAt("badges", schema.List(schema.Struct[badge]("badge",
			schema.FieldAt("id", schema.Int64(), func(value *badge) *int64 { return &value.ID }).Identity(),
			schema.FieldAt("code", schema.Text().Check(schema.MaxLength(20)), func(value *badge) *string { return &value.Code }).Unique())),
			func(value *club) *[]badge { return &value.Badges }))), clubID)

	noteID = schema.FieldAt("id", schema.Int64(), func(value *note) *int64 { return &value.ID }).Identity().Computed()
	notes  = sql.NewRepository(sql.Map(schema.Struct[note]("note", noteID,
		schema.FieldAt("text", schema.Text(), func(value *note) *string { return &value.Text }))), noteID)
)

type writesResult struct {
	subscriberRefused, badgeRefused bool
	subscriberKept                  subscriber
	renamed                         subscriber
	byEmail                         subscriber
	everyone                        []subscriber
	firstClub                       string
	notes                           []note
}

func writeWithUniqueKeys(t *testing.T, dialect ddl.Dialect, driver string, address string) {
	t.Helper()
	var statements []string
	for _, node := range []structure.Node{subscribers.Structure(), clubs.Structure(), notes.Structure()} {
		drop, _ := ddl.Drop(dialect, node)
		statements = append(statements, drop...)
	}
	for _, node := range []structure.Node{subscribers.Structure(), clubs.Structure(), notes.Structure()} {
		create, err := ddl.Create(dialect, node)
		if err != nil {
			t.Fatal(err)
		}
		statements = append(statements, create...)
	}
	refused := func(effort sqlEffect[effect.Unit]) sqlEffect[bool] {
		return effort.As(false).CatchAll(func(fault sql.Fault) sqlEffect[bool] {
			return effect.Succeed[effect.Unit, sql.Fault](errors.Is(fault, sql.ErrAlreadyThere))
		})
	}
	runtime, _ := effect.NewRuntime()
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[writesResult] {
		return sql.Open[effect.Unit](scope, driver, address).FlatMap(func(database *sql.Database) sqlEffect[writesResult] {
			var result writesResult
			saveRoot := func(value subscriber) sqlEffect[effect.Unit] {
				return subscribers.SaveRoot[effect.Unit](database, dialect, value)
			}
			return executeAll(database, statements).
				FlatMap(func(effect.Unit) sqlEffect[effect.Unit] { return saveRoot(subscriber{1, "ann@example.test", "Ann"}) }).
				FlatMap(func(effect.Unit) sqlEffect[effect.Unit] { return saveRoot(subscriber{2, "bob@example.test", "Bob"}) }).
				FlatMap(func(effect.Unit) sqlEffect[bool] { return refused(saveRoot(subscriber{3, "ann@example.test", "Cat"})) }).
				FlatMap(func(was bool) sqlEffect[subscriber] {
					result.subscriberRefused = was
					return subscribers.Find[effect.Unit](database, dialect, 1)
				}).
				FlatMap(func(kept subscriber) sqlEffect[effect.Unit] {
					result.subscriberKept = kept
					return saveRoot(subscriber{1, "ann@example.test", "Ann Lee"})
				}).
				FlatMap(func(effect.Unit) sqlEffect[subscriber] { return subscribers.Find[effect.Unit](database, dialect, 1) }).
				FlatMap(func(renamed subscriber) sqlEffect[subscriber] {
					result.renamed = renamed
					return subscribers.FindOneBy[effect.Unit](database, dialect,
						sql.Equal(subscribers.Of(subscriberEmail), sql.Param("bob@example.test")))
				}).
				FlatMap(func(found subscriber) sqlEffect[[]subscriber] {
					result.byEmail = found
					return subscribers.FindBy[effect.Unit](database, dialect, sql.True(), subscribers.Of(subscriberEmail).Descending())
				}).
				FlatMap(func(everyone []subscriber) sqlEffect[effect.Unit] {
					result.everyone = everyone
					return clubs.Save[effect.Unit](database, dialect, club{1, []badge{{1, "GOLD"}}})
				}).
				FlatMap(func(effect.Unit) sqlEffect[bool] {
					return refused(clubs.Save[effect.Unit](database, dialect, club{2, []badge{{9, "GOLD"}}}))
				}).
				FlatMap(func(was bool) sqlEffect[club] {
					result.badgeRefused = was
					return clubs.Find[effect.Unit](database, dialect, 1)
				}).
				FlatMap(func(first club) sqlEffect[effect.Unit] {
					result.firstClub = fmt.Sprint(first)
					return notes.Insert[effect.Unit](database, dialect, note{Text: "first"})
				}).
				FlatMap(func(effect.Unit) sqlEffect[effect.Unit] {
					return notes.Insert[effect.Unit](database, dialect, note{Text: "second"})
				}).
				FlatMap(func(effect.Unit) sqlEffect[[]note] {
					return notes.FindBy[effect.Unit](database, dialect, sql.True(), notes.Of(noteID).Ascending())
				}).
				Map(func(found []note) writesResult { result.notes = found; return result })
		})
	})
	within, giveUp := context.WithTimeout(context.Background(), 60*time.Second)
	defer giveUp()
	exit := runtime.Run(within, effect.Unit{}, program)
	result, ok := exit.Value()
	if !ok {
		t.Fatal(exit)
	}
	if !result.subscriberRefused || result.subscriberKept != (subscriber{1, "ann@example.test", "Ann"}) {
		t.Errorf("expected a root whose unique email another holds refused, and the other as it was; refused %v, kept %+v",
			result.subscriberRefused, result.subscriberKept)
	}
	if result.renamed.Name != "Ann Lee" {
		t.Errorf("expected the root replaced under its identity, got %+v", result.renamed)
	}
	if result.byEmail.ID != 2 || len(result.everyone) != 2 || result.everyone[0].ID != 2 {
		t.Errorf("expected aggregates found by a criterion, in order; one %+v, every %+v", result.byEmail, result.everyone)
	}
	if !result.badgeRefused || result.firstClub != fmt.Sprint(club{1, []badge{{1, "GOLD"}}}) {
		t.Errorf("expected a child whose unique code another aggregate holds refused, and that aggregate as it was; refused %v, kept %s",
			result.badgeRefused, result.firstClub)
	}
	if len(result.notes) != 2 || result.notes[0].Text != "first" || result.notes[0].ID == result.notes[1].ID {
		t.Errorf("expected two notes inserted with identities the database gave them, got %+v", result.notes)
	}
}

func TestWritesKeepEveryUniqueKeyOnSQLite(t *testing.T) {
	writeWithUniqueKeys(t, ddl.SQLite, "sqlite", "file:"+t.TempDir()+"/unique.db?_pragma=foreign_keys(1)")
}

func TestWritesKeepEveryUniqueKeyOnPostgres(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_POSTGRES_URL") == "" {
		t.Skip("set EFFECT_GOLANG_POSTGRES_URL to run against a real postgres")
	}
	writeWithUniqueKeys(t, ddl.Postgres, "pgx", os.Getenv("EFFECT_GOLANG_POSTGRES_URL"))
}

func TestWritesKeepEveryUniqueKeyOnMySQL(t *testing.T) {
	if os.Getenv("EFFECT_GOLANG_MYSQL_URL") == "" {
		t.Skip("set EFFECT_GOLANG_MYSQL_URL to run against a real mysql")
	}
	writeWithUniqueKeys(t, ddl.MySQL, "mysql", os.Getenv("EFFECT_GOLANG_MYSQL_URL"))
}
