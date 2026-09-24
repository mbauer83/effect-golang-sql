package sql

// Where each element of a list stands, so that moving one writes one row.
//
// A list's position column holds integers spaced apart. Saving a list keeps
// the stored position of every element whose order relative to the others
// held -- the longest run of them in order -- and gives each other element a
// position between its neighbours'. Only when two neighbours have no room
// between them is the list numbered afresh.

import (
	"math"
	"sort"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

// positionGap is how far apart a list's positions are when it is numbered:
// room for ten elements put between any two before a list is numbered again.
const positionGap = 1024

// placeRows gives an ordered table's desired rows their positions: the stored
// one where an element's order held, one between its neighbours' otherwise.
func placeRows(table TableLayout, stored []dynamic.Object, desired []dynamic.Object) []dynamic.Object {
	held := map[string]int64{}
	for _, row := range stored {
		if position, known := storedPosition(table, row); known {
			held[keyOf(table, row)] = position
		}
	}
	placed := make([]dynamic.Object, 0, len(desired))
	for _, group := range groupedByHolder(table, desired) {
		positions := make([]int64, len(group))
		known := make([]bool, len(group))
		for at, row := range group {
			positions[at], known[at] = held[keyOf(table, row)]
		}
		for at, position := range placeList(positions, known) {
			placed = append(placed, withMember(group[at], table.Position, dynamic.OfInteger(position)))
		}
	}
	return placed
}

// placeList is the positions of a list whose stored positions are known[at]
// positions[at]: the longest increasing run of them kept, the rest between.
func placeList(positions []int64, known []bool) []int64 {
	kept := longestIncreasing(positions, known)
	placed := make([]int64, len(positions))
	for at := 0; at < len(positions); {
		if kept[at] {
			placed[at] = positions[at]
			at++
			continue
		}
		end := at
		for end < len(positions) && !kept[end] {
			end++
		}
		run := int64(end - at)
		for step := range end - at {
			index := int64(step)
			switch {
			case at == 0 && end == len(positions):
				placed[at+step] = index * positionGap
			case at == 0:
				placed[at+step] = positions[end] - (run-index)*positionGap
			case end == len(positions):
				placed[at+step] = placed[at-1] + (index+1)*positionGap
			default:
				low, high := placed[at-1], positions[end]
				if high-low <= run {
					return numbered(len(positions))
				}
				placed[at+step] = low + (high-low)*(index+1)/(run+1)
			}
		}
		at = end
	}
	for _, position := range placed {
		if position < math.MinInt32 || position > math.MaxInt32 {
			return numbered(len(positions))
		}
	}
	return placed
}

func numbered(count int) []int64 {
	placed := make([]int64, count)
	for at := range placed {
		placed[at] = int64(at) * positionGap
	}
	return placed
}

// longestIncreasing marks the longest run of known positions that increase
// in the list's order: the elements that need not move.
func longestIncreasing(positions []int64, known []bool) []bool {
	var tails []int // indices ending the best run of each length
	previous := make([]int, len(positions))
	for at := range positions {
		if !known[at] {
			continue
		}
		length := sort.Search(len(tails), func(candidate int) bool {
			return positions[tails[candidate]] >= positions[at]
		})
		previous[at] = -1
		if length > 0 {
			previous[at] = tails[length-1]
		}
		if length == len(tails) {
			tails = append(tails, at)
		} else {
			tails[length] = at
		}
	}
	kept := make([]bool, len(positions))
	if len(tails) > 0 {
		for at := tails[len(tails)-1]; at >= 0; at = previous[at] {
			kept[at] = true
		}
	}
	return kept
}

// groupedByHolder is an ordered table's rows grouped by the holder whose list
// they are, each group in the list's order.
func groupedByHolder(table TableLayout, rows []dynamic.Object) [][]dynamic.Object {
	var order []string
	groups := map[string][]dynamic.Object{}
	for _, row := range rows {
		key := tupleOf(row, table.Parent.Columns)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], row)
	}
	grouped := make([][]dynamic.Object, 0, len(order))
	for _, key := range order {
		grouped = append(grouped, groups[key])
	}
	return grouped
}

func storedPosition(table TableLayout, row dynamic.Object) (int64, bool) {
	value, _ := row.Member(table.Position)
	switch held := value.(type) {
	case dynamic.Integer:
		return held.Value, true
	case dynamic.Number:
		return int64(held.Value), true
	default:
		return 0, false
	}
}

// withMember is the row with one member's value replaced.
func withMember(row dynamic.Object, name string, value dynamic.Value) dynamic.Object {
	fields := make([]dynamic.Field, len(row.Fields))
	copy(fields, row.Fields)
	for at := range fields {
		if fields[at].Name == name {
			fields[at].Value = value
		}
	}
	return dynamic.Object{Fields: fields}
}
