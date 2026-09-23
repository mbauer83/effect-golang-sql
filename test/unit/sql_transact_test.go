package unit

// A transaction is a scoped resource: it commits when the work succeeds and
// rolls back when it fails or is interrupted, and the read that decides a write
// goes through the same transaction the write does.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

type bankEffect[A any] = effect.Effect[effect.Unit, sql.Fault, A]

func itself(fault sql.Fault) sql.Fault { return fault }

func TestWorkThatSucceedsIsCommitted(t *testing.T) {
	kept := &recorder{}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}

	exit := runtime.Run(context.Background(), effect.Unit{},
		sql.Transact(kept, itself, func(within sql.Querier) bankEffect[sql.Outcome] {
			return sql.Execute[effect.Unit](within, "update accounts set balance = 0")
		}))

	if _, succeeded := exit.Value(); !succeeded {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	begun, committed, rolledBack := kept.counts()
	if begun != 1 || committed != 1 || rolledBack != 0 {
		t.Fatalf("expected one begun and committed, got %d begun, %d committed, %d rolled back",
			begun, committed, rolledBack)
	}
}

func TestWorkThatFailsIsRolledBack(t *testing.T) {
	kept := &recorder{}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	refused := sql.Fault{Op: "deciding", Err: errors.New("the account is frozen")}

	exit := runtime.Run(context.Background(), effect.Unit{},
		sql.Transact(kept, itself, func(within sql.Querier) bankEffect[sql.Outcome] {
			return sql.Execute[effect.Unit](within, "update accounts set balance = 0").
				FlatMap(func(sql.Outcome) bankEffect[sql.Outcome] {
					return effect.For[effect.Unit, sql.Fault]().Fail[sql.Outcome](refused)
				})
		}))

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the refusal to reach the caller, got %+v", exit)
	}
	if failures := cause.Failures(); len(failures) != 1 || failures[0].Op != "deciding" {
		t.Fatalf("expected the work's own failure, got %+v", cause)
	}
	// And nothing else. Asking only what Failures holds is what let a defect
	// ride along beside the refusal for as long as it did: a cause that
	// contains one is answered by a boundary as a five hundred, so the
	// refusal a caller was meant to act on never reached them.
	if cause.ContainsDefect() {
		t.Fatalf("expected the refusal and nothing else, got %s", cause.String())
	}
	_, committed, rolledBack := kept.counts()
	if committed != 0 || rolledBack != 1 {
		t.Fatalf("expected a rollback and no commit, got %d committed, %d rolled back",
			committed, rolledBack)
	}
}

func TestWorkThatIsInterruptedIsRolledBack(t *testing.T) {
	// The release is what covers this: there is no failure to react to and no
	// commit on the way out, so a transaction with nothing watching it would
	// simply be left open holding its locks.
	kept := &recorder{}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	stopped, stop := context.WithCancel(context.Background())

	exit := runtime.Run(stopped, effect.Unit{},
		sql.Transact(kept, itself, func(within sql.Querier) bankEffect[sql.Outcome] {
			return sql.Execute[effect.Unit](within, "update accounts set balance = 0").
				FlatMap(func(sql.Outcome) bankEffect[sql.Outcome] {
					stop()
					return effect.For[effect.Unit, sql.Fault]().
						CheckInterrupt().As(sql.Outcome{})
				})
		}))

	if _, succeeded := exit.Value(); succeeded {
		t.Fatalf("expected the interruption to end the work, got %+v", exit)
	}
	_, committed, rolledBack := kept.counts()
	if committed != 0 || rolledBack != 1 {
		t.Fatalf("expected a rollback and no commit, got %d committed, %d rolled back",
			committed, rolledBack)
	}
}

func TestAReadAndTheWriteItDecidesGoThroughTheSameTransaction(t *testing.T) {
	// The case a transaction exists for: what the write does depends on what
	// the read saw, so the two have to be one. It works because a transaction
	// answers the same operations a database does -- QueryRow does not know it
	// is inside one -- and this is the witness that it went there, since the
	// recorded statements are the transaction's own.
	kept := &recorder{rows: []dynamic.Object{{Fields: []dynamic.Field{
		{Name: "account", Value: dynamic.OfText("held")},
		{Name: "balance", Value: dynamic.OfInteger(30)},
	}}}}
	runtime, err := effect.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}

	exit := runtime.Run(context.Background(), effect.Unit{},
		sql.Transact(kept, itself, func(within sql.Querier) bankEffect[sql.Outcome] {
			return sql.QueryRow[effect.Unit](within, talliedSchema,
				"select account, balance from ledger where account = ?",
				dynamic.OfText("held")).
				FlatMap(func(row tally) bankEffect[sql.Outcome] {
					if row.Balance < 10 {
						return effect.For[effect.Unit, sql.Fault]().
							Fail[sql.Outcome](sql.Fault{Op: "deciding", Err: errShort})
					}
					return sql.Execute[effect.Unit](within,
						"update ledger set balance = balance - 10 where account = ?",
						dynamic.OfText("held"))
				})
		}))

	if _, succeeded := exit.Value(); !succeeded {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	asked := kept.queries()
	if len(asked) != 2 ||
		!strings.HasPrefix(asked[0], "select") || !strings.HasPrefix(asked[1], "update") {
		t.Fatalf("expected the read and the write on the transaction, got %v", asked)
	}
	_, committed, rolledBack := kept.counts()
	if committed != 1 || rolledBack != 0 {
		t.Fatalf("expected one commit and no rollback, got %d committed, %d rolled back",
			committed, rolledBack)
	}
}

var errShort = errors.New("the balance would go below zero")
