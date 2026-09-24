package sql

// What a described value holds, as a query's kind.

import (
	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// KindOf is what a described node holds, as a query's kind.
//
// A document -- a value object, a list, a map, a union in one column -- is a
// document whatever the dialect stores it as, because what a query may do with
// it is decided by its being a document and not by its being text on SQLite.
func KindOf(node structure.Node) Kind {
	for {
		switch shape := node.(type) {
		case structure.Nullable:
			node = shape.Inner
			continue
		case structure.Reference:
			if shape.Resolve == nil {
				return OfDocument
			}
			node = shape.Resolve()
			continue
		case structure.Scalar:
			switch shape.Kind {
			case structure.Integer:
				return OfWhole
			case structure.Number:
				return OfNumber
			case structure.Boolean:
				return OfTruth
			case structure.Bytes:
				return OfBytes
			case structure.Timestamp:
				return OfMoment
			default:
				return OfText
			}
		default:
			return OfDocument
		}
	}
}
