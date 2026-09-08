package unit

// What the DDL projection refuses, and why each refusal beats the alternative.
//
// Every one of these is a table that could have been emitted, and would have
// meant something the description did not say.

import (
	"strings"
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-sql/ddl"
)

func TestTheProjectionRefusesWhatTheDescriptionDoesNotSay(t *testing.T) {
	for named, expected := range map[string]struct {
		shape  schema.Schema[dynamic.Value]
		reason string
	}{
		"no identity": {
			shape: schema.Struct[dynamic.Value]("Anonymous",
				schema.DescribedField("name", schema.Text())),
			reason: "names no identity",
		},
		"a computed column with nothing to compute it": {
			shape: schema.Struct[dynamic.Value]("Stamped",
				schema.DescribedField("id", schema.Int64()).Identity().Computed(),
				schema.DescribedField("at", schema.Time()).Computed()),
			reason: "not what computes it",
		},
		"an identity with parts": {
			shape: schema.Struct[dynamic.Value]("Composite",
				schema.DescribedField("key", schema.Struct[dynamic.Value]("Key",
					schema.DescribedField("a", schema.Text()))).Identity()),
			reason: "an identity is one value",
		},
		"an identity that may be absent": {
			shape: schema.Struct[dynamic.Value]("Maybe",
				schema.DescribedField("id", schema.Int64()).Identity().Optional()),
			reason: "identifies nothing",
		},
	} {
		_, err := ddl.Tables(ddl.Postgres, expected.shape.Structure())
		if err == nil {
			t.Errorf("expected %s to be refused", named)
			continue
		}
		if !strings.Contains(err.Error(), expected.reason) {
			t.Errorf("%s: expected the reason to mention %q, got %v",
				named, expected.reason, err)
		}
	}
}

// entityShape is a minimal entity, for the cases that need one. Its identity
// is bounded, because MySQL cannot key an unbounded string.
var entityShape = schema.Struct[dynamic.Value]("Line",
	schema.DescribedField("id", schema.MaxLength(schema.UUID(), 36)).Identity(),
	schema.DescribedField("what", schema.Text()),
)
