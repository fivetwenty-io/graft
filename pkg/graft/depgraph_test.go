package graft

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
)

// buildTestGraph parses yamlSrc with a fresh sequential engine and
// returns the eval phase's DependencyGraph, failing the test on any
// error.
func buildTestGraph(t *testing.T, engine *DefaultEngine, yamlSrc string) *DependencyGraph {
	t.Helper()
	const phase = EvalPhase
	tree := parseYAMLForTest(t, yamlSrc)
	g, err := engine.BuildDependencyGraph(NewDocument(tree), phase)
	if err != nil {
		t.Fatalf("BuildDependencyGraph: %v", err)
	}
	return g
}

func sequentialTestEngine(t *testing.T) *DefaultEngine {
	t.Helper()
	opts := defaultEngineOpts()
	return newEngineFromOptions(&opts)
}

// --- Fixture shapes: linear chain, diamond, disjoint islands, self-cycle, mutual cycle ---

func TestDependencyGraph_LinearChain(t *testing.T) {
	engine := sequentialTestEngine(t)
	g := buildTestGraph(t, engine, "a: (( grab c ))\nb: (( grab a ))\nc: hello\n")

	nodes := g.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 operator nodes (a, b - c is a literal), got %d: %+v", len(nodes), nodes)
	}

	// b depends on a; a has no operator dependency (c is a literal, not an opcall node).
	if deps := g.Dependencies("b"); len(deps) != 1 || deps[0] != "a" {
		t.Errorf("Dependencies(b) = %v, want [a]", deps)
	}
	if deps := g.Dependencies("a"); len(deps) != 0 {
		t.Errorf("Dependencies(a) = %v, want none (c is a literal, not an operator node)", deps)
	}
	if dependents := g.Dependents("a"); len(dependents) != 1 || dependents[0] != "b" {
		t.Errorf("Dependents(a) = %v, want [b]", dependents)
	}

	order, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	pos := positionOf(order)
	if pos["a"] >= pos["b"] {
		t.Errorf("TopologicalSort order %v places b before or with a", pathsOf(order))
	}

	if cycles := g.DetectCycles(); len(cycles) != 0 {
		t.Errorf("DetectCycles() = %v, want none", cycles)
	}
}

func TestDependencyGraph_Diamond(t *testing.T) {
	engine := sequentialTestEngine(t)
	yamlSrc := "" +
		"base: seed\n" +
		"left: (( grab base ))\n" +
		"right: (( grab base ))\n" +
		"joined: (( concat left right ))\n"
	g := buildTestGraph(t, engine, yamlSrc)

	if deps := g.Dependencies("joined"); len(deps) != 2 {
		t.Fatalf("Dependencies(joined) = %v, want 2 entries (left, right)", deps)
	} else {
		sort.Strings(deps)
		if deps[0] != "left" || deps[1] != "right" {
			t.Errorf("Dependencies(joined) = %v, want [left right]", deps)
		}
	}

	waves := BuildEvalPlan(g)
	if len(waves) != 2 {
		t.Fatalf("BuildEvalPlan produced %d waves, want 2 (left+right, then joined): %+v", len(waves), waves)
	}
	firstWave := pathsOfRefs(waves[0].Operators)
	sort.Strings(firstWave)
	if len(firstWave) != 2 || firstWave[0] != "left" || firstWave[1] != "right" {
		t.Errorf("wave 0 = %v, want [left right]", firstWave)
	}
	secondWave := pathsOfRefs(waves[1].Operators)
	if len(secondWave) != 1 || secondWave[0] != "joined" {
		t.Errorf("wave 1 = %v, want [joined]", secondWave)
	}
}

func TestDependencyGraph_DisjointIslands(t *testing.T) {
	engine := sequentialTestEngine(t)
	yamlSrc := "" +
		"island_a:\n" +
		"  x: seed\n" +
		"  y: (( grab island_a.x ))\n" +
		"island_b:\n" +
		"  x: seed\n" +
		"  y: (( grab island_b.x ))\n"
	g := buildTestGraph(t, engine, yamlSrc)

	if len(g.Nodes()) != 2 {
		t.Fatalf("expected 2 operator nodes, got %d: %+v", len(g.Nodes()), g.Nodes())
	}

	waves := BuildEvalPlan(g)
	if len(waves) != 1 {
		t.Fatalf("BuildEvalPlan produced %d waves, want 1 (both islands' single op are independent): %+v", len(waves), waves)
	}
	if len(waves[0].Operators) != 2 {
		t.Errorf("wave 0 has %d operators, want 2", len(waves[0].Operators))
	}
}

