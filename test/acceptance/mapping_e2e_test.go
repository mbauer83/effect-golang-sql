package acceptance

// A mapped aggregate against a real database: the checks and the foreign key
// its domain implies are ones the database keeps, not only statements that
// read well.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

var (
	mappedTitleID = schema.DynamicField("id", schema.Int64()).Identity()
	mappedTitle   = schema.Struct[dynamic.Value]("title", mappedTitleID,
		schema.DynamicField("name", schema.Text().Check(schema.MinLength(1))))
	mappedShowing = schema.Struct[dynamic.Value]("showing",
		schema.DynamicField("id", schema.Int64()).Identity(),
		schema.DynamicField("title", schema.Ref(mappedTitle, mappedTitleID)))
)

// kept runs statements against a fresh SQLite database made from the two
// mappings, and reports each statement's refusal, or nil.
func kept(t *testing.T, statements ...string) []error {
	t.Helper()
	titles := sql.Map(mappedTitle)
	showings := sql.Map(mappedShowing).Referring(titles)
	var ddlStatements []string
	for _, stored := range []schema.Schema[dynamic.Value]{titles.Schema(), showings.Schema()} {
		made, err := ddl.Create(ddl.SQLite, stored.Structure())
		if err != nil {
			t.Fatal(err)
		}
		ddlStatements = append(ddlStatements, made...)
	}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	refusals := make([]error, len(statements))
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[effect.Unit] {
		return sql.Open[effect.Unit](scope, "sqlite", "file:"+t.TempDir()+"/mapped.db?_pragma=foreign_keys(1)").
			FlatMap(func(database *sql.Database) sqlEffect[effect.Unit] {
				return effect.ForEach(ddlStatements, func(statement string) sqlEffect[sql.Outcome] {
					return sql.Execute[effect.Unit](database, statement)
				}).FlatMap(func([]sql.Outcome) sqlEffect[effect.Unit] {
					return effect.ForEach(statements, func(statement string) sqlEffect[effect.Unit] {
						index := indexOf(statements, statement)
						return sql.Execute[effect.Unit](database, statement).
							As(effect.Unit{}).
							CatchAll(func(fault sql.Fault) sqlEffect[effect.Unit] {
								refusals[index] = fault
								return effect.Succeed[effect.Unit, sql.Fault](effect.Unit{})
							})
					}).As(effect.Unit{})
				})
			})
	})
	within, giveUp := context.WithTimeout(context.Background(), 10*time.Second)
	defer giveUp()
	if _, ok := runtime.Run(within, effect.Unit{}, program).Value(); !ok {
		t.Fatal("expected the schema to be made")
	}
	return refusals
}

func indexOf(statements []string, statement string) int {
	for index, candidate := range statements {
		if candidate == statement {
			return index
		}
	}
	return -1
}

func TestAMappedAggregatesChecksAndKeysAreKeptByTheDatabase(t *testing.T) {
	refusals := kept(t,
		`INSERT INTO "title" ("id", "name") VALUES (1, 'Alien')`,
		`INSERT INTO "title" ("id", "name") VALUES (2, '')`,
		`INSERT INTO "showing" ("id", "title") VALUES (10, 1)`,
		`INSERT INTO "showing" ("id", "title") VALUES (11, 99)`,
		`DELETE FROM "title" WHERE "id" = 1`,
		`DELETE FROM "showing" WHERE "id" = 10`,
		`DELETE FROM "title" WHERE "id" = 1 AND 1 = 1`,
	)
	expect := []struct {
		refused bool
		why     string
	}{
		{false, "a title"},
		{true, "a blank name, which the check refuses"},
		{false, "a showing of a title that exists"},
		{true, "a showing of a title that does not"},
		{true, "deleting a title something shows"},
		{false, "deleting the showing"},
		{false, "deleting the title once nothing shows it"},
	}
	for index, expected := range expect {
		if (refusals[index] != nil) != expected.refused {
			t.Errorf("%s: expected refused=%v, got %v", expected.why, expected.refused, refusals[index])
		}
	}
	if refusals[1] != nil && !strings.Contains(refusals[1].Error(), "title_name_min_length") {
		t.Errorf("expected the refusal to name the check, got %v", refusals[1])
	}
}

type authored struct {
	ID     int64
	Title  string
	Author string
}

var authoredID = schema.FieldAt("id", schema.Int64(), func(value *authored) *int64 { return &value.ID }).Identity()

var authoredTitles = sql.NewRepository(sql.Map(schema.Struct[authored]("authored", authoredID,
	schema.FieldAt("title", schema.Text(), func(value *authored) *string { return &value.Title }).UniqueTogether("title_per_author"),
	schema.FieldAt("author", schema.Text(), func(value *authored) *string { return &value.Author }).UniqueTogether("title_per_author"),
)), authoredID.Shape())

func TestFieldsUniqueTogetherAreKeptByTheDatabase(t *testing.T) {
	create, err := ddl.Create(ddl.SQLite, authoredTitles.Structure())
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := effect.NewRuntime()
	program := effect.Scoped(func(scope effect.Scope) sqlEffect[bool] {
		return sql.Open[effect.Unit](scope, "sqlite", "file:"+t.TempDir()+"/authored.db").
			FlatMap(func(database *sql.Database) sqlEffect[bool] {
				save := func(value authored) sqlEffect[sql.Outcome] {
					return authoredTitles.Save(value).Provide(sql.Session{Database: database, Dialect: ddl.SQLite}).As(sql.Outcome{})
				}
				return executeAll(database, create).
					FlatMap(func(effect.Unit) sqlEffect[sql.Outcome] { return save(authored{1, "Emma", "Jane Austen"}) }).
					FlatMap(func(sql.Outcome) sqlEffect[sql.Outcome] { return save(authored{2, "Emma", "Emma Tennant"}) }).
					FlatMap(func(sql.Outcome) sqlEffect[bool] {
						return save(authored{3, "Emma", "Jane Austen"}).As(false).
							CatchAll(func(fault sql.Fault) sqlEffect[bool] {
								return effect.Succeed[effect.Unit, sql.Fault](errors.Is(fault, sql.ErrAlreadyThere))
							})
					})
			})
	})
	within, giveUp := context.WithTimeout(context.Background(), 10*time.Second)
	defer giveUp()
	refused, ok := runtime.Run(within, effect.Unit{}, program).Value()
	if !ok || !refused {
		t.Fatal("expected one title twice by different authors kept, and the same pair again refused as already there")
	}
}
