package migrate

// What one step actually consists of, in order.
//
// Mostly statements. But a recomputation moves rows with a Go function -- so
// that it can read what is there, compute, write back, and call out if the
// change needs it -- and that is not a statement, so a step is a sequence of
// two kinds of thing rather than a list of strings.

import (
	"context"
	"fmt"

	"github.com/mbauer83/effect-golang-schema/schema/structure"
	"github.com/mbauer83/effect-golang-sql/ddl"
	"github.com/mbauer83/effect-golang-sql/evolve"
	"github.com/mbauer83/effect-golang-sql/sql"
	"github.com/mbauer83/effect-golang/effect"
)

// Action is one thing a step does.
//
// Exactly one of the two is set. A struct with two fields rather than a sealed
// interface because there are two of them and there will not be a third: the
// database either runs a statement or runs somebody's function against it.
type Action struct {
	// Name names it, for a report that has to say which one failed.
	Name string
	// Statement is the SQL, when this action is SQL.
	Statement string
	// Rows moves rows, when this action is a function.
	Rows func(ctx context.Context, within sql.Querier, spelling sql.Spelling) error
}

// Actions are everything a migration would do to get from one version to
// another, in order, without doing any of it.
//
// A dry run, which every migrator needs: a deployment wants to see the plan
// before it runs, and a review wants to read it. What it cannot show is what a
// row-moving function will do -- that is a Go function and its Statement is
// empty -- so an Action with no statement is one whose effect can only be read
// in the code it names.
func Actions(plan Plan, from string, to string) ([]Action, error) {
	if err := plan.fault(); err != nil {
		return nil, faultOf("reading the plan", plan.History.Name(), to, err)
	}
	return actionsBetween(plan, from, to)
}

// actions are the actions one step consists of.
//
// A recomputation is walked here rather than in ddl, because only this side
// knows how to run a function: the columns it needs arrive, its function moves
// the rows, and the columns it read are dropped. ddl refuses to project one
// whole for exactly that reason.
func actions(dialect ddl.Dialect, stage evolve.Stage) ([]Action, error) {
	rewrite, isRewrite := stage.Change.(evolve.Recomputation)
	if !isRewrite {
		statements, err := ddl.Statements(dialect, stage)
		if err != nil {
			return nil, err
		}
		return statementActions(evolve.Describe(stage.Change), statements), nil
	}
	return rewriteActions(dialect, stage, rewrite)
}

// rewriteActions is the three phases of a recomputation, in the only order they
// work in: what arrives, what moves, what goes.
func rewriteActions(
	dialect ddl.Dialect,
	stage evolve.Stage,
	recomputation evolve.Recomputation,
) ([]Action, error) {
	if recomputation.Forward.IsEmpty() {
		return nil, fmt.Errorf("%s: %w", recomputation.Name, errNothingToRun)
	}

	additions, before, err := structuralActions(dialect, recomputation, recomputation.Additions, stage.Before)
	if err != nil {
		return nil, err
	}
	removals, _, err := structuralActions(dialect, recomputation, recomputation.Removals, before)
	if err != nil {
		return nil, err
	}

	result := append([]Action{}, additions...)
	if recomputation.Forward.Rows != nil {
		// Between the two, which is the whole reason there are two: the
		// function needs the columns it writes to exist and the ones it reads
		// not to be gone yet.
		result = append(result, Action{Name: recomputation.Name, Rows: recomputation.Forward.Rows})
	}
	return append(result, removals...), nil
}

// structuralActions is one list's actions, and the description they leave behind.
func structuralActions(
	dialect ddl.Dialect,
	recomputation evolve.Recomputation,
	list []evolve.Change,
	before structure.Node,
) ([]Action, structure.Node, error) {
	result := []Action{}
	for _, change := range list {
		statements, err := ddl.Statements(dialect,
			evolve.Stage{Change: change, Before: before})
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", recomputation.Name, err)
		}
		result = append(result, statementActions(recomputation.Name, statements)...)

		object, isObject := before.(structure.Object)
		if !isObject {
			return nil, nil, fmt.Errorf("%s: %w", recomputation.Name, errNotAnObject)
		}
		applied, err := evolve.Apply(change, object)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", recomputation.Name, err)
		}
		before = applied
	}
	return result, before, nil
}

func statementActions(name string, statements []string) []Action {
	result := make([]Action, 0, len(statements))
	for _, statement := range statements {
		result = append(result, Action{Name: name, Statement: statement})
	}
	return result
}

// run does one action.
func run[R any](
	within sql.Querier,
	plan Plan,
	action Action,
	version string,
) migration[R, effect.Unit] {
	if action.Rows == nil {
		return runStatement[R](within, plan, action.Statement, nil, action.Name, version)
	}
	return effect.Try(
		func(ctx context.Context, _ R) (effect.Unit, error) {
			return effect.Unit{}, action.Rows(ctx, within, plan.Dialect)
		},
		func(err error) Fault {
			return faultOf(action.Name, plan.History.Name(), version, err)
		},
	)
}