func TestDependencyGraph_SelfCycle(t *testing.T) {
	engine := sequentialTestEngine(t)
	g := buildTestGraph(t, engine, "a: (( grab a ))\n")

	cycles := g.DetectCycles()
	if len(cycles) == 0 {
		t.Fatalf("DetectCycles() found no cycle for a self-referencing grab")
	}
	foundSelfCycle := false
	for _, c := range cycles {
		if len(c) == 1 && c[0] == "a" {
			foundSelfCycle = true
		}
	}
	if !foundSelfCycle {
		t.Errorf("DetectCycles() = %v, want a cycle containing exactly [a]", cycles)
	}

	if _, err := g.TopologicalSort(); !errors.Is(err, ErrDependencyCycle) {
		t.Errorf("TopologicalSort() error = %v, want ErrDependencyCycle", err)
	}

	if waves := BuildEvalPlan(g); waves != nil {
		t.Errorf("BuildEvalPlan on a cyclic graph = %v, want nil", waves)
	}
}

func TestDependencyGraph_MutualCycle(t *testing.T) {
	engine := sequentialTestEngine(t)
	g := buildTestGraph(t, engine, "a: (( grab b ))\nb: (( grab a ))\n")

	cycles := g.DetectCycles()
	if len(cycles) == 0 {
		t.Fatalf("DetectCycles() found no cycle for a<->b mutual grab")
	}
	for _, c := range cycles {
		set := map[string]bool{}
		for _, p := range c {
			set[p] = true
		}
		if !set["a"] || !set["b"] {
			t.Errorf("cycle %v does not contain both a and b", c)
		}
	}

	if _, err := g.TopologicalSort(); !errors.Is(err, ErrDependencyCycle) {
		t.Errorf("TopologicalSort() error = %v, want ErrDependencyCycle", err)
	}
	if waves := BuildEvalPlan(g); waves != nil {
		t.Errorf("BuildEvalPlan on a cyclic graph = %v, want nil", waves)
	}
}

func TestDependencyGraph_ToDOT(t *testing.T) {
	engine := sequentialTestEngine(t)
	g := buildTestGraph(t, engine, "a: (( grab c ))\nb: (( grab a ))\nc: hello\n")

	dot := g.ToDOT()
	if !strings.HasPrefix(dot, "digraph DependencyGraph {") {
		t.Errorf("ToDOT() does not start with digraph header: %q", dot)
	}
	if !strings.Contains(dot, `"a" -> "b"`) {
		t.Errorf("ToDOT() = %q, want an edge from a to b", dot)
	}
}

func TestDependencyGraph_NilReceiverIsSafe(t *testing.T) {
	var g *DependencyGraph
	if got := g.Nodes(); got != nil {
		t.Errorf("nil.Nodes() = %v, want nil", got)
	}
	if got := g.Dependencies("x"); got != nil {
		t.Errorf("nil.Dependencies(x) = %v, want nil", got)
	}
	if got := g.Dependents("x"); got != nil {
		t.Errorf("nil.Dependents(x) = %v, want nil", got)
	}
	if got := g.DetectCycles(); got != nil {
		t.Errorf("nil.DetectCycles() = %v, want nil", got)
	}
	if got, err := g.TopologicalSort(); got != nil || err != nil {
		t.Errorf("nil.TopologicalSort() = %v, %v, want nil, nil", got, err)
	}
	if got := BuildEvalPlan(g); got != nil {
		t.Errorf("BuildEvalPlan(nil) = %v, want nil", got)
	}
	if dot := g.ToDOT(); !strings.Contains(dot, "digraph DependencyGraph") {
		t.Errorf("nil.ToDOT() = %q, want a valid empty digraph", dot)
	}
}

func TestBuildDependencyGraph_InvalidInputs(t *testing.T) {
	engine := sequentialTestEngine(t)

	if _, err := engine.BuildDependencyGraph(nil, EvalPhase); err == nil {
		t.Error("BuildDependencyGraph(nil, ...) did not return an error")
	}
}

// --- EvaluateParallel ---

func TestEvaluateParallel_NoWorkerPool(t *testing.T) {
	engine := sequentialTestEngine(t) // no pool configured
	doc := NewDocument(parseYAMLForTest(t, "a: hello\n"))

	_, err := engine.EvaluateParallel(context.Background(), doc, nil)
	if !errors.Is(err, ErrNoWorkerPool) {
		t.Errorf("EvaluateParallel with no pool: err = %v, want ErrNoWorkerPool", err)
	}
}

func TestEvaluateParallel_NilDoc(t *testing.T) {
	engine := newParallelEngine(t)
	if _, err := engine.EvaluateParallel(context.Background(), nil, nil); err == nil {
		t.Error("EvaluateParallel(nil doc) did not return an error")
	}
}

