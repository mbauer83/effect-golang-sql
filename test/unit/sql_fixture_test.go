package unit

// The values these cases bind, spelled once.

import (
	"strings"
	"time"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
)

func text(said string) dynamic.Value { return dynamic.OfText(said) }

func values(said ...string) []dynamic.Value {
	held := make([]dynamic.Value, 0, len(said))
	for _, one := range said {
		held = append(held, text(one))
	}
	return held
}

// timeStub is the type a described moment is read as.
type timeStub = time.Time

func contains(held string, wanted string) bool { return strings.Contains(held, wanted) }
