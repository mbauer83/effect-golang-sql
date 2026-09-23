package warehouse

// The rows half of the split: what happens to the pallets a database already
// holds.

import (
	"context"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/sql"
)

// splitRows moves the rows a database already holds.
//
// A Go function and not a list of statements, and this is what that buys.
// Postgres spells "the part before the dash" as split_part, MySQL as
// substring_index and SQLite as substr with instr -- so as statements this
// would have been three declarations of one change, each of which had to be got
// right separately. Read, split in Go, write back, and there is one.
//
// It also means the split can be anything: a lookup against another table, a
// call to a service that knows the depot codes, a checksum. None of that is
// expressible in a statement list, and a migration that needed it would have
// had nowhere to put it.
func splitRows(ctx context.Context, within sql.Querier, spelling sql.Spelling) error {
	rows, err := everyReference(ctx, within)
	if err != nil {
		return err
	}
	for _, row := range rows {
		prefix, serial, found := strings.Cut(row.reference, "-")
		if !found {
			// A reference with no dash is all serial and no prefix, which is
			// what the depots that never used one wrote.
			prefix, serial = "", row.reference
		}
		// Composed through the dialect the migration is running on, so the
		// same mover works on every server. Written as text it would have to
		// choose one.
		statement := sql.Compose(spelling,
			sql.Text(`update `+spelling.QuoteIdentifier("Pallet")+
				` set `+spelling.QuoteIdentifier("prefix")+` = `),
			sql.Bind(dynamic.OfText(prefix)),
			sql.Text(`, `+spelling.QuoteIdentifier("serial")+` = `),
			sql.Bind(dynamic.OfText(serial)),
			sql.Text(` where `+spelling.QuoteIdentifier("id")+` = `),
			sql.Bind(dynamic.OfInteger(row.id)))
		if _, err := within.Execute(ctx, statement.Text(), statement.Values()); err != nil {
			return err
		}
	}
	return nil
}

// joinRows puts them back together.
func joinRows(ctx context.Context, within sql.Querier, spelling sql.Spelling) error {
	rows, err := everySplit(ctx, within)
	if err != nil {
		return err
	}
	for _, row := range rows {
		reference := row.serial
		if row.prefix != "" {
			reference = row.prefix + "-" + row.serial
		}
		statement := sql.Compose(spelling,
			sql.Text(`update `+spelling.QuoteIdentifier("Pallet")+
				` set `+spelling.QuoteIdentifier("reference")+` = `),
			sql.Bind(dynamic.OfText(reference)),
			sql.Text(` where `+spelling.QuoteIdentifier("id")+` = `),
			sql.Bind(dynamic.OfInteger(row.id)))
		if _, err := within.Execute(ctx, statement.Text(), statement.Values()); err != nil {
			return err
		}
	}
	return nil
}

// referenceRow is one row's identity and the reference it holds.
type referenceRow struct {
	id        int64
	reference string
	prefix    string
	serial    string
}

func everyReference(ctx context.Context, within sql.Querier) ([]referenceRow, error) {
	return readRows(ctx, within, `select "id", "reference" from "Pallet"`,
		func(row dynamic.Object) referenceRow {
			return referenceRow{id: whole(row, "id"), reference: text(row, "reference")}
		})
}

func everySplit(ctx context.Context, within sql.Querier) ([]referenceRow, error) {
	return readRows(ctx, within, `select "id", "prefix", "serial" from "Pallet"`,
		func(row dynamic.Object) referenceRow {
			return referenceRow{
				id:     whole(row, "id"),
				prefix: text(row, "prefix"),
				serial: text(row, "serial"),
			}
		})
}

// readRows reads every row of a statement, closing the cursor whatever happens.
func readRows(
	ctx context.Context,
	within sql.Querier,
	statement string,
	decode func(dynamic.Object) referenceRow,
) ([]referenceRow, error) {
	cursor, err := within.Query(ctx, statement, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close() }()

	rows := []referenceRow{}
	for cursor.Next() {
		row, err := cursor.Row()
		if err != nil {
			return nil, err
		}
		rows = append(rows, decode(row))
	}
	return rows, cursor.Err()
}

func whole(row dynamic.Object, name string) int64 {
	member, present := row.Member(name)
	if !present {
		return 0
	}
	number, isNumber := member.(dynamic.Integer)
	if !isNumber {
		return 0
	}
	return number.Value
}

func text(row dynamic.Object, name string) string {
	member, present := row.Member(name)
	if !present {
		return ""
	}
	return textOf(member)
}
