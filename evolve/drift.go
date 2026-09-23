package evolve

// Whether a history and the description a program holds are the same shape.
//
// Its own file because it is the one question a history cannot answer about
// itself. Every version after the first is derived, so a step and a
// declaration cannot disagree about what the declaration became -- but nothing
// in a history says that the end of its chain is the shape the program reads
// rows into. A description is edited far more often than a history, so when
// the two come apart it is always the same way round, and silently: the
// migrator builds the table the chain describes and the program selects the
// columns the description names.

import (
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
)

// Validate reports whether this history's latest version is the description a
// program is holding, and says where they differ when it is not.
//
// The check the forward anchor needs. Version one is declared and every later
// one is derived, so a step and a declaration cannot disagree about what the
// declaration became -- but nothing said that the end of the chain is the
// shape the program actually reads rows into. A description edited without a
// step beside it leaves the two silently apart: the migrator builds the table
// the chain describes, the program selects the columns the description names,
// and the first anybody hears of it is a statement naming a column that is
// not there.
//
// So it is asked where both are known, which is where a store is built, and
// answered before anything is migrated. A history whose first version is
// stated as a frozen structure is exactly the case that needs it, since the
// frozen half is the half nobody edits and therefore the half that drifts.
//
//	if err := store.HistoryOfFilms().Validate(catalog.FilmSchema.Structure()); err != nil {
//	    return err
//	}
func (history History) Validate(node structure.Node) error {
	if history.fault != nil {
		return history.fault
	}
	current, isObject := node.(structure.Object)
	if !isObject {
		return errNotAnObject
	}
	latest, err := history.objectAt(history.Latest())
	if err != nil {
		return err
	}
	if difference := firstDifference(latest, current); difference != "" {
		return fmt.Errorf("%s at %q describes %s: %w",
			history.name, history.Latest(), difference, errDescriptionDiffers)
	}
	return nil
}

// firstDifference is how two versions of one description disagree, in the words
// somebody fixing it needs, and nothing when they agree.
//
// The first difference rather than all of them, because the first is the one
// to act on and a list of five is usually one mistake seen five ways.
func firstDifference(latest structure.Object, current structure.Object) string {
	if latest.Name != current.Name {
		return fmt.Sprintf("a %q and the program holds a %q", latest.Name, current.Name)
	}
	byName := map[string]structure.Field{}
	for _, field := range current.Fields {
		byName[field.Name] = field
	}
	for _, field := range latest.Fields {
		other, present := byName[field.Name]
		if !present {
			return fmt.Sprintf("a field %q the program does not", field.Name)
		}
		if field.Optional != other.Optional {
			return fmt.Sprintf("%q as %s and the program as %s", field.Name,
				optionality(field.Optional), optionality(other.Optional))
		}
		delete(byName, field.Name)
	}
	for name := range byName {
		return fmt.Sprintf("no field %q, which the program holds", name)
	}
	return ""
}

func optionality(optional bool) string {
	if optional {
		return "optional"
	}
	return "required"
}
