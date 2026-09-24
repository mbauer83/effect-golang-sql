package acceptance

// A database that counts the statements that write, for the tests that say
// how many a change costs.

import (
	"context"
	"sync/atomic"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// countingDatabase is a database that counts the statements that write.
type countingDatabase struct {
	*sql.Database
	writes *atomic.Int64
}

// Begin is a transaction whose writes are counted too.
func (database countingDatabase) Begin(ctx context.Context) (sql.Transaction, error) {
	transaction, err := database.Database.Begin(ctx)
	return countingTransaction{Transaction: transaction, writes: database.writes}, err
}

type countingTransaction struct {
	sql.Transaction
	writes *atomic.Int64
}

func (transaction countingTransaction) Execute(ctx context.Context, statement string, arguments []dynamic.Value) (sql.Outcome, error) {
	transaction.writes.Add(1)
	return transaction.Transaction.Execute(ctx, statement, arguments)
}

func (database countingDatabase) Execute(ctx context.Context, statement string, arguments []dynamic.Value) (sql.Outcome, error) {
	database.writes.Add(1)
	return database.Database.Execute(ctx, statement, arguments)
}
