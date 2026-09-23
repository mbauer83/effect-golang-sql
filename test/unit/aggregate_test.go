package unit

// The aggregate these tests project into tables.
//
// Deliberately awkward: an identity the database generates, a value the
// database computes, a value object with no identity of its own, and a
// collection of entities that do have one. Every rule the projection has shows
// up in one of those.

import (
	"testing"

	"github.com/mbauer83/effect-golang-schema/schema"
	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// address is a value object: it belongs to whatever holds it and has no
// identity, so it lives in the order's own row.
var address = schema.Struct[dynamic.Value]("Address",
	schema.DynamicField("street", schema.Text()),
	schema.DynamicField("city", schema.Text()),
)

// orderLine is an entity: it has an identity, so it is a thing rather than a
// part -- and it gets a table of its own. Its identity is the application's,
// so it is Identity without Computed, and it is bounded because MySQL cannot
// key an unbounded string.
var orderLine = schema.Struct[dynamic.Value]("OrderLine",
	schema.DynamicField("id", schema.UUID().Check(schema.MaxLength(36))).Identity(),
	schema.DynamicField("sku", schema.Text()),
	schema.DynamicField("quantity", schema.Int32().Check(schema.AtLeast[int32](1))),
	schema.DynamicField("lineTotal", schema.Int64()).
		Computed().
		WithDefault(dynamic.OfInteger(0)),
)

var order = schema.Struct[dynamic.Value]("Order",
	// The database generates it, so it is both: an identity, and not the
	// caller's to give.
	schema.DynamicField("id", schema.Int64()).Identity().Computed(),
	schema.DynamicField("reference", schema.UUID()),
	schema.DynamicField("shipTo", address),
	schema.DynamicField("lines", schema.List(orderLine)),
	schema.DynamicField("placedAt", schema.Time()).Computed().WithDefaultNow(),
)

// fieldNames is the field names of an object, in order.
func fieldNames(t *testing.T, node structure.Node) []string {
	t.Helper()
	object, isObject := node.(structure.Object)
	if !isObject {
		t.Fatalf("expected an object, got %T", node)
	}
	names := make([]string, 0, len(object.Fields))
	for _, field := range object.Fields {
		names = append(names, field.Name)
	}
	return names
}
