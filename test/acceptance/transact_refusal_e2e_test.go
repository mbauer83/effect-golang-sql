package acceptance

// What reaches a caller when work inside a transaction refuses.
//
// Against a real server, because the failure this states was invisible on a
// fake: a rollback that cannot run turns a typed refusal into a cause carrying
// a defect, and whether the rollback can run depends on the driver and on the
// context it is given.

import (
	"context"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

func TestOnPostgresARefusalInsideATransactionReachesTheCallerAsARefusal(t *testing.T) {
	address := os.Getenv("EFFECT_GOLANG_POSTGRES_URL")
	if address == "" {
		t.Skip("set EFFECT_GOLANG_POSTGRES_URL to run this against a real server")
	}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("the work decided not to")

	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
		return sql.Open[effect.Unit](scope, "pgx", address).
			FlatMap(func(connected *sql.Connected) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
				return sql.Transact(connected, itself,
					func(within sql.Querying) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
						return sql.Execute[effect.Unit](within, `select 1`).
							FlatMap(func(sql.Outcome) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
								return effect.Fail[effect.Unit, effect.Unit](
									sql.Fault{Doing: "deciding", Err: refused})
							})
					})
			})
	})

	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatal("expected the refusal to reach the caller")
	}
	// The whole point: one refusal, and no defect riding along with it. A
	// cause that contains a defect is answered by a boundary as a five
	// hundred, so a refusal a caller was meant to act on becomes "this system
	// broke".
	if cause.ContainsDefect() {
		t.Fatalf("expected a refusal and nothing else, got %s", cause.String())
	}
	if failures := cause.Failures(); len(failures) != 1 || failures[0].Doing != "deciding" {
		t.Fatalf("expected the work's own refusal, got %s", cause.String())
	}
}

// itself is the failure mapping for a transaction whose work already fails
// with the package's own fault.
func itself(faulted sql.Fault) sql.Fault { return faulted }
