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

	if holding.MaxOpen <= 0 {
		t.Fatal("the default pool is unbounded, which exhausts whatever it points at")
	}
	if holding.MaxOpen > 50 {
		t.Fatalf("the default pool holds %d, which is most of a default Postgres", holding.MaxOpen)
	}
	if holding.MaxIdle <= 0 || holding.MaxIdle > holding.MaxOpen {
		t.Fatalf("keeping %d idle of %d makes no sense", holding.MaxIdle, holding.MaxOpen)
	}
	if holding.MaxIdleTime <= 0 || holding.MaxIdleTime > time.Hour {
		t.Fatalf("an idle connection is kept for %s", holding.MaxIdleTime)
	}
}
