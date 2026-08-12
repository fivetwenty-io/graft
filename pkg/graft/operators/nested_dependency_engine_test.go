package operators

import (
	"testing"

	graft "github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// depReportOperator is a minimal custom operator whose Dependencies()
// method reports a fixed cursor, independent of its own arguments. It is
// registered only on a per-engine registry (never globally via RegisterOp),
// so OperatorFor("depreport") — the process-global-only lookup — always
// returns NullOperator for it; only OperatorForEngine, given the engine it
// was registered on, finds it.
type depReportOperator struct {
	reportPath string
}

func (o depReportOperator) Setup() error {
	return nil
}

func (o depReportOperator) Phase() graft.OperatorPhase {
	return graft.EvalPhase
}

func (o depReportOperator) Dependencies(_ *graft.Evaluator, _ []*graft.Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	cursor, err := tree.ParseCursor(o.reportPath)
	if err != nil {
		return auto
	}
	return append(append([]*tree.Cursor{}, auto...), cursor)
}

func (o depReportOperator) Run(_ *graft.Evaluator, _ []*graft.Expr) (*graft.Response, error) {
	return &graft.Response{Type: graft.Replace, Value: "unused"}, nil
}

// TestNestedOperatorDependencies_ConsultEngineRegistry is the F2 regression
// test: op_concat.go, op_cartesian_product.go, op_inject.go, and op_ips.go
// each have their own "nested operator" branch inside Dependencies() that
// looks the nested operator up (by name, via OperatorForEngine) and calls
// its Dependencies() method. For an expression parsed by graft's own
// Parser, that branch is redundant with a separate, already-correct path:
// Expr.Dependencies's OperatorCall case delegates to the nested Opcall's
// own Dependencies(), which already carries its Parser-resolved (and, since
// P0-1, already engine-aware) operator instance — so a document-driven
// test that merely nests a custom operator inside (( concat ... )) cannot
// distinguish the four sites' own lookup from that already-correct path
// (confirmed empirically: reverting all four while leaving the parser and
// evaluateNestedOperator fixed does not fail such a test).
//
// The four sites' lookup only matters when the nested Expr's Call field is
// nil — i.e., an OperatorCall expression built directly (not by the
// Parser), which Expr.Args() and Expr.Dependencies() both already handle as
// a distinct, supported shape (Args() falls back to Left/Right;
// Dependencies()'s OperatorCall case is a no-op when Call is nil). A
// library caller constructing operator ASTs programmatically instead of
// through YAML+Parser — exactly the shape of usage the graft library API
// is meant to support — hits exactly this. This test constructs such an
// Expr directly and calls each of the four operators' own Dependencies()
// method, bypassing the Parser entirely, to isolate the lookup this test
// exists to cover.
func TestNestedOperatorDependencies_ConsultEngineRegistry(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithCustomOperator("depreport", depReportOperator{reportPath: "produced.value"}))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	ev := &graft.Evaluator{Tree: map[string]interface{}{}}
	ev.SetEngine(engine)

	// A hand-built OperatorCall expression with no Call field — the shape
	// Expr.Args()/Expr.Dependencies() explicitly support for
	// programmatically constructed ASTs, and the only shape where the four
	// sites' own lookup is not shadowed by an already-correct parser-baked
	// path.
	nestedCall := &graft.Expr{Type: graft.OperatorCall, Operator: "depreport"}
	containerArgs := []*graft.Expr{nestedCall}

	cases := []struct {
		name string
		deps func() []*tree.Cursor
	}{
		{"concat", func() []*tree.Cursor { return (ConcatOperator{}).Dependencies(ev, containerArgs, nil, nil) }},
		{"cartesian-product", func() []*tree.Cursor { return (CartesianProductOperator{}).Dependencies(ev, containerArgs, nil, nil) }},
		{"inject", func() []*tree.Cursor { return (InjectOperator{}).Dependencies(ev, containerArgs, nil, nil) }},
		{"ips", func() []*tree.Cursor { return (IpsOperator{}).Dependencies(ev, containerArgs, nil, nil) }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deps := c.deps()
			for _, d := range deps {
				if d != nil && d.String() == "produced.value" {
					return
				}
			}
			t.Fatalf("%s.Dependencies(...) = %v, want it to include the nested custom operator's reported dependency \"produced.value\" — the engine-aware nested lookup did not find \"depreport\"", c.name, deps)
		})
	}
}
