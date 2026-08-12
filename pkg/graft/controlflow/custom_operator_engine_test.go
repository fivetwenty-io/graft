package controlflow

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// probeFlagOperator is a zero-argument custom operator used to prove that
// control-flow expressions resolve operators through the engine that is
// doing the parse, not only through DefaultRegistry. It always returns
// true, so "(( if probeflag ))" takes its true branch — but only once the
// engine's local registration is actually consulted; before the F3 fix, the
// nil-engine parser used inside condition evaluation saw an unregistered
// name, rewrote "probeflag" to "grab probeflag" (scope.go's
// bareIdentifierRe/evalExpr), and failed with "unable to resolve
// 'probeflag'" because no such document key exists.
type probeFlagOperator struct{}

func (probeFlagOperator) Setup() error {
	return nil
}

func (probeFlagOperator) Phase() graft.OperatorPhase {
	return graft.EvalPhase
}

func (probeFlagOperator) Dependencies(_ *graft.Evaluator, _ []*graft.Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

func (probeFlagOperator) Run(_ *graft.Evaluator, _ []*graft.Expr) (*graft.Response, error) {
	return &graft.Response{Type: graft.Replace, Value: true}, nil
}

// TestExpand_CustomOperatorResolvesInsideIfCondition is the F3 red-then-green
// repro from the review: an engine-local custom operator referenced inside
// (( if ... )) must resolve through that engine, exactly like it does
// everywhere else in the document. The condition calls "probeflag()" with
// explicit parens rather than a bare "probeflag": a bare identifier
// argument to evalTruthy's "! ! (...)" wrapper sits in a parenthesized
// group's non-primary position, where the parser's own two-token
// disambiguation rule (identifierOpensOpcallAt) always treats a name
// immediately followed by ")" as a reference regardless of engine
// awareness — that is unrelated, pre-existing parser behavior, not what F3
// is about. Explicit-call syntax is recognized as an operator call at any
// position (parseIdentifierOrOperator's "followed by '(' " branch), so it
// isolates the actual engine-registry lookup this test exists to cover.
// Reverting the engine parameter threaded through
// Expand/env/evalExpr/buildPrescanScope (i.e. passing nil at
// pkg/graft/engine.go's ControlFlowExpander(e, data) call) reproduces the
// original failure: a parse error, because pre-fix the nil-engine parser
// sees NullOperator for "probeflag" and treats the bare name as a
// reference, leaving the call's "()" as unconsumed, unexpected tokens.
func TestExpand_CustomOperatorResolvesInsideIfCondition(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithCustomOperator("probeflag", probeFlagOperator{}))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	src := "(( if probeflag() ))\nfoo: 1\n(( fi ))\n"
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v (want the (( if )) branch to take, not error — this is exactly the F3 symptom if it fires)", err)
	}
	if doc == nil {
		t.Fatal("ParseYAML returned a nil document")
	}

	data, ok := doc.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("document data is not a map: %#v", doc.RawData())
	}
	if _, present := data["foo"]; !present {
		t.Fatalf("data = %#v, want key \"foo\" present (the if-condition's custom operator should have evaluated true)", data)
	}
}

// TestExpand_CustomOperatorResolvesInsideForIterable proves the same for a
// (( for )) loop's iterable expression, which goes through evalExpr's same
// code path as (( if ))'s condition, but without the "! !" truthy wrapper.
// A *bare* "probelist" iterable is deliberately always rewritten to
// "grab probelist" regardless of operator registration — scope.go's
// bareIdentifierRe/evalExpr doc comments, spec decision C-1, sidestepping
// the operator-name-collision case entirely — so this test also uses
// explicit call syntax ("probelist()") to reach the engine-aware operator
// lookup this test exists to cover, not the unconditional bare-identifier
// grab rewrite (a separate, correct, unrelated behavior).
func TestExpand_CustomOperatorResolvesInsideForIterable(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithCustomOperator("probelist", probeListOperator{}))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	src := "out:\n" +
		"(( for item in probelist() ))\n" +
		"- name: (( grab item ))\n" +
		"(( done ))\n"
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}
	if doc == nil {
		t.Fatal("ParseYAML returned a nil document")
	}
}

// probeListOperator is a zero-argument custom operator returning a small
// list, used to exercise (( for item in probelist )).
type probeListOperator struct{}

func (probeListOperator) Setup() error {
	return nil
}

func (probeListOperator) Phase() graft.OperatorPhase {
	return graft.EvalPhase
}

func (probeListOperator) Dependencies(_ *graft.Evaluator, _ []*graft.Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

func (probeListOperator) Run(_ *graft.Evaluator, _ []*graft.Expr) (*graft.Response, error) {
	return &graft.Response{Type: graft.Replace, Value: []interface{}{"a", "b"}}, nil
}

// probeNameOperator is a zero-argument custom operator returning a fixed
// string, used to prove buildPrescanScope's engine-aware resolution with a
// discriminating assertion: an unresolved (( probename )) reference echoes
// its own source text (NullOperator.Run), which is itself a non-empty
// string and so, misleadingly, still truthy — a boolean-condition probe
// (like probeFlagOperator's) cannot tell "resolved" from "left as literal
// placeholder text" apart. Routing the value through (( case )) exact-match
// instead can: only the genuinely resolved value "probed-value" takes the
// "probed-value" branch; the literal placeholder "(( probename() ))" takes
// neither branch and the case falls through with no default.
type probeNameOperator struct{}

func (probeNameOperator) Setup() error {
	return nil
}

func (probeNameOperator) Phase() graft.OperatorPhase {
	return graft.EvalPhase
}

func (probeNameOperator) Dependencies(_ *graft.Evaluator, _ []*graft.Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

func (probeNameOperator) Run(_ *graft.Evaluator, _ []*graft.Expr) (*graft.Response, error) {
	return &graft.Response{Type: graft.Replace, Value: "probed-value"}, nil
}

// TestExpand_CustomOperatorResolvesInPrescanScope proves buildPrescanScope
// (spec §8.3 step 2) evaluates the static remainder of the document through
// the same engine, so a custom operator referenced there (outside any
// control-flow block, but in a document that also contains one) resolves
// too. mode's value only reaches the "probed-value" case branch if the
// prescan scope actually ran probeNameOperator, not merely echoed its
// placeholder text.
func TestExpand_CustomOperatorResolvesInPrescanScope(t *testing.T) {
	engine, err := graft.NewEngine(graft.WithCustomOperator("probename", probeNameOperator{}))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	src := "mode: (( probename() ))\n" +
		"(( case mode ))\n" +
		"(( when \"probed-value\" ))\n" +
		"foo: 1\n" +
		"(( esac ))\n"
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}
	data, ok := doc.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("document data is not a map: %#v", doc.RawData())
	}
	if _, present := data["foo"]; !present {
		t.Fatalf("data = %#v, want key \"foo\" present (prescan scope should have resolved probename() to \"probed-value\", not left it as unresolved literal text)", data)
	}
}

// TestExpand_NilEngineControlFlowUnchanged is a regression guard: control
// flow with no custom operators, evaluated through Expand's nil-engine path
// directly (as a caller outside DefaultEngine.ParseYAML might), still
// resolves ordinary built-in operators exactly as before.
func TestExpand_NilEngineControlFlowUnchanged(t *testing.T) {
	src := "environment: production\n" +
		"(( if environment == \"production\" ))\n" +
		"replicas: 5\n" +
		"(( fi ))\n"
	out, err := Expand(nil, []byte(src))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("Expand returned empty output")
	}
}
