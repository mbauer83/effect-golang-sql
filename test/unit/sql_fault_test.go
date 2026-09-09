package unit

// What a fault says, and what a caller can ask it.

import (
	"errors"
	"testing"

	"github.com/mbauer83/effect-golang-sql/sql"
)

func TestAskingForOneRowHasThreeAnswersAndACallerCanTellThemApart(t *testing.T) {
	// A repository asking for one row by its identity needs all three: the
	// thing is there, the thing is not there, or the database could not be
	// reached. Without the first two being nameable, a film nobody has saved
	// and a database that is down are the same event to whoever asked -- and
	// what to do about those two is not the same.
	held := sql.Fault{
		Doing:     "reading one row",
		Statement: `select "title" from "books" where "title" = ?`,
		Err:       sql.ErrNoRows,
	}
	if !errors.Is(held, sql.ErrNoRows) {
		t.Fatal("expected a caller to recognise no rows through the fault")
	}
	if errors.Is(held, sql.ErrSeveralRows) {
		t.Fatal("expected the two refusals to stay apart")
	}

	several := sql.Fault{Doing: "reading one row", Err: sql.ErrSeveralRows}
	if !errors.Is(several, sql.ErrSeveralRows) {
		t.Fatal("expected a caller to recognise several rows through the fault")
	}

	// And a driver's own failure is neither, which is what makes it the third
	// answer rather than a special case of the first.
	fromDriver := sql.Fault{Doing: "executing", Err: errors.New("connection refused")}
	if errors.Is(fromDriver, sql.ErrNoRows) || errors.Is(fromDriver, sql.ErrSeveralRows) {
		t.Fatal("expected a driver's own failure to be neither refusal")
	}
}
