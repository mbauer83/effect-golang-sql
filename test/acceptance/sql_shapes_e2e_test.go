package acceptance

// What a store built only out of the statement shapes does, as one sequence.
//
// A case each would need the table the one before it left, and what is under
// test is exactly that: a pallet written, three lines written on it, one of
// them replaced, a page of them read from a cursor, and two of them removed by
// a set -- every one of them said as a shape and spelled by the server.

import (
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// exercised writes a pallet and three lines on it with the shapes, replaces
// one, pages the lines with a cursor, and removes two of them by a set.
//
// One program rather than a case each, because every step needs the table the
// one before it left -- and because what is under test is that a store built
// only out of these shapes works, which is a claim about the sequence.
func exercised(dialect ddl.Dialect, database *sql.Connected) building[[]lined] {
	pallet := sql.Writing{
		Table:   "Pallet",
		Columns: []string{"reference", "warehouse"},
		Values:  []dynamic.Value{dynamic.OfText("P-1"), dynamic.OfText("Kiel")},
	}
	found := sql.Reading{
		Select: sql.Selected("id"),
		From:   sql.From("Pallet"),
		Where:  sql.Equals("reference", "P-1"),
	}
	return sql.Run[effect.Unit](database, pallet.Statement(dialect)).
		FlatMap(func(sql.Outcome) building[keyed] {
			return sql.Row[effect.Unit](database, keyedSchema, found.Statement(dialect))
		}).
		FlatMap(func(held keyed) building[[]lined] {
			return linesOn(dialect, database, held.ID)
		})
}

// linesOn writes the lines, replaces one and reads a page of them.
func linesOn(dialect ddl.Dialect, database *sql.Connected, pallet int64) building[[]lined] {
	// Two lines share a position with nothing, but the cursor below orders by
	// the pallet and then the position -- so the comparison has to fix the
	// pallet and compare the position, which is the part of a keyset that is
	// wrong more often than it is right.
	written := effect.ForEach([]lined{
		{SKU: "BOLT-8", Quantity: 40, Position: 0},
		{SKU: "NUT-8", Quantity: 80, Position: 1},
		{SKU: "WASHER-8", Quantity: 120, Position: 2},
	}, func(line lined) building[sql.Outcome] {
		return sql.Run[effect.Unit](database, writingLine(pallet, line).Statement(dialect))
	})
	return written.FlatMap(func([]sql.Outcome) building[[]lined] {
		// The same line of the same pallet again, with a different quantity:
		// one statement, and afterwards there is one row holding the second
		// quantity. A second insert would have been refused and a
		// delete-then-insert would have shown as two rows if either had been
		// what this generated.
		//
		// The key is the pallet and the line together, because that is what a
		// child table's key is: a line's identity distinguishes it among its
		// pallet's lines and not among every pallet's.
		replaced := sql.Replacement{
			Table:   "PalletItem",
			Columns: []string{"id", "sku", "quantity", "Pallet_id", "position"},
			Key:     []string{"Pallet_id", "id"},
			Values: []dynamic.Value{
				dynamic.OfText("line-NUT-8"),
				dynamic.OfText("NUT-8"),
				dynamic.OfInteger(85),
				dynamic.OfInteger(pallet),
				dynamic.OfInteger(1),
			},
		}
		return sql.Run[effect.Unit](database, replaced.Statement(dialect)).
			FlatMap(func(sql.Outcome) building[[]lined] {
				return paged(dialect, database, pallet)
			})
	})
}

func writingLine(pallet int64, line lined) sql.Writing {
	return sql.Writing{
		Table:   "PalletItem",
		Columns: []string{"id", "sku", "quantity", "Pallet_id", "position"},
		Values: []dynamic.Value{
			dynamic.OfText("line-" + line.SKU),
			dynamic.OfText(line.SKU),
			dynamic.OfInteger(int64(line.Quantity)),
			dynamic.OfInteger(pallet),
			dynamic.OfInteger(int64(line.Position)),
		},
	}
}

// paged reads the lines after the last one, in the order the cursor is stated
// in, and then removes two of them by a set.
func paged(dialect ddl.Dialect, database *sql.Connected, pallet int64) building[[]lined] {
	page := sql.Reading{
		Select: sql.Selected("sku", "quantity", "position"),
		From:   sql.From("PalletItem"),
		Ordered: []sql.Ordering{
			sql.Column[int64]("Pallet_id").Descending(),
			sql.Column[int64]("position").Descending(),
		},
		After: []dynamic.Value{sql.At(pallet), sql.At(int64(2))},
		Rows:  2,
	}
	removal := sql.Removal{
		Table: "PalletItem",
		Where: sql.AmongValues(sql.Column[string]("id"), "line-BOLT-8", "line-WASHER-8"),
	}
	return effect.RunCollect(sql.Rows[effect.Unit](database, linedSchema, page.Statement(dialect))).
		FlatMap(func(read []lined) building[[]lined] {
			return sql.Run[effect.Unit](database, removal.Statement(dialect)).As(read)
		})
}

// remaining is what the shapes established, checked once for every server.
func remaining(t *testing.T, exit effect.Exit[sql.Fault, []lined]) {
	t.Helper()
	read, ok := exit.Value()
	if !ok {
		t.Fatalf("unexpected exit: %+v", exit)
	}
	// Two rows, because the cursor stood at position 2 and asked for what
	// follows it descending -- so positions 1 and 0, in that order, and not
	// position 2 itself.
	if len(read) != 2 {
		t.Fatalf("expected the two lines after the cursor, got %d: %+v", len(read), read)
	}
	if read[0].Position != 1 || read[1].Position != 0 {
		t.Fatalf("expected positions 1 then 0, got %+v", read)
	}
	// And the replacement changed the row it matched rather than adding one:
	// the quantity is the second one, on the identity the first insert used.
	if read[0].SKU != "NUT-8" || read[0].Quantity != 85 {
		t.Fatalf("expected the replaced quantity on the same line, got %+v", read[0])
	}
}
