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
// Declare; teaching a dialect one is Also.

var (
	// The six comparisons. Every server has all six, so all six are ordinary.
	EqualTo     = Declare("compare for equality").WithDefault(Infix(" = "))
	UnequalTo   = Declare("compare for difference").WithDefault(Infix(" <> "))
	LessThan    = Declare("compare for less").WithDefault(Infix(" < "))
	NoMoreThan  = Declare("compare for no more").WithDefault(Infix(" <= "))
	GreaterThan = Declare("compare for more").WithDefault(Infix(" > "))
	NoLessThan  = Declare("compare for no less").WithDefault(Infix(" >= "))

	// Membership and the two questions about absence.
	OneOf     = Declare("test for membership").WithDefault(ListOperator(" IN ", "(", ", ", ")"))
	SomeValue = Declare("test for a value").WithDefault(Phrase("", " IS NOT NULL"))
	NoValue   = Declare("test for no value").WithDefault(Phrase("", " IS NULL"))

	// Patterns. A wildcard pattern is universal; a regular expression is not,
	// and a dialect whose server has none says nothing rather than composing
	// something that fails at the first request.
	PatternMatch    = Declare("match a wildcard pattern").WithDefault(Infix(" LIKE "))
	ExpressionMatch = Declare("match a regular expression")

	// Text. Concatenation and a substring are spelled three ways; the rest are
	// the same everywhere except that MySQL counts characters under another
	// name, which it says itself.
	Concatenation  = Declare("concatenate")
	SubstringOf    = Declare("take a substring")
	LowerCase      = Declare("lower the case").WithDefault(Function("LOWER"))
	UpperCase      = Declare("raise the case").WithDefault(Function("UPPER"))
	WhitespaceTrim = Declare("trim the ends").WithDefault(Function("TRIM"))
	CharacterCount = Declare("count characters").WithDefault(Function("LENGTH"))

	// Groups. Counting rows and counting a column's values are two questions:
	// a count of a column does not count the rows where it is null.
	RowCount   = Declare("count rows").WithDefault(Phrase("COUNT(*)"))
	ValueCount = Declare("count values").WithDefault(Function("COUNT"))
	Maximum    = Declare("take the greatest").WithDefault(Function("MAX"))
	Minimum    = Declare("take the least").WithDefault(Function("MIN"))
	Summation  = Declare("total").WithDefault(Function("SUM"))
	// WholeTotal and Average carry no ordinary spelling, and the reason is
	// the one thing a type cannot check: two of the three servers answer an
	// aggregate with a *wider* type than the values it was over. A sum of
	// whole numbers is a decimal on MySQL and, past 32 bits, a numeric on
	// Postgres; an average is a decimal on both. Their drivers hand those
	// back as text, so a query that claimed the summand's type would decode
	// nothing -- which is a failure at the first request against a real
	// server and never against SQLite.
	//
	// So each dialect says how to bring the answer back to the type the query
	// claims, and the claim becomes true everywhere.
	WholeTotal        = Declare("total whole numbers")
	Average           = Declare("average")
	StringAggregation = Declare("join a group's values")

	// Arithmetic, bracketed, because an operator inside another one is what
	// precedence decides.
	Addition       = Declare("add").WithDefault(Operator(" + "))
	Subtraction    = Declare("subtract").WithDefault(Operator(" - "))
	Multiplication = Declare("multiply").WithDefault(Operator(" * "))
	Division       = Declare("divide").WithDefault(Operator(" / "))

	// The first argument that has a value, which is how a nullable column
	// becomes a number a caller can order by.
	Coalescence = Declare("take the first with a value").WithDefault(Function("COALESCE"))

	// Time. Asked for in seconds and only in seconds: a difference in days is
	// a whole number on one server and a fraction on another, so a caller that
	// wants days divides and knows which it got.
	SecondsBetween = Declare("take a difference in seconds")

	// A window. Ordinary because every server these dialects are for has had
	// them for years -- Postgres always, MySQL since 8.0, SQLite since 3.25 --
	// and a dialect for an older one says so by answering nothing.
	OverWindow = Declare("read over a window").WithDefault(Infix(" OVER "))
)
