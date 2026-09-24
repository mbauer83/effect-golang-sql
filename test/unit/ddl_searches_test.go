package unit

// What each dialect makes for a search, for the CI that runs without servers;
// the acceptance tests run the same statements against each server.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
)

func TestEachDialectMakesWhatASearchNeeds(t *testing.T) {
	prefix := sql.NewSearch("name", sql.PrefixMatch, "name")
	words := sql.NewSearch("words", sql.FullTextMatch, "name", "overview").WithLanguage("english")
	fragment := sql.NewSearch("fragment", sql.SubstringMatch, "name")
	cases := []struct {
		dialect  ddl.Dialect
		searches []sql.Search
		want     []string
	}{
		{ddl.Postgres, []sql.Search{prefix, words, fragment}, []string{
			`ADD COLUMN "name_lower" TEXT COLLATE "C" GENERATED ALWAYS AS (LOWER("name")) STORED`,
			`TSVECTOR GENERATED ALWAYS AS (TO_TSVECTOR('english', COALESCE("name", '') || ' ' || COALESCE("overview", ''))) STORED`,
			`CREATE INDEX "film_words_search" ON "film" USING GIN ("words_search")`,
			`USING GIN ("name" gin_trgm_ops)`,
		}},
		{ddl.MySQL, []sql.Search{prefix, words}, []string{
			"TEXT COLLATE utf8mb4_bin GENERATED ALWAYS AS (LOWER(`name`)) STORED",
			"CREATE INDEX `film_name_search` ON `film` (`name_lower`(191))",
			"CREATE FULLTEXT INDEX `film_words_search` ON `film` (`name`, `overview`)",
		}},
		{ddl.SQLite, []sql.Search{prefix, words}, []string{
			`GENERATED ALWAYS AS (LOWER("name")) VIRTUAL`,
			`CREATE VIRTUAL TABLE "film_words_search" USING fts5("name", "overview", content='film')`,
			`AFTER UPDATE ON "film"`,
			`VALUES ('rebuild')`,
		}},
	}
	for _, each := range cases {
		made, err := ddl.CreateSearches(each.dialect, "film", each.searches...)
		if err != nil {
			t.Fatalf("%s: %v", each.dialect.Name(), err)
		}
		joined := strings.Join(made, "\n")
		for _, want := range each.want {
			if !strings.Contains(joined, want) {
				t.Errorf("%s: expected %s among\n%s", each.dialect.Name(), want, joined)
			}
		}
	}
}

func TestEachDialectWritesTheSearchAQueryAsksFor(t *testing.T) {
	source := sql.From("film", sql.ColumnOf("id", sql.OfWhole), sql.ColumnOf("name", sql.OfText))
	listing := sql.NewListing(loggedSchema, source, "id").
		Sort("id", sql.Of[int64](source, "id").Ascending()).
		WithSearch(sql.NewSearch("words", sql.FullTextMatch, "name"), sql.NewSearch("name", sql.PrefixMatch, "name"))
	cases := map[ddl.Dialect]string{
		ddl.Postgres: `("film"."words_search" @@ PLAINTO_TSQUERY('simple', $1))`,
		ddl.MySQL:    "MATCH (`film`.`name`) AGAINST (CONCAT('+', REPLACE(?, ' ', ' +')) IN BOOLEAN MODE)",
		ddl.SQLite:   `"film"."rowid" IN (SELECT rowid FROM "film_words_search" WHERE "film_words_search" MATCH ?)`,
	}
	for dialect, want := range cases {
		statement := listing.Statement(dialect, sql.PageQuery{Where: listing.Match("words", "Alien -- planet!")})
		if err := statement.Err(); err != nil {
			t.Fatalf("%s: %v", dialect.Name(), err)
		}
		if !strings.Contains(statement.Text(), want) {
			t.Errorf("%s: expected %s in\n%s", dialect.Name(), want, statement.Text())
		}
		if values := statement.Values(); len(values) == 0 || !strings.Contains(fmt.Sprint(values[0]), "alien planet") {
			t.Errorf("%s: expected the words bound as they are searched by, got %v", dialect.Name(), values)
		}
	}
	prefix := listing.Statement(ddl.SQLite, sql.PageQuery{Where: listing.Match("name", "Ali")})
	if !strings.Contains(prefix.Text(), `("film"."name_lower" >= LOWER(?) AND "film"."name_lower" < LOWER(?))`) {
		t.Errorf("expected a prefix as a range of the lowercased column, got\n%s", prefix.Text())
	}
	if every := listing.Statement(ddl.SQLite, sql.PageQuery{Where: listing.Match("words", " -- ")}); strings.Contains(every.Text(), "MATCH") {
		t.Errorf("expected text with no word in it to be every row, got\n%s", every.Text())
	}
}
