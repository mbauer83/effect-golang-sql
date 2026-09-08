package unit

// The one place this module holds a value it cannot name, over its whole
// surface.
//
// Everything above the boundary works in the universal representation, which
// has a case for each of the seven kinds a driver may produce. That claim is
// worth only as much as the coverage behind it, so it is checked against a
// driver that produces all seven -- and against one that produces something
// else, because what the boundary does then is the reason it is allowed to hold
// an unnamed value at all.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"database/sql/driver"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// querying is the channel shape the sql package works in.
type querying[A any] = effect.Effect[effect.Unit, sql.Fault, A]

// everyKind is one row of each kind a driver may hand back.
type everyKind struct {
	Text    string
	Integer int64
	Number  float64
	Boolean bool
	Bytes   []byte
	Moment  time.Time
	Nothing *string
}

var everyKindSchema = schema.Struct[everyKind]("EveryKind",
	schema.FieldOf("text", schema.Text(),
		func(row everyKind) string { return row.Text },
		func(row *everyKind, held string) { row.Text = held }),
	schema.FieldOf("integer", schema.Int64(),
		func(row everyKind) int64 { return row.Integer },
		func(row *everyKind, held int64) { row.Integer = held }),
	schema.FieldOf("number", schema.Float64(),
		func(row everyKind) float64 { return row.Number },
		func(row *everyKind, held float64) { row.Number = held }),
	schema.FieldOf("boolean", schema.Bool(),
		func(row everyKind) bool { return row.Boolean },
		func(row *everyKind, held bool) { row.Boolean = held }),
	schema.FieldOf("bytes", schema.Bytes(),
		func(row everyKind) []byte { return row.Bytes },
		func(row *everyKind, held []byte) { row.Bytes = held }),
	schema.FieldOf("moment", schema.Time(),
		func(row everyKind) time.Time { return row.Moment },
		func(row *everyKind, held time.Time) { row.Moment = held }),
	schema.FieldOf("nothing", schema.Nullable(schema.Text()),
		func(row everyKind) *string { return row.Nothing },
		func(row *everyKind, held *string) { row.Nothing = held }),
)

// asked opens a connection to the named driver and runs the work. No table is
// made: this driver holds nothing and answers two statements.
func askedOf[A any](
	t *testing.T,
	driver string,
	source string,
	work func(*sql.Connected) querying[A],
) effect.Exit[sql.Fault, A] {
	t.Helper()
	runtime, err := effect.NewRuntime(effect.WithDebugTracking())
	if err != nil {
		t.Fatal(err)
	}

	program := effect.Scoped(func(scope effect.Scope) querying[A] {
		return sql.Open[effect.Unit](scope, driver, source).FlatMap(work)
	})

	within, giveUp := context.WithTimeout(context.Background(), 10*time.Second)
	defer giveUp()

	exit := runtime.Run(within, effect.Unit{}, program)
	if live := runtime.LiveWork(); !live.IsEmpty() {
		t.Fatalf("the program left work behind: %#v", live)
	}
	return exit
}

func TestEveryKindADriverMayProduceCrossesTheBoundaryAsItself(t *testing.T) {
	read := producedBy(t, askedOf(t, "contract", "", func(database *sql.Connected) querying[everyKind] {
		return sql.QueryRow[effect.Unit](database, everyKindSchema, eachKind)
	}))

	if read.Text != "held" || read.Integer != 7 || read.Number != 1.5 || !read.Boolean {
		t.Errorf("unexpected scalars: %#v", read)
	}
	if len(read.Bytes) != 2 || read.Bytes[0] != 1 || read.Bytes[1] != 2 {
		t.Errorf("unexpected bytes: %#v", read.Bytes)
	}
	if !read.Moment.Equal(contractMoment) {
		t.Errorf("unexpected instant: %v", read.Moment)
	}
	// Null is the explicit absence a column carries, which is distinct from a
	// member that is not there at all.
	if read.Nothing != nil {
		t.Errorf("expected the null column absent, got %q", *read.Nothing)
	}
}

