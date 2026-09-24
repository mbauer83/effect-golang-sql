package unit

// Value objects in a table. What matters is that a value object is stored as a
// column per member named after the field that holds it, that it comes back as
// the value it was, that an absent one is stored as nulls and comes back
// absent, and that a mapping can keep one whole instead.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

type frame struct {
	Poster   string
	Backdrop string
}

var frameSchema = schema.Struct[frame]("frame",
	schema.FieldAt("poster", schema.Text(), func(value *frame) *string { return &value.Poster }),
	schema.FieldAt("backdrop", schema.Text(), func(value *frame) *string { return &value.Backdrop }),
)

type still struct {
	ID       int64
	Title    string
	Artwork  frame
	Fallback *frame
}

var stillFields = struct {
	ID       schema.Field[still, int64]
	Title    schema.Field[still, string]
	Artwork  schema.Field[still, frame]
	Fallback schema.Field[still, frame]
}{
	ID:      schema.FieldAt("id", schema.Int64(), func(value *still) *int64 { return &value.ID }).Identity(),
	Title:   schema.FieldAt("title", schema.Text(), func(value *still) *string { return &value.Title }),
	Artwork: schema.FieldAt("artwork", frameSchema, func(value *still) *frame { return &value.Artwork }),
	Fallback: schema.OptionalFieldOf("fallbackArtwork", frameSchema,
		func(value still) (frame, bool) {
			if value.Fallback == nil {
				return frame{}, false
			}
			return *value.Fallback, true
		},
		func(value *still, held frame) { value.Fallback = &held }),
}

var stillSchema = schema.Struct[still]("still",
	stillFields.ID, stillFields.Title, stillFields.Artwork, stillFields.Fallback)

func TestAValueObjectIsAColumnPerMember(t *testing.T) {
	columns := strings.Join(sql.Columns(sql.Map(stillSchema).Schema()), ",")
	want := "id,title,artwork_poster,artwork_backdrop,fallback_artwork_poster,fallback_artwork_backdrop"
	if columns != want {
		t.Fatalf("expected\n\t%s\ngot\n\t%s", want, columns)
	}
	statements, err := ddl.Create(ddl.Postgres, sql.Map(stillSchema).Schema().Structure())
	if err != nil {
		t.Fatal(err)
	}
	// The members of a value object that may be absent may be absent too.
	if !strings.Contains(statements[0], `"artwork_poster" TEXT NOT NULL`) ||
		strings.Contains(statements[0], `"fallback_artwork_poster" TEXT NOT NULL`) {
		t.Fatalf("expected required and nullable columns, got\n%s", statements[0])
	}
}

func TestAFlattenedValueComesBackAsItWas(t *testing.T) {
	stored := sql.Map(stillSchema).Schema()
	for _, value := range []still{
		{ID: 1, Title: "Alien", Artwork: frame{Poster: "/a.jpg", Backdrop: "/b.jpg"}},
		{ID: 2, Title: "Heat", Artwork: frame{Poster: "/c.jpg"}, Fallback: &frame{Poster: "/d.jpg"}},
	} {
		row, err := schema.ToDynamic(stored, value)
		if err != nil {
			t.Fatal(err)
		}
		read, err := schema.FromDynamic(stored, row)
		if err != nil {
			t.Fatal(err)
		}
		if read.Title != value.Title || read.Artwork != value.Artwork ||
			(value.Fallback == nil) != (read.Fallback == nil) ||
			(value.Fallback != nil && *read.Fallback != *value.Fallback) {
			t.Fatalf("expected %+v back, got %+v", value, read)
		}
	}
}

func TestAMappingKeepsAValueObjectWholeWhenAsked(t *testing.T) {
	stored := sql.Map(stillSchema).AsDocument(stillFields.Artwork).Schema()
	columns := strings.Join(sql.Columns(stored), ",")
	if columns != "id,title,artwork,fallback_artwork_poster,fallback_artwork_backdrop" {
		t.Fatalf("expected the artwork as one column, got %s", columns)
	}
}
