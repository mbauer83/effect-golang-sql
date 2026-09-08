// Package migrate applies an aggregate's history to a database, once.
//
// `evolve` says what changed and `ddl` says how to spell it; neither of them
// touches a database or remembers anything. This is the part that does: it
// keeps a ledger of which versions have been applied to which database, works
// out what is left to do, and does that and nothing else. Running it twice is
// running it once.
//
// It depends on the sql port and never on a driver, so the same migration runs
// against Postgres, MySQL and SQLite -- and the dialect it is given has to be
// the dialect the database actually is, which is the one thing here that
// nothing can check for you.
//
// # What it does not promise
//
// **DDL is not always transactional.** Postgres can roll a failed migration
// back; MySQL cannot, because its DDL commits as it goes. So each version's
// step is applied and recorded separately, and a failure part-way leaves the
// ledger accurate up to the last version that completed -- which is the most a
// migrator can offer where the database will not help it. Running again
// continues from there.
//
// **One at a time is the caller's to arrange.** Two processes migrating the
// same database at once is a race the ledger narrows and does not close: the
// row is taken for update inside a transaction, which serialises the *decision*
// on Postgres and MySQL, but MySQL's DDL commits outside it. A deployment that
// migrates from one place has nothing to worry about; one that migrates from
// every replica should take a lock of its own.
package migrate
