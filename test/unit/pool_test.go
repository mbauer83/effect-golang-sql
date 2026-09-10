package unit

// How many connections a program holds.

import (
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-sql/sql"
)

// An unbounded pool is not a default anybody wants: database/sql opens as
// many as are asked for at once, and a server has a limit. MEASURED against
// Postgres with forty simulated people, an unbounded pool grew past a hundred
// and the server answered "sorry, too many clients already" -- which reached
// a person as a 502 on an ordinary page.
func TestTheDefaultPoolIsBounded(t *testing.T) {
	holding := sql.ModestConnections()

	if holding.Most <= 0 {
		t.Fatal("the default pool is unbounded, which exhausts whatever it points at")
	}
	if holding.Most > 50 {
		t.Fatalf("the default pool holds %d, which is most of a default Postgres", holding.Most)
	}
	if holding.Idle <= 0 || holding.Idle > holding.Most {
		t.Fatalf("keeping %d idle of %d makes no sense", holding.Idle, holding.Most)
	}
	if holding.IdleFor <= 0 || holding.IdleFor > time.Hour {
		t.Fatalf("an idle connection is kept for %s", holding.IdleFor)
	}
}
