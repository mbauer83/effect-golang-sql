package sql

// A repository's operations, as effects that require the Session they run
// against. Each writing operation is one transaction of its own, or part of
// the one InTransaction runs it in.

import "github.com/mbauer83/effect-golang/effect"

// Save writes the whole aggregate, as a delta: what is kept is read -- one
// statement per table -- and compared by key. A row gone is deleted, a row new
// is inserted, a row changed is updated, a row the same is left alone; an
// element of a list keeps its place unless it moved.
func (repository Repository[A, ID]) Save(value A) effect.Effect[Session, Fault, effect.Unit] {
	return withSession(func(session Session) effect.Effect[Session, Fault, effect.Unit] {
		return repository.saveIn[Session](session.Database, session.Dialect, value)
	})
}

// SaveRoot writes the aggregate's root row alone -- inserted, or replaced
// under the same identity -- and leaves what is beneath it as it is: one
// statement, unless the root has a unique key besides its identity.
func (repository Repository[A, ID]) SaveRoot(value A) effect.Effect[Session, Fault, effect.Unit] {
	return withSession(func(session Session) effect.Effect[Session, Fault, effect.Unit] {
		return repository.saveRootIn[Session](session.Database, session.Dialect, value)
	})
}

// SaveChanges writes what changed between two values of one aggregate
// without reading it: before is what is stored, after what is to be.
func (repository Repository[A, ID]) SaveChanges(before A, after A) effect.Effect[Session, Fault, effect.Unit] {
	return withSession(func(session Session) effect.Effect[Session, Fault, effect.Unit] {
		return repository.saveChangesIn[Session](session.Database, session.Dialect, before, after)
	})
}

// Insert writes a new aggregate, leaving to the database the columns it fills.
func (repository Repository[A, ID]) Insert(value A) effect.Effect[Session, Fault, effect.Unit] {
	return withSession(func(session Session) effect.Effect[Session, Fault, effect.Unit] {
		return repository.insertIn[Session](session.Database, session.Dialect, value)
	})
}

// Find is the aggregate with that identity, whole, or a fault that is
// ErrNoRows when none is kept.
func (repository Repository[A, ID]) Find(identity ID) effect.Effect[Session, Fault, A] {
	return withSession(func(session Session) effect.Effect[Session, Fault, A] {
		return repository.findIn[Session](session.Database, session.Dialect, identity)
	})
}

// FindOneBy is the one aggregate whose root the criterion finds, whole.
func (repository Repository[A, ID]) FindOneBy(where Criterion) effect.Effect[Session, Fault, A] {
	return withSession(func(session Session) effect.Effect[Session, Fault, A] {
		return repository.findOneByIn[Session](session.Database, session.Dialect, where)
	})
}

// FindBy is every aggregate whose root the criterion finds, whole, in that
// order.
func (repository Repository[A, ID]) FindBy(where Criterion, order ...Ordering) effect.Effect[Session, Fault, []A] {
	return withSession(func(session Session) effect.Effect[Session, Fault, []A] {
		return repository.findByIn[Session](session.Database, session.Dialect, where, order...)
	})
}

// Delete removes the aggregate with that identity and everything beneath it;
// the outcome says whether one was kept.
func (repository Repository[A, ID]) Delete(identity ID) effect.Effect[Session, Fault, Outcome] {
	return withSession(func(session Session) effect.Effect[Session, Fault, Outcome] {
		return repository.deleteIn[Session](session.Database, session.Dialect, identity)
	})
}

// Check says whether a dialect can keep what the repository describes: for a
// program to ask where it is wired, rather than at its first request.
func (repository Repository[A, ID]) Check(dialect Spelling) error {
	_, err := repository.layout(dialect)
	return err
}

// Tables are the tables a dialect keeps the aggregate in, root first: for a
// read model that joins them.
func (repository Repository[A, ID]) Tables(dialect Spelling) ([]TableLayout, error) {
	return repository.layout(dialect)
}