func TestADriverThatGoesBeyondTheContractIsToldSoRatherThanGuessedAt(t *testing.T) {
	exit := askedOf(t, "contract", "", func(database *sql.Connected) querying[everyKind] {
		return sql.QueryRow[effect.Unit](database, everyKindSchema, beyondTheContract)
	})

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the value to be refused, got %+v", exit)
	}
	failures := cause.Failures()
	if len(failures) != 1 || failures[0].Doing != "reading a row" {
		t.Fatalf("expected the stage named, got %+v", cause)
	}
	// The type is in the message, because "a driver produced something odd" is
	// not a report anybody can act on.
	if !strings.Contains(failures[0].Error(), "int32") {
		t.Fatalf("expected the type named, got %v", failures[0])
	}
}

func producedBy[A any](t *testing.T, exit effect.Exit[sql.Fault, A]) A {
	t.Helper()
	value, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	return value
}

func TestEveryKindAStatementMayBindCrossesTheBoundaryAsItself(t *testing.T) {
	// The same boundary in the other direction. A statement's parameters are
	// values of whatever kind the columns are, and the driver's contract is
	// untyped -- so what is checked is that each of the seven arrives as the
	// type the contract names for it, in the position it was given.
	moment := contractMoment
	producedBy(t, askedOf(t, "contract", "", func(database *sql.Connected) querying[sql.Outcome] {
		return sql.Execute[effect.Unit](database, takingEveryKind,
			dynamic.OfText("held"),
			dynamic.OfInteger(7),
			dynamic.OfNumber(1.5),
			dynamic.OfBoolean(true),
			dynamic.OfBytes([]byte{1, 2}),
			dynamic.OfTimestamp(moment),
			dynamic.Absent{})
	}))

	wanted := []driver.Value{
		"held", int64(7), 1.5, true, []byte{1, 2}, moment, nil,
	}
	if !reflect.DeepEqual(lastBound, wanted) {
		t.Fatalf("expected %#v, got %#v", wanted, lastBound)
	}
}

func TestAValueNoStatementMayBindIsRefusedBeforeItReachesTheDriver(t *testing.T) {
	// A list is a value the representation has and a column does not, so
	// binding one is a mistake worth naming rather than flattening.
	exit := askedOf(t, "contract", "", func(database *sql.Connected) querying[sql.Outcome] {
		return sql.Execute[effect.Unit](database, takingEveryKind,
			dynamic.List{Elements: []dynamic.Value{dynamic.OfInteger(1)}})
	})

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the argument to be refused, got %+v", exit)
	}
	failures := cause.Failures()
	// Which argument, because a statement with twelve of them needs that.
	if len(failures) != 1 || !strings.Contains(failures[0].Error(), "argument 1") {
		t.Fatalf("expected the position named, got %+v", cause)
	}
}

func TestADriversOwnErrorIsStillReachableThroughTheFault(t *testing.T) {
	// The reason a Fault unwraps: a caller that needs to know whether a
	// constraint was violated asks the driver's error, and it would not be
	// there to ask if the boundary had replaced it with a message.
	exit := askedOf(t, "contract", "", func(database *sql.Connected) querying[sql.Outcome] {
		return sql.Execute[effect.Unit](database, "a statement this driver has never heard of")
	})

	cause, failed := exit.Cause()
	if !failed {
		t.Fatalf("expected the statement to be refused, got %+v", exit)
	}
	failures := cause.Failures()
	if len(failures) != 1 {
		t.Fatalf("expected one failure, got %+v", cause)
	}
	if !errors.Is(failures[0], errUnknownStatement) {
		t.Fatalf("expected the driver's own error still reachable, got %v", failures[0])
	}
	// And the statement beside it, because a database error without the text
	// that caused it is nearly useless.
	if failures[0].Statement == "" {
		t.Fatal("expected the statement carried")
	}
}
