// Package evolve carries a description from one version to the next, and the
// values with it.
//
// A migration here is not a diff. Two industry camps exist -- versioned, a
// script per change, and declarative, a desired state compared against whatever
// the database happens to hold -- and the second is candid that its plans are
// non-deterministic because they depend on the state they find. Neither is
// this: a step is a **declared** list of changes between two pinned versions,
// so nothing is inferred, a rename is exact rather than a question somebody is
// asked interactively, and a plan is the same every time because it never
// consults a database to decide what to do.
//
// Only version one and the steps are written. Every later version is **derived**
// by applying the steps, which is what stops a declaration and a step from
// disagreeing about what the declaration became -- there is nothing to
// disagree with. Materialise a derived version with schemagen if a Go type for
// it is wanted, the way any other generated artifact is materialised.
//
// The steps run both ways. A change knows its inverse, so N-1 steps give every
// one of the N-squared version pairs by composition, in either direction. That
// is a bidirectional lens, and the plan's section 7.2 records where the idea
// came from.
//
// What a down migration cannot do is invent data. Dropping a column loses what
// was in it, so putting the column back restores the shape and not the values,
// and this says so rather than implying that a migration is reversible.
package evolve
