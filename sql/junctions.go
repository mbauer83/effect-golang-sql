package sql

// How criteria are joined, and where a bracket goes.
//
// Joining is an operation like any other -- a dialect writes the word -- and
// so is a bracket, which is why there is no shape here for either: a
// conjunction is an application of one operation over criteria, each of which
// has been bracketed if it was a junction itself.
//
// Bracketing every junction and nothing else is deliberate. "and" binds
// tighter than "or", so a disjunction inside a conjunction is a different
// criterion without brackets; working out which direction needs them is
// arithmetic on precedence levels in aid of leaving out a character.

var (
	// Conjunction and Disjunction join criteria, and Negation reverses one.
	// Ordinary, because every server spells all three the same way.
	Conjunction = Declare("join criteria with and").WithDefault(Weave("", " AND ", ""))
	Disjunction = Declare("join criteria with or").WithDefault(Weave("", " OR ", ""))
	Negation    = Declare("reverse a criterion").WithDefault(Phrase("NOT (", ")"))
	// Bracket is a criterion inside another one.
	Bracket = Declare("bracket a criterion").WithDefault(Phrase("(", ")"))
)

// IsEmpty reports whether this expression was never said.
//
// For a criterion that is "excludes nothing", which is what a query with no
// filter means -- and it is worth asking, because a shape that ands its own
// criterion into whatever a caller supplied should not write a clause when the
// caller supplied nothing. A conjunction of unsaid criteria is unsaid, and so
// is a disjunction with one unsaid member: any row satisfies it.
func (expr Expr[A]) IsEmpty() bool { return expr.node.isEmpty() }

func (expr node) isEmpty() bool {
	if expr.kind == noExpression {
		return true
	}
	if expr.kind != anApplication {
		return false
	}
	switch expr.operation {
	case Conjunction:
		for _, one := range expr.arguments {
			if !one.isEmpty() {
				return false
			}
		}
		return true
	case Disjunction:
		for _, one := range expr.arguments {
			if one.isEmpty() {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// junction is the criteria joined by that operation, each bracketed if it is a
// junction itself.
//
// A conjunct that excludes nothing is dropped, because anding it in would only
// add brackets. Nothing left is every row for a conjunction and no row for a
// disjunction, which is the reading each of them gives an empty set. One
// member is that member: a bracket around it would be a bracket around the
// whole criterion.
func junction(operation Operation, criteria []Criterion) Criterion {
	nonEmpty := make([]Criterion, 0, len(criteria))
	for _, one := range criteria {
		if operation == Conjunction && one.IsEmpty() {
			continue
		}
		nonEmpty = append(nonEmpty, one)
	}
	switch len(nonEmpty) {
	case 0:
		if operation == Conjunction {
			return All()
		}
		return None()
	case 1:
		// One member is that member. Bracketing it would bracket the whole
		// criterion, which every clause that wrote this would then carry for
		// no reader's benefit.
		return nonEmpty[0]
	}
	members := make([]Term, 0, len(nonEmpty))
	for _, one := range nonEmpty {
		members = append(members, asMember(one))
	}
	return Apply[bool](operation, members...)
}

// asMember is a criterion as a member of another, in brackets when it is a
// junction and bare when it is a comparison.
func asMember(criterion Criterion) Term {
	if !isJunction(criterion.node) {
		return criterion.Term()
	}
	return Apply[bool](Bracket, criterion.Term()).Term()
}

func isJunction(expr node) bool {
	if expr.kind != anApplication {
		return false
	}
	return expr.operation == Conjunction || expr.operation == Disjunction
}
