package unit

// Integer widths. What matters is that a column is as narrow as the range the
// domain states, never wider than the width it declared, and that a range
// only an unsigned sixty-four-bit integer holds is still refused where a
// dialect has no such type.

import (
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
)

func columnTyped(t *testing.T, dialect ddl.Dialect, field schema.Field[dynamic.Value, dynamic.Value]) ddl.Column {
	t.Helper()
	shape := schema.Struct[dynamic.Value]("held",
		schema.DynamicField("id", schema.Int64()).Identity(), field)
	tables, err := ddl.Tables(dialect, shape.Structure())
	if err != nil {
		t.Fatal(err)
	}
	column, _ := columnIn(tables[0], "value")
	return column
}

func TestAnIntegerColumnIsAsNarrowAsTheRangeTheDomainStates(t *testing.T) {
	year := schema.DynamicField("value", schema.Int64().Check(schema.AtLeast[int64](0), schema.AtMost[int64](9999)))
	if column := columnTyped(t, ddl.Postgres, year); column.Type != "SMALLINT" {
		t.Fatalf("expected a year to be a small integer, got %s", column.Type)
	}
	// The narrowed type keeps neither bound, so both are checked.
	if column := columnTyped(t, ddl.Postgres, year); len(column.Checks) != 2 {
		t.Fatalf("expected both bounds checked, got %+v", column.Checks)
	}
	percent := schema.DynamicField("value", schema.Int32().Check(schema.AtLeast[int32](0), schema.AtMost[int32](100)))
	if column := columnTyped(t, ddl.MySQL, percent); column.Type != "TINYINT" {
		t.Fatalf("expected MySQL's smallest integer, got %s", column.Type)
	}
}

func TestAColumnWithoutAStatedRangeKeepsItsDeclaredWidth(t *testing.T) {
	count := schema.DynamicField("value", schema.Int32().Check(schema.AtLeast[int32](0)))
	if column := columnTyped(t, ddl.Postgres, count); column.Type != "INTEGER" {
		t.Fatalf("expected the declared width, got %s", column.Type)
	}
}

func TestABoundedUnsignedSixtyFourIsStorableWhereTheRangeFits(t *testing.T) {
	small := schema.DynamicField("value", schema.Uint64().Check(schema.AtMost[uint64](1000)))
	if column := columnTyped(t, ddl.Postgres, small); column.Type != "SMALLINT" {
		t.Fatalf("expected a bounded unsigned integer to fit a signed one, got %s", column.Type)
	}
}

func TestTheTightestBoundDecidesWhateverOrderTheyWereStatedIn(t *testing.T) {
	stated := schema.DynamicField("value", schema.Int64().Check(
		schema.AtMost[int64](100), schema.AtMost[int64](1<<40), schema.AtLeast[int64](0)))
	if column := columnTyped(t, ddl.Postgres, stated); column.Type != "SMALLINT" {
		t.Fatalf("expected the tighter bound to decide, got %s", column.Type)
	}
}