func TestEvaluateParallel_SucceedsWithoutWaves(t *testing.T) {
	engine := newParallelEngine(t)
	doc := NewDocument(parseYAMLForTest(t, "x: hello\ny: (( grab x ))\n"))

	result, err := engine.EvaluateParallel(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("EvaluateParallel: %v", err)
	}
	if got, _ := result.Get("y"); got != "hello" {
		t.Errorf("y = %v, want hello", got)
	}
}

func TestEvaluateParallel_SucceedsWithValidPlan(t *testing.T) {
	engine := newParallelEngine(t)
	tree := parseYAMLForTest(t, "x: hello\ny: (( grab x ))\n")
	doc := NewDocument(tree)

	graph, err := engine.BuildDependencyGraph(doc, EvalPhase)
	if err != nil {
		t.Fatalf("BuildDependencyGraph: %v", err)
	}
	waves := BuildEvalPlan(graph)

	result, err := engine.EvaluateParallel(context.Background(), doc, waves)
	if err != nil {
		t.Fatalf("EvaluateParallel with a valid plan: %v", err)
	}
	if got, _ := result.Get("y"); got != "hello" {
		t.Errorf("y = %v, want hello", got)
	}
}

func TestEvaluateParallel_RejectsInvalidPlan(t *testing.T) {
	// A dependency edge only exists between two operator calls (findDependency
	// looks up the dependency path in the opcall set); a plain literal like
	// "z: hello" never produces one, so both x and y here must themselves be
	// opcalls for there to be a live edge to violate.
	yamlSrc := "z: hello\nx: (( grab z ))\ny: (( grab x ))\n"

	t.Run("with Phase set", func(t *testing.T) {
		engine := newParallelEngine(t)
		doc := NewDocument(parseYAMLForTest(t, yamlSrc))

		// y depends on x; place them in the wrong relative order.
		badPlan := []EvalWave{
			{Operators: []OperatorRef{{Path: "y", Operator: "grab", Phase: EvalPhase}}},
			{Operators: []OperatorRef{{Path: "x", Operator: "grab", Phase: EvalPhase}}},
		}

		_, err := engine.EvaluateParallel(context.Background(), doc, badPlan)
		if !errors.Is(err, ErrInvalidEvalPlan) {
			t.Errorf("EvaluateParallel with an inverted plan: err = %v, want ErrInvalidEvalPlan", err)
		}
	})

	// M1 regression (phase4-review.md): a hand-built plan that omits Phase
	// (left at its zero value, MergePhase) must be validated against every
	// phase, not silently checked against MergePhase alone - the same
	// inverted x/y ordering must be rejected identically whether or not the
	// caller bothered to set Phase.
	t.Run("with Phase left at zero value", func(t *testing.T) {
		engine := newParallelEngine(t)
		doc := NewDocument(parseYAMLForTest(t, yamlSrc))

		badPlan := []EvalWave{
			{Operators: []OperatorRef{{Path: "y", Operator: "grab"}}},
			{Operators: []OperatorRef{{Path: "x", Operator: "grab"}}},
		}

		_, err := engine.EvaluateParallel(context.Background(), doc, badPlan)
		if !errors.Is(err, ErrInvalidEvalPlan) {
			t.Errorf("EvaluateParallel with an inverted plan and no Phase set: err = %v, want ErrInvalidEvalPlan", err)
		}
	})
}

func TestEvaluateParallel_RejectsDuplicatePathInPlan(t *testing.T) {
	engine := newParallelEngine(t)
	doc := NewDocument(parseYAMLForTest(t, "x: hello\n"))

	badPlan := []EvalWave{
		{Operators: []OperatorRef{{Path: "x", Phase: EvalPhase}}},
		{Operators: []OperatorRef{{Path: "x", Phase: EvalPhase}}},
	}

	_, err := engine.EvaluateParallel(context.Background(), doc, badPlan)
	if !errors.Is(err, ErrInvalidEvalPlan) {
		t.Errorf("EvaluateParallel with a duplicate path: err = %v, want ErrInvalidEvalPlan", err)
	}
}

// --- helpers ---

func positionOf(refs []OperatorRef) map[string]int {
	pos := make(map[string]int, len(refs))
	for i, r := range refs {
		pos[r.Path] = i
	}
	return pos
}

func pathsOf(refs []OperatorRef) []string {
	paths := make([]string, len(refs))
	for i, r := range refs {
		paths[i] = r.Path
	}
	return paths
}

func pathsOfRefs(refs []OperatorRef) []string {
	return pathsOf(refs)
}
