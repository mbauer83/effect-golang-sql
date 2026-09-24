package sql

// Order keys: where an element stands in an order a person chose.
//
// A key is text that sorts between its neighbours, so putting an element
// between two others writes one key -- one row -- whatever the collection's
// size, where positions numbered 1, 2, 3 would renumber every row after it.
// Between any two keys there is another, so they never run out; they grow a
// digit instead when an element is placed in the same gap again and again,
// and a collection whose keys grow too long has them rewritten evenly.
//
// The digits sort the same byte by byte -- 0-9, then A-Z, then a-z -- so every
// dialect orders them alike when the column compares bytes.

import (
	"errors"
	"strings"
)

const orderDigits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// longestOrderKey is the length past which a collection's keys are rewritten
// evenly: about six placements in one gap per digit, so a person dropping
// film after film at the top of one list reaches it only after a great many.
const longestOrderKey = 32

var errOrderKeys = errors.New("sql: order keys out of order: the key before has to sort before the key after")

// OrderKeyBetween is a key that sorts after before and before after. An empty
// before is the start of the order, and an empty after its end.
func OrderKeyBetween(before string, after string) (string, error) {
	if after != "" && before >= after {
		return "", errOrderKeys
	}
	if strings.HasSuffix(before, "0") || strings.HasSuffix(after, "0") {
		return "", errOrderKeys
	}
	switch {
	case after == "" && before != "":
		return next(before), nil
	case before == "" && after != "":
		return previous(after), nil
	}
	return midpoint(before, after), nil
}

// next is a key just past key, for an element put at the end: the last digit
// one higher where there is room, and a digit more where there is not. Stepping
// rather than halving the rest of the range is what keeps appending cheap -- a
// digit more every sixty-odd appends rather than every five.
func next(key string) string {
	last := strings.IndexByte(orderDigits, key[len(key)-1])
	if last < len(orderDigits)-1 {
		return key[:len(key)-1] + string(orderDigits[last+1])
	}
	return key + midpoint("", "")
}

// previous is a key just before key, for an element put at the start: the last
// digit one lower where that does not leave the lowest digit at the end.
func previous(key string) string {
	last := strings.IndexByte(orderDigits, key[len(key)-1])
	if last > 1 {
		return key[:len(key)-1] + string(orderDigits[last-1])
	}
	return midpoint("", key)
}

// midpoint is a key strictly between low and high, where an empty high is
// past every key. Neither ends in the lowest digit, so there is always room
// below a key as well as above it.
func midpoint(low string, high string) string {
	if high != "" {
		// Past the digits the two share, the key is the shared digits and a
		// midpoint of what differs.
		shared := 0
		for shared < len(high) && digitAt(low, shared) == high[shared] {
			shared++
		}
		if shared > 0 {
			return high[:shared] + midpoint(tail(low, shared), high[shared:])
		}
	}
	lowDigit := 0
	if low != "" {
		lowDigit = strings.IndexByte(orderDigits, low[0])
	}
	highDigit := len(orderDigits)
	if high != "" {
		highDigit = strings.IndexByte(orderDigits, high[0])
	}
	if highDigit-lowDigit > 1 {
		return string(orderDigits[(lowDigit+highDigit+1)/2])
	}
	// Adjacent first digits: one past the low one, under the high one, or the
	// low one with a digit more.
	if high != "" && len(high) > 1 {
		return high[:1]
	}
	return string(orderDigits[lowDigit]) + midpoint(tail(low, 1), "")
}

func digitAt(key string, at int) byte {
	if at < len(key) {
		return key[at]
	}
	return orderDigits[0]
}

func tail(key string, from int) string {
	if from >= len(key) {
		return ""
	}
	return key[from:]
}

// evenOrderKeys are count keys spread evenly: what a collection's keys are
// rewritten to when they have grown too long.
//
// All of one width -- the fewest digits with room for twice as many keys, so
// each has a gap beside it -- and spaced evenly across it: two digits hold
// nearly two thousand elements. A key that would end in the lowest digit gets
// a digit more, which still sorts before the key after it.
func evenOrderKeys(count int) []string {
	base := len(orderDigits)
	width, room := 1, base
	for room <= 2*(count+1) {
		width, room = width+1, room*base
	}
	step := room / (count + 1)
	keys := make([]string, 0, count)
	for index := 1; index <= count; index++ {
		digits := make([]byte, width)
		value := index * step
		for at := width - 1; at >= 0; at-- {
			digits[at] = orderDigits[value%base]
			value /= base
		}
		key := string(digits)
		if strings.HasSuffix(key, string(orderDigits[0])) {
			key += string(orderDigits[base/2])
		}
		keys = append(keys, key)
	}
	return keys
}
