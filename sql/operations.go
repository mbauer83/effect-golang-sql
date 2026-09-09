package sql

// What a database can do to a value, and how each dialect writes it.
//
// This is the extensible half of the specification, and it has to be
// extensible: the set of operations a server offers is that server's, it grows
// between versions, and no interface written here could name them all. Two of
// the three concatenate with an operator and the third with a function; one
// measures a string in characters under a different name than the other two;
// one has no regular expression at all. Holding that in a method per operation
// would need a new method, and a new implementation in every dialect, for each
// one somebody wanted.
//
// So an operation is a value. A dialect is asked whether it writes that value
// and how; one that does not is a refusal naming the dialect and the
// operation, rather than a statement a server rejects. An operation this
// module does not name is Declaring; a dialect that writes one this module's
// dialects do not is Also. Neither needs anything here to change.

import (
	"fmt"
)

// Operation is one thing a database can do to a value.
//
// Opaque and compared by identity, so an operation is something a program
// holds rather than something it spells: two uses are the same operation when
// they are the same value, a dialect answers about the value, and there is no
// string a typo can turn into an operation nobody offers. Comparable, so a
// dialect answers with a switch.
type Operation struct {
	held *operating
}

type operating struct {
	named      string
	ordinarily Written
}

// Declaring is how an operation comes to exist.
//
// The value it returns is the operation's identity, so it is declared once, in
// a package-level variable, and passed around. Two calls with the same name
// are two different operations -- which is the point of identity rather than
// spelling: a program cannot accidentally name somebody else's.
func Declaring(name string) Operation {
	return Operation{held: &operating{named: name}}
}

// Ordinarily is this operation with a spelling every dialect is taken to use
// unless it says otherwise.
//
// Most operations are ordinary: lower(x) is lower(x) everywhere, and a dialect
// having to say so would be a dialect answering thirty questions to disagree
// about three. A dialect's own answer always wins, so an ordinary spelling is
// a default and never a claim about a server.
func (operation Operation) Ordinarily(written Written) Operation {
	operation.held.ordinarily = written
	return operation
}

// Named is what to call this operation in a refusal. Not its identity.
func (operation Operation) Named() string {
	if operation.held == nil {
		return "an operation nobody declared"
	}
	return operation.held.named
}

// Applied is one use of an operation: what is being done, to what, and
// whatever the use has to say rather than bind.
//
// The arguments arrive as pieces rather than as text, because an argument may
// bind a value and the ordinal a dialect gives that value is decided by the
// Compose this ends up in -- so a dialect splices pieces and never counts
// them.
type Applied struct {
	Operation Operation
	// Detail is what this use needs said rather than bound: the separator of a
	// joined column, the format of a rendered date. Said, because no dialect
	// binds a keyword, and a dialect writes it in its own quoting.
	Detail string
	Over   [][]Part
}

// Written is how one dialect writes one use of an operation.
type Written func(spelling Spelling, applied Applied) []Part

// Operations is what a dialect can do to a value.
//
// One method, and absence is the boolean: a dialect says how it writes an
// operation, or says nothing and the statement carries the refusal. That is
// the whole port, so a dialect written elsewhere is a switch and a quoting
// rule.
type Operations interface {
	Writes(operation Operation) (Written, bool)
}

// Also is a dialect that writes these operations too, and answers as it did
// for everything else.
//
// The seam for an affordance this module does not name and a dialect it did
// not write: a program that needs one server's own function declares the
// operation and wraps the dialect it already has. The entries win over the
// dialect's own, so an answer can be corrected as well as added.
func Also(spelling Spelling, written map[Operation]Written) Spelling {
	return extended{Spelling: spelling, written: written}
}

type extended struct {
	Spelling
	written map[Operation]Written
}

func (held extended) Writes(operation Operation) (Written, bool) {
	if answer, known := held.written[operation]; known {
		return answer, true
	}
	return held.Spelling.Writes(operation)
}

// applying is one use of an operation as the dialect writes it, or the refusal
// that nothing knows how to write it.
func applying(spelling Spelling, applied Applied) []Part {
	if written, known := spelling.Writes(applied.Operation); known {
		return written(spelling, applied)
	}
	if applied.Operation.held != nil && applied.Operation.held.ordinarily != nil {
		return applied.Operation.held.ordinarily(spelling, applied)
	}
	return []Part{Refused(fmt.Errorf("sql: %s cannot %s",
		spelling.Name(), applied.Operation.Named()))}
}
