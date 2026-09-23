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
// module does not name is Declare; a dialect that writes one this module's
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
	definition *definition
}

type definition struct {
	name     string
	standard Syntax
}

// Declare is how an operation comes to exist.
//
// The value it returns is the operation's identity, so it is declared once, in
// a package-level variable, and passed around. Two calls with the same name
// are two different operations -- which is the point of identity rather than
// spelling: a program cannot accidentally name somebody else's.
func Declare(name string) Operation {
	return Operation{definition: &definition{name: name}}
}

// WithDefault is this operation with a spelling every dialect is taken to use
// unless it says otherwise.
//
// Most operations are ordinary: lower(x) is lower(x) everywhere, and a dialect
// having to say so would be a dialect answering thirty questions to disagree
// about three. A dialect's own answer always wins, so an ordinary spelling is
// a default and never a claim about a server.
func (operation Operation) WithDefault(syntax Syntax) Operation {
	operation.definition.standard = syntax
	return operation
}

// String is what to call this operation in a refusal. Not its identity.
func (operation Operation) String() string {
	if operation.definition == nil {
		return "an operation nobody declared"
	}
	return operation.definition.name
}

// Application is one use of an operation: what is being done, to what, and
// whatever the use has to say rather than bind.
//
// The arguments arrive as pieces rather than as text, because an argument may
// bind a value and the ordinal a dialect gives that value is decided by the
// Compose this ends up in -- so a dialect splices pieces and never counts
// them.
type Application struct {
	Operation Operation
	// Detail is what this use needs said rather than bound: the separator of a
	// joined column, the format of a rendered date. Said, because no dialect
	// binds a keyword, and a dialect writes it in its own quoting.
	Detail    string
	Arguments [][]Part
}

// Syntax is how one dialect writes one use of an operation.
type Syntax func(spelling Spelling, application Application) []Part

// Operations is what a dialect can do to a value.
//
// One method, and absence is the boolean: a dialect says how it writes an
// operation, or says nothing and the statement carries the refusal. That is
// the whole port, so a dialect written elsewhere is a switch and a quoting
// rule.
type Operations interface {
	Syntax(operation Operation) (Syntax, bool)
}

// Also is a dialect that writes these operations too, and answers as it did
// for everything else.
//
// The seam for an affordance this module does not name and a dialect it did
// not write: a program that needs one server's own function declares the
// operation and wraps the dialect it already has. The entries win over the
// dialect's own, so an answer can be corrected as well as added.
func Also(spelling Spelling, syntax map[Operation]Syntax) Spelling {
	return extension{Spelling: spelling, syntax: syntax}
}

type extension struct {
	Spelling
	syntax map[Operation]Syntax
}

func (dialect extension) Syntax(operation Operation) (Syntax, bool) {
	if answer, known := dialect.syntax[operation]; known {
		return answer, true
	}
	return dialect.Spelling.Syntax(operation)
}

// renderApplication is one use of an operation as the dialect writes it, or the refusal
// that nothing knows how to write it.
func renderApplication(spelling Spelling, application Application) []Part {
	if syntax, known := spelling.Syntax(application.Operation); known {
		return syntax(spelling, application)
	}
	if application.Operation.definition != nil && application.Operation.definition.standard != nil {
		return application.Operation.definition.standard(spelling, application)
	}
	return []Part{Refusal(fmt.Errorf("sql: %s cannot %s",
		spelling.Name(), application.Operation.String()))}
}
