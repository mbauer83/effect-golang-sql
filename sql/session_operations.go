package sql

// A listing's and a lazy collection's operations, as effects that require
// the Session they run against.

import "github.com/mbauer83/effect-golang/effect"

// Page reads one page of the listing.
func (listing Listing[A]) Page(query PageQuery) effect.Effect[Session, Fault, Page[A]] {
	return withSession(func(session Session) effect.Effect[Session, Fault, Page[A]] {
		return listing.pageIn[Session](session.Database, session.Dialect, query)
	})
}

// Count is how many rows the listing holds that where admits.
func (listing Listing[A]) Count(where Criterion) effect.Effect[Session, Fault, int64] {
	return withSession(func(session Session) effect.Effect[Session, Fault, int64] {
		return listing.countIn[Session](session.Database, session.Dialect, where)
	})
}

// CountUpTo is how many rows where admits, counting no further than most.
func (listing Listing[A]) CountUpTo(where Criterion, most int) effect.Effect[Session, Fault, int64] {
	return withSession(func(session Session) effect.Effect[Session, Fault, int64] {
		return listing.countUpToIn[Session](session.Database, session.Dialect, where, most)
	})
}

// Insert puts element into owner's collection at a place.
func (collection Collection[ID, E]) Insert(owner ID, element E, at Placement[E]) effect.Effect[Session, Fault, effect.Unit] {
	return withSession(func(session Session) effect.Effect[Session, Fault, effect.Unit] {
		return collection.insertIn[Session](session.Database, session.Dialect, owner, element, at)
	})
}

// Move puts an element owner's collection holds at another place.
func (collection Collection[ID, E]) Move(owner ID, element E, to Placement[E]) effect.Effect[Session, Fault, effect.Unit] {
	return withSession(func(session Session) effect.Effect[Session, Fault, effect.Unit] {
		return collection.moveIn[Session](session.Database, session.Dialect, owner, element, to)
	})
}

// Remove takes element out of owner's collection.
func (collection Collection[ID, E]) Remove(owner ID, element E) effect.Effect[Session, Fault, effect.Unit] {
	return withSession(func(session Session) effect.Effect[Session, Fault, effect.Unit] {
		return collection.removeIn[Session](session.Database, session.Dialect, owner, element)
	})
}

// Has reports whether owner's collection holds element.
func (collection Collection[ID, E]) Has(owner ID, element E) effect.Effect[Session, Fault, bool] {
	return withSession(func(session Session) effect.Effect[Session, Fault, bool] {
		return collection.hasIn[Session](session.Database, session.Dialect, owner, element)
	})
}
