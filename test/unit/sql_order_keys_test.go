package unit

// Order keys. What matters is that a key between two always sorts between
// them, however many times the same gap is filled, and that keys stay short.

import (
	"testing"

	"github.com/mbauer83/effect-golang-sql/sql"
)

func TestAKeyBetweenTwoSortsBetweenThemEveryTime(t *testing.T) {
	// The same gap, again and again, at the top of the order and in the middle.
	for _, gap := range [][2]string{{"", ""}, {"", "V"}, {"V", ""}, {"V", "W"}, {"a", "a1"}} {
		low, high := gap[0], gap[1]
		for range 300 {
			key, err := sql.OrderKeyBetween(low, high)
			if err != nil {
				t.Fatalf("between %q and %q: %v", low, high, err)
			}
			if (low != "" && key <= low) || (high != "" && key >= high) {
				t.Fatalf("expected a key between %q and %q, got %q", low, high, key)
			}
			high = key
		}
	}
}

func TestKeysOutOfOrderAreRefused(t *testing.T) {
	if _, err := sql.OrderKeyBetween("b", "a"); err == nil {
		t.Fatal("expected keys out of order refused")
	}
}
