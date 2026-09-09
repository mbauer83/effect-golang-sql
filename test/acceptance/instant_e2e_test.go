package acceptance

// An instant, written and read back.
//
// This suite had no test that round-tripped a time.Time, and a whole dialect
// could not do it: SQLite has no date type, so this module projects an instant
// onto a text column, the driver hands the text back, and a description asking
// for a timestamp refused it. Nothing here was wrong -- the projection is the
// only honest one SQLite admits -- and nothing caught it either, because
// writing an instant and reading it again is exactly what no test did.
//
// The tolerance that fixes it is in the schema layer, beside the one that lets
// text read a bytes value: which representation a source chose is its business
// rather than the value's meaning. This is the test that would have found it.

import (
	"context"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// screening is the smallest aggregate with an instant in it.
type screening struct {
	ID       string
	StartsAt time.Time
}

var screeningSchema = schema.Struct[screening]("screening",
	schema.FieldOf("id", schema.MaxLength(schema.MinLength(schema.Text(), 1), 64),
		func(held screening) string { return held.ID },
		func(held *screening, id string) { held.ID = id }).
		Identity(),
	schema.FieldOf("starts_at", schema.Time(),
		func(held screening) time.Time { return held.StartsAt },
		func(held *screening, at time.Time) { held.StartsAt = at }),
).Documented("one showing, at a moment")

func TestAnInstantSurvivesADialectWithNoDateType(t *testing.T) {
	wanted := time.Date(2026, 9, 9, 21, 30, 0, 0, time.UTC)

	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	source := "file:" + t.TempDir() + "/instants.db"

	type reading = effect.Effect[effect.Unit, sql.Fault, screening]
	program := effect.Scoped(func(scope effect.Scope) reading {
		return sql.Open[effect.Unit](scope, "sqlite", source).
			FlatMap(func(database *sql.Connected) reading {
				return writing(t, database, wanted)
			})
	})

	within, giveUp := context.WithTimeout(context.Background(), 10*time.Second)
	defer giveUp()

	exit := runtime.Run(within, effect.Unit{}, program)
	held, succeeded := exit.Value()
	if !succeeded {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if !held.StartsAt.Equal(wanted) {
		t.Fatalf("expected %v back, got %v", wanted, held.StartsAt)
	}
}

func writing(
	t *testing.T,
	database *sql.Connected,
	at time.Time,
) effect.Effect[effect.Unit, sql.Fault, screening] {
	t.Helper()
	statements, err := ddl.Create(ddl.SQLite, screeningSchema.Structure())
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := sql.Arguments(screeningSchema, screening{ID: "one", StartsAt: at})
	if err != nil {
		t.Fatal(err)
	}

	type reading = effect.Effect[effect.Unit, sql.Fault, screening]
	made := sql.Execute[effect.Unit](database, statements[0])
	return made.FlatMap(func(sql.Outcome) reading {
		return sql.Execute[effect.Unit](database,
			`insert into "screening" ("id", "starts_at") values (?, ?)`, arguments...).
			FlatMap(func(sql.Outcome) reading {
				return sql.QueryRow[effect.Unit](database, screeningSchema,
					`select "id", "starts_at" from "screening" where "id" = ?`,
					dynamic.OfText("one"))
			})
	})
}

func TestAnInstantIsStoredInTheSpellingTheProjectionDeclares(t *testing.T) {
	// The projection says a SQLite instant is text in RFC 3339 *because* text
	// sorts chronologically in that format. Leaving the spelling to the driver
	// broke that claim silently: modernc writes Go's default String, which
	// does not sort and which nothing reads back. So the module formats it,
	// and this is the claim.
	wanted := time.Date(2026, 9, 9, 21, 30, 0, 0, time.UTC)

	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	source := "file:" + t.TempDir() + "/spelling.db"
	type reading = effect.Effect[effect.Unit, sql.Fault, string]

	asText := schema.Struct[string]("row",
		schema.FieldOf("held", schema.Text(),
			func(held string) string { return held },
			func(held *string, value string) { *held = value }))

	program := effect.Scoped(func(scope effect.Scope) reading {
		return sql.Open[effect.Unit](scope, "sqlite", source).
			FlatMap(func(database *sql.Connected) reading {
				return sql.Execute[effect.Unit](database,
					`create table "spelling" ("starts_at" text not null)`).
					FlatMap(func(sql.Outcome) reading {
						return sql.Execute[effect.Unit](database,
							`insert into "spelling" ("starts_at") values (?)`,
							dynamic.OfTimestamp(wanted)).
							FlatMap(func(sql.Outcome) reading {
								return sql.QueryRow[effect.Unit](database, asText,
									`select cast("starts_at" as text) as "held" from "spelling"`)
							})
					})
			})
	})

	within, giveUp := context.WithTimeout(context.Background(), 10*time.Second)
	defer giveUp()

	exit := runtime.Run(within, effect.Unit{}, program)
	stored, succeeded := exit.Value()
	if !succeeded {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	if _, err := time.Parse(time.RFC3339Nano, stored); err != nil {
		t.Fatalf("expected RFC 3339 in the column, got %q", stored)
	}
	// And it sorts as text the way it sorts as time, which is the reason the
	// format was chosen.
	earlier := wanted.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if !(earlier < stored) {
		t.Fatalf("expected %q to sort before %q", earlier, stored)
	}
}
