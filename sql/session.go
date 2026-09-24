package sql

// What repositories, listings and collections run against.
//
// Their operations are effects that require a Session, the way ZIO's require a
// DataSource: the requirement channel is the reader, so a declaration made
// once is run against whatever a program supplies where it is wired -- a
// test's SQLite, a deployment's Postgres -- and a transaction is a Session
// whose database is the transaction.

import "github.com/mbauer83/effect-golang/effect"

// Session is a database, or a transaction in one, and the dialect its
// statements are spelled in.
type Session struct {
	Database Querier
	Dialect  Spelling
}

// InTransaction runs work in one transaction: every repository, listing and
// collection operation in it reads and writes through the transaction, and it
// commits when the work succeeds and rolls back when it fails. Work already
// in a transaction runs in that one.
func InTransaction[A any](work effect.Effect[Session, Fault, A]) effect.Effect[Session, Fault, A] {
	return withSession(func(session Session) effect.Effect[Session, Fault, A] {
		beginner, can := session.Database.(Beginner)
		if !can {
			return work
		}
		return Transact(beginner, func(fault Fault) Fault { return fault },
			func(transaction Querier) effect.Effect[Session, Fault, A] {
				return work.ContramapEnv(func(Session) Session {
					return Session{Database: transaction, Dialect: session.Dialect}
				})
			})
	})
}

// withSession is an effect of the session it runs in.
func withSession[A any](work func(Session) effect.Effect[Session, Fault, A]) effect.Effect[Session, Fault, A] {
	return effect.Environment[Session, Fault]().FlatMap(work)
}

// OpenSession connects to a database for as long as the scope lasts, as the
// Session its statements are spelled for: the dialect is the database's, so it
// is said where the database is.
func OpenSession[R any](scope effect.Scope, dialect Spelling, driver string, source string) effect.Effect[R, Fault, Session] {
	return Open[R](scope, driver, source).
		Map(func(database *Database) Session { return Session{Database: database, Dialect: dialect} })
}

// SessionLayer is a Session as a layer: a database connected when a program
// that requires it starts, and closed when it ends. A deployment and a test
// provide different ones to the same program -- Postgres from its address,
// SQLite in a file the test owns.
func SessionLayer(dialect Spelling, driver string, source string) effect.Layer[effect.Unit, Fault, Session] {
	return effect.LayerScoped(func(scope effect.Scope) effect.Effect[effect.Unit, Fault, Session] {
		return OpenSession[effect.Unit](scope, dialect, driver, source)
	})
}
