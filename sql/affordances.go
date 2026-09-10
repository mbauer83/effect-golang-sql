package sql

// The operations this module names, and the spelling each is taken to have.
//
// Two kinds live here. Most are ordinary -- lower(x) is lower(x) on every
// server anybody has shipped -- and carry the spelling, so a dialect answers
// nothing and gets it. A few are not: two of the three concatenate with an
// operator and the third with a function, one measures a string in characters
// under another name, one has no regular expression at all. Those carry no
// spelling, so a dialect that does not answer about them refuses, which is the
// honest outcome for an operation its server does not have.
//
// They are values, so a dialect answers about them with a switch and a program
// asks for one by holding it. Adding an operation nobody here named is
// Declaring; teaching a dialect one is Also.

var (
	// The six comparisons. Every server has all six, so all six are ordinary.
	EqualTo     = Declaring("compare for equality").Ordinarily(Relating(" = "))
	UnequalTo   = Declaring("compare for difference").Ordinarily(Relating(" <> "))
	LessThan    = Declaring("compare for less").Ordinarily(Relating(" < "))
	NoMoreThan  = Declaring("compare for no more").Ordinarily(Relating(" <= "))
	GreaterThan = Declaring("compare for more").Ordinarily(Relating(" > "))
	NoLessThan  = Declaring("compare for no less").Ordinarily(Relating(" >= "))

	// Membership and the two questions about absence.
	OneOf     = Declaring("test for membership").Ordinarily(Leading(" in ", "(", ", ", ")"))
	SomeValue = Declaring("test for a value").Ordinarily(Phrased("", " is not null"))
	NoValue   = Declaring("test for no value").Ordinarily(Phrased("", " is null"))

	// Patterns. A wildcard pattern is universal; a regular expression is not,
	// and a dialect whose server has none says nothing rather than composing
	// something that fails at the first request.
	PatternMatch    = Declaring("match a wildcard pattern").Ordinarily(Relating(" like "))
	ExpressionMatch = Declaring("match a regular expression")

	// Text. Concatenation and a substring are spelled three ways; the rest are
	// the same everywhere except that MySQL counts characters under another
	// name, which it says itself.
	Concatenation  = Declaring("concatenate")
	SubstringOf    = Declaring("take a substring")
	LowerCase      = Declaring("lower the case").Ordinarily(Calling("lower"))
	UpperCase      = Declaring("raise the case").Ordinarily(Calling("upper"))
	Trimming       = Declaring("trim the ends").Ordinarily(Calling("trim"))
	CharacterCount = Declaring("count characters").Ordinarily(Calling("length"))

	// Groups. Counting rows and counting a column's values are two questions:
	// a count of a column does not count the rows where it is null.
	RowCount   = Declaring("count rows").Ordinarily(Phrased("count(*)"))
	ValueCount = Declaring("count values").Ordinarily(Calling("count"))
	Maximum    = Declaring("take the greatest").Ordinarily(Calling("max"))
	Minimum    = Declaring("take the least").Ordinarily(Calling("min"))
	Sum        = Declaring("total").Ordinarily(Calling("sum"))
	// WholeTotal and Average carry no ordinary spelling, and the reason is
	// the one thing a type cannot check: two of the three servers answer an
	// aggregate with a *wider* type than the values it was over. A sum of
	// whole numbers is a decimal on MySQL and, past 32 bits, a numeric on
	// Postgres; an average is a decimal on both. Their drivers hand those
	// back as text, so a reading that claimed the summand's type would decode
	// nothing -- which is a failure at the first request against a real
	// server and never against SQLite.
	//
	// So each dialect says how to bring the answer back to the type the query
	// claims, and the claim becomes true everywhere.
	WholeTotal   = Declaring("total whole numbers")
	Average      = Declaring("average")
	JoinedValues = Declaring("join a group's values")

	// Arithmetic, bracketed, because an operator inside another one is what
	// precedence decides.
	Addition       = Declaring("add").Ordinarily(Between(" + "))
	Subtraction    = Declaring("subtract").Ordinarily(Between(" - "))
	Multiplication = Declaring("multiply").Ordinarily(Between(" * "))
	Division       = Declaring("divide").Ordinarily(Between(" / "))

	// The first argument that has a value, which is how a nullable column
	// becomes a number a caller can order by.
	Coalescence = Declaring("take the first with a value").Ordinarily(Calling("coalesce"))

	// Time. Asked for in seconds and only in seconds: a difference in days is
	// a whole number on one server and a fraction on another, so a caller that
	// wants days divides and knows which it got.
	SecondsBetween = Declaring("take a difference in seconds")

	// A window. Ordinary because every server these dialects are for has had
	// them for years -- Postgres always, MySQL since 8.0, SQLite since 3.25 --
	// and a dialect for an older one says so by answering nothing.
	OverWindow = Declaring("read over a window").Ordinarily(Relating(" over "))
)
