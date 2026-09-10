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

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-sql/ddl"
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

func TestOnPostgresARefusalWhileStreamingReachesTheCallerAsARefusal(t *testing.T) {
	// The same fault in the other release this package has. A streamed read
	// that ends badly -- a row that will not decode, a refusal further down,
	// an interruption -- closes its cursor on the way out, and a cursor whose
	// context has been cancelled reports the context. Read as a failure, that
	// put a defect beside every such refusal.
	address := os.Getenv("EFFECT_GOLANG_POSTGRES_URL")
	if address == "" {
		t.Skip("set EFFECT_GOLANG_POSTGRES_URL to run this against a real server")
	}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("the reader decided not to")

	program := effect.Scoped(func(scope effect.Scope) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
		return sql.Open[effect.Unit](scope, "pgx", address).
			FlatMap(func(connected *sql.Connected) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
				// Many rows, so the cursor is still open when the reading
				// gives up: a stream that had already finished would have
				// closed it before the context went.
				streamed := sql.Rows[effect.Unit](connected, countedSchema,
					sql.Compose(ddl.Postgres,
						sql.Text(`select generate_series(1, 5000) as counted`)))
				return effect.RunForEach(streamed,
					func(counted) effect.Effect[effect.Unit, sql.Fault, effect.Unit] {
						return effect.Fail[effect.Unit, effect.Unit](
							sql.Fault{Doing: "reading", Err: refused})
					})
			})
	})

	exit := runtime.Run(context.Background(), effect.Unit{}, program)
	cause, failed := exit.Cause()
	if !failed {
		t.Fatal("expected the refusal to reach the caller")
	}
	if cause.ContainsDefect() {
		t.Fatalf("expected a refusal and nothing else, got %s", cause.String())
	}
}

// counted is one row of a series, which is enough of a shape to stream.
type counted struct {
	Counted int32
}

var countedSchema = schema.Struct[counted]("counted",
	schema.FieldOf("counted", schema.Int32(),
		func(row counted) int32 { return row.Counted },
		func(row *counted, value int32) { row.Counted = value }),
)
