package unit

// A domain type as a table stores it. What matters is that the default costs
// nothing -- the domain's names, spelled as a table spells them -- that what a
// mapping states instead is exactly what it states, and that a row is read
// back through the domain's own constructor.

import (
	"errors"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// shelfItem is a domain type with a rule: its title is never blank.
type shelfItem struct {
	id        int64
	title     string
	shelvedAt string
}

var errBlank = errors.New("an item has a title")

func newShelfItem(id int64, title string, at string) (shelfItem, error) {
	if strings.TrimSpace(title) == "" {
		return shelfItem{}, errBlank
	}
	return shelfItem{id: id, title: title, shelvedAt: at}, nil
}

var shelfItemFields = struct {
	ID        schema.Field[shelfItem, int64]
	Title     schema.Field[shelfItem, string]
	ShelvedAt schema.Field[shelfItem, string]
}{
	ID:        schema.FieldOf("id", schema.Int64(), func(item shelfItem) int64 { return item.id }).Identity(),
	Title:     schema.FieldOf("title", schema.Text(), func(item shelfItem) string { return item.title }),
	ShelvedAt: schema.FieldOf("shelvedAt", schema.Text(), func(item shelfItem) string { return item.shelvedAt }),
}

var shelfItemSchema = schema.Object("ShelfItem", func(values schema.Values) (shelfItem, error) {
	f := shelfItemFields
	return newShelfItem(f.ID.Of(values), f.Title.Of(values), f.ShelvedAt.Of(values))
}, shelfItemFields.ID, shelfItemFields.Title, shelfItemFields.ShelvedAt)

func TestAMappingSpellsTheDomainsNamesAsATableDoes(t *testing.T) {
	stored := sql.Map(shelfItemSchema)

	if got := strings.Join(sql.Columns(stored.Schema()), ","); got != "id,title,shelved_at" {
		t.Fatalf("expected the domain's names in snake_case, got %s", got)
	}
	if stored.TableName() != "shelf_item" {
		t.Fatalf("expected the table named after the object, got %s", stored.TableName())
	}
	statements, err := ddl.Create(ddl.Postgres, stored.Schema().Structure())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statements[0], `CREATE TABLE "shelf_item"`) ||
		!strings.Contains(statements[0], `"shelved_at"`) {
		t.Fatalf("expected the table and its columns spelled that way, got %s", statements[0])
	}
}

func TestAMappingNamesExactlyWhatItNames(t *testing.T) {
	stored := sql.Map(shelfItemSchema).Table("shelf").Column(shelfItemFields.ID, "itemID")

	if got := strings.Join(sql.Columns(stored.Schema()), ","); got != "itemID,title,shelved_at" {
		t.Fatalf("expected the given name exactly and the rest spelled, got %s", got)
	}
	if stored.TableName() != "shelf" {
		t.Fatalf("expected the given table name, got %s", stored.TableName())
	}
}

func TestARowIsReadBackThroughTheDomainsConstructor(t *testing.T) {
	stored := sql.Map(shelfItemSchema).Schema()
	item, _ := newShelfItem(7, "Dune", "today")

	row, err := schema.ToDynamic(stored, item)
	if err != nil {
		t.Fatal(err)
	}
	read, err := schema.FromDynamic(stored, row)
	if err != nil || read != item {
		t.Fatalf("expected the item back, got %+v, %v", read, err)
	}

	// A row an older version wrote, with a blank title, is refused where it
	// is read.
	blank := shelfItem{id: 8, title: " ", shelvedAt: "today"}
	row, _ = schema.ToDynamic(stored, blank)
	if _, err := schema.FromDynamic(stored, row); err == nil || !strings.Contains(err.Error(), errBlank.Error()) {
		t.Fatalf("expected the constructor's refusal, got %v", err)
	}
}

func TestAFilterIsNamedByTheDomainsFieldHandle(t *testing.T) {
	stored := sql.Map(shelfItemSchema).Column(shelfItemFields.ID, "itemID")
	tables, err := ddl.Tables(ddl.Postgres, stored.Schema().Structure())
	if err != nil {
		t.Fatal(err)
	}
	source := tables[0].Source()
	reading := sql.SelectQuery{
		Select: sql.SelectColumns("title"),
		From:   source,
		Where: sql.And(
			sql.Equal(stored.Of(source, shelfItemFields.ShelvedAt), sql.Param("today")),
			sql.Equal(stored.Of(source, shelfItemFields.ID), sql.Param(int64(7)))),
	}
	held := reading.Statement(ddl.Postgres)
	if held.Err() != nil || !strings.Contains(held.Text(), `"shelved_at" = $1 AND "itemID" = $2`) {
		t.Fatalf("expected the fields' own columns, got %s, %v", held.Text(), held.Err())
	}
}
