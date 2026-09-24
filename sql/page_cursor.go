package sql

// Where a page stopped, as the opaque value a client hands back.

import (
	"encoding/base64"
	"errors"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// PageCursor is a position in a listing: the sort values of one row, tagged with
// the sort and the filter it was read under. Opaque on purpose -- a client
// that read one would come to depend on a listing's order -- and refused when
// it is handed back with another sort or filter, because the position it names
// means nothing there.
type PageCursor string

// IsStart reports whether the cursor names no position: the first page.
func (cursor PageCursor) IsStart() bool { return cursor == "" }

// ErrPageCursor is a cursor this listing did not write, or wrote for another sort
// or filter: a client's mistake, and one it recovers from by starting again.
var ErrPageCursor = errors.New(
	"sql: that cursor belongs to another sort or filter, or to nothing this wrote; start from the first page")

// cursorOf is a row's position, under a tag naming the sort and filter.
func cursorOf(tag string, values []dynamic.Value) (PageCursor, error) {
	parts := make([]string, 0, len(values)+1)
	parts = append(parts, tag)
	for _, value := range values {
		part, err := cursorPart(value)
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	return PageCursor(base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x1f")))), nil
}

// positionOf is the values a cursor holds, when it was written under tag.
func positionOf(cursor PageCursor, tag string) ([]dynamic.Value, error) {
	raw, err := base64.RawURLEncoding.DecodeString(string(cursor))
	if err != nil {
		return nil, ErrPageCursor
	}
	parts := strings.Split(string(raw), "\x1f")
	if parts[0] != tag {
		return nil, ErrPageCursor
	}
	values := make([]dynamic.Value, 0, len(parts)-1)
	for _, part := range parts[1:] {
		value, err := cursorValue(part)
		if err != nil {
			return nil, ErrPageCursor
		}
		values = append(values, value)
	}
	return values, nil
}

func cursorPart(value dynamic.Value) (string, error) {
	switch held := value.(type) {
	case dynamic.Integer:
		return "i" + strconv.FormatInt(held.Value, 10), nil
	case dynamic.Number:
		return "n" + strconv.FormatFloat(held.Value, 'g', -1, 64), nil
	case dynamic.Text:
		return "t" + base64.RawURLEncoding.EncodeToString([]byte(held.Value)), nil
	case dynamic.Boolean:
		return "b" + strconv.FormatBool(held.Value), nil
	case dynamic.Timestamp:
		return "m" + held.Value.UTC().Format(time.RFC3339Nano), nil
	case dynamic.Bytes:
		return "x" + base64.RawURLEncoding.EncodeToString(held.Value), nil
	default:
		return "", errors.New("sql: a page is ordered by a value a cursor cannot hold, such as a null")
	}
}

func cursorValue(part string) (dynamic.Value, error) {
	if part == "" {
		return nil, ErrPageCursor
	}
	body := part[1:]
	switch part[0] {
	case 'i':
		value, err := strconv.ParseInt(body, 10, 64)
		return dynamic.OfInteger(value), err
	case 'n':
		value, err := strconv.ParseFloat(body, 64)
		return dynamic.OfNumber(value), err
	case 't':
		value, err := base64.RawURLEncoding.DecodeString(body)
		return dynamic.OfText(string(value)), err
	case 'b':
		value, err := strconv.ParseBool(body)
		return dynamic.OfBoolean(value), err
	case 'm':
		value, err := time.Parse(time.RFC3339Nano, body)
		return dynamic.OfTimestamp(value), err
	case 'x':
		value, err := base64.RawURLEncoding.DecodeString(body)
		return dynamic.OfBytes(value), err
	default:
		return nil, ErrPageCursor
	}
}

// fingerprint is what a filter says, reduced to a few characters: the text
// and the values it binds, so a cursor made under one filter is not taken for
// a position under another.
func fingerprint(spelling Spelling, where Criterion) string {
	statement := Compose(spelling, Condition(spelling, where)...)
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(statement.Text()))
	for _, value := range statement.Values() {
		part, _ := cursorPart(value)
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(part))
	}
	return strconv.FormatUint(hash.Sum64(), 36)
}
