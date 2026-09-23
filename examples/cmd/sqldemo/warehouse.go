package main

// The aggregate, its three derived shapes, and the tables it becomes.
//
// One description does four jobs, and this prints all four so the point is
// visible rather than described: a column that changed would change every one
// of them together.

import (
	"fmt"
	"strings"

	"github.com/mbauer83/effect-golang-schema/schema/dynamic"
	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-schema/schema/variant"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/examples/warehouse"
)

func runWarehouse() {
	shape := warehouse.PalletSchema.Structure()

	fmt.Println("warehouse: the shapes one description has")
	for _, projection := range []struct {
		name string
		of   func(structure.Node) (structure.Node, error)
	}{
		{"create", variant.Create},
		{"create, with the items", variant.CreateWithEntities},
		{"update", variant.Update},
	} {
		node, err := projection.of(shape)
		if err != nil {
			fail(err)
		}
		fmt.Printf("  %-22s %s\n", projection.name, fieldsOf(node))
	}

	for _, dialect := range []ddl.Dialect{ddl.Postgres, ddl.MySQL} {
		statements, err := ddl.Create(dialect, shape)
		if err != nil {
			fail(err)
		}
		fmt.Printf("\nwarehouse: the tables, in %s\n", dialect.Name())
		for _, statement := range statements {
			fmt.Println(statement + ";")
		}
	}
}

// fieldsOf names an object's fields, with the optional ones marked -- because
// an update shape asking for nothing is the whole difference between it and a
// create shape with the identity removed.
func fieldsOf(node structure.Node) string {
	object, isObject := node.(structure.Object)
	if !isObject {
		return "(not an object)"
	}
	names := make([]string, 0, len(object.Fields))
	for _, field := range object.Fields {
		if field.Optional {
			names = append(names, field.Name+"?")
			continue
		}
		names = append(names, field.Name)
	}
	return strings.Join(names, ", ")
}

// runEvolution shows the migration: one declared step, and the three things it
// produces -- the derived version, the statements, and the value carried.
func runEvolution() {
	if err := warehouse.Pallets.Fault(); err != nil {
		fail(err)
	}
	fmt.Printf("\nwarehouse: the pallet's versions, latest %s\n", warehouse.Pallets.Latest())
	for _, version := range warehouse.Pallets.Versions() {
		node, err := warehouse.Pallets.At(version)
		if err != nil {
			fail(err)
		}
		fmt.Printf("  %-7s %s\n", version, fieldsOf(node))
	}

	// A value written under version one, carried to version two. The rename
	// moves it; the added field takes its default, because a pallet that
	// predates the field has nothing else it could hold.
	var pallet dynamic.Value = dynamic.Object{Fields: []dynamic.Field{
		{Name: "reference", Value: dynamic.OfText("P-1")},
		{Name: "warehouse", Value: dynamic.OfText("Kiel")},
	}}
	latest, err := warehouse.Pallets.Migrate("1.0.0", "3.1.0", pallet)
	if err != nil {
		fail(err)
	}
	fmt.Printf("  a value, 1.0.0 to 3.1.0  %s\n", membersOf(latest))
	back, err := warehouse.Pallets.Migrate("3.1.0", "1.0.0", latest)
	if err != nil {
		fail(err)
	}
	fmt.Printf("  and back, 3.1.0 to 1.0.0 %s\n", membersOf(back))

	// The statements a span produces, asked for up to 3.0.0. The span stops
	// there because 3.1.0 moves rows with a function, and a version reached by
	// running a function is not a version statements alone arrive at.
	for _, dialect := range []ddl.Dialect{ddl.Postgres, ddl.MySQL} {
		statements, err := ddl.Alter(dialect, warehouse.Pallets, "1.0.0", "3.0.0")
		if err != nil {
			fail(err)
		}
		fmt.Printf("\nwarehouse: 1.0.0 to 3.0.0, in %s\n", dialect.Name())
		for _, statement := range statements {
			fmt.Println("  " + statement + ";")
		}
	}

	// And what asking anyway produces: a refusal naming the step, rather than
	// SQL that would drop the column the function has to read. runMigration
	// crosses this step, because migrate is where a recomputation belongs.
	statements, err := ddl.Alter(ddl.Postgres, warehouse.Pallets, "3.0.0", "3.1.0")
	if err == nil {
		fail(fmt.Errorf("a rewriting produced %d statements as if it were structural", len(statements)))
	}
	fmt.Printf("\nwarehouse: 3.0.0 to 3.1.0, in postgres\n  %s\n", err)
}

// membersOf names a value's members and what they hold, briefly.
func membersOf(value dynamic.Value) string {
	object, isObject := value.(dynamic.Object)
	if !isObject {
		return "(not an object)"
	}
	members := make([]string, 0, len(object.Fields))
	for _, field := range object.Fields {
		text, isText := field.Value.(dynamic.Text)
		if !isText {
			members = append(members, field.Name)
			continue
		}
		members = append(members, field.Name+"="+text.Value)
	}
	return strings.Join(members, ", ")
}
