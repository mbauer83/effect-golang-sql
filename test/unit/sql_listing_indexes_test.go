package unit

// A listing's indexes. What matters is that a listing is read by the indexes it
// is given -- one index may serve several listings, and one may include the
// columns a page reads -- and that only a listing that asks for its derived
// index gets one.

import (
	"errors"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

type logged struct {
	ID    int64
	Owner string
	Year  int64
}

var loggedSchema = schema.Struct[logged]("logged",
	schema.FieldAt("id", schema.Int64(), func(value *logged) *int64 { return &value.ID }).Identity(),
	schema.FieldAt("owner", schema.Text(), func(value *logged) *string { return &value.Owner }),
	schema.FieldAt("year", schema.Int64(), func(value *logged) *int64 { return &value.Year }))

var loggedSource = sql.From("logged",
	sql.ColumnOf("id", sql.OfWhole), sql.ColumnOf("owner", sql.OfText), sql.ColumnOf("year", sql.OfWhole))

func loggedListing() sql.Listing[logged] {
	return sql.NewListing(loggedSchema, loggedSource, "id").
		Sort("year", sql.Of[int64](loggedSource, "year").Ascending()).
		Within(sql.Equal(sql.Of[string](loggedSource, "owner"), sql.Param("ann")))
}

func TestAListingIsReadByTheIndexesItIsGiven(t *testing.T) {
	byOwnerYear := sql.IndexOn("logged_owner_year", "owner", "year", "id").WithInclude("title")
	// The same index serves two listings, stated once.
	for _, listing := range []sql.Listing[logged]{loggedListing().IndexedBy(byOwnerYear), loggedListing().IndexedBy(byOwnerYear)} {
		indexes := listing.Indexes()
		if len(indexes) != 1 || indexes[0].Name != "logged_owner_year" {
			t.Fatalf("expected the given index, got %+v", indexes)
		}
	}
	postgres, _ := ddl.CreateIndexes(ddl.Postgres, "logged", byOwnerYear)
	if !strings.Contains(postgres[0], `("owner", "year", "id") INCLUDE ("title")`) {
		t.Fatalf("expected Postgres to include the column apart from the key, got %s", postgres[0])
	}
	sqlite, _ := ddl.CreateIndexes(ddl.SQLite, "logged", byOwnerYear)
	if !strings.Contains(sqlite[0], `("owner", "year", "id", "title")`) {
		t.Fatalf("expected SQLite to carry it after the key, got %s", sqlite[0])
	}
	// Unique over its columns alone: Postgres carries the included column
	// besides, and a dialect without INCLUDE refuses, naming what to declare.
	unique := byOwnerYear
	unique.Unique = true
	if held, _ := ddl.CreateIndexes(ddl.Postgres, "logged", unique); len(held) != 1 ||
		!strings.Contains(held[0], `CREATE UNIQUE INDEX "logged_owner_year" ON "logged" ("owner", "year", "id") INCLUDE ("title")`) {
		t.Fatalf("expected one unique index with an included column, got %v", held)
	}
	for _, dialect := range []ddl.Dialect{ddl.SQLite, ddl.MySQL} {
		_, err := ddl.CreateIndexes(dialect, "logged", unique)
		if !errors.Is(err, ddl.ErrUniqueInclude) || !strings.Contains(err.Error(), "owner, year, id, title") {
			t.Errorf("%s: expected the unique index refused, naming the index to declare instead, got %v", dialect.Name(), err)
		}
	}
}

func TestOnlyAListingThatAsksIsGivenItsDerivedIndex(t *testing.T) {
	if indexes := loggedListing().Indexes(); len(indexes) != 0 {
		t.Fatalf("expected no index unless asked, got %+v", indexes)
	}
	derived := loggedListing().IndexedBy(sql.DerivedIndex).Indexes()
	if len(derived) != 1 || strings.Join(derived[0].Columns, ",") != "owner,year,id" {
		t.Fatalf("expected the scope, the sort and the key, got %+v", derived)
	}
}
