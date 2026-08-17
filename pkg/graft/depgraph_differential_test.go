package graft

import (
	"bytes"
	"context"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/parallel"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// This file is the C9a differential test suite required by Wave C's
// library-API plan: DependencyGraph/BuildEvalPlan must agree with the two
// live orderings - the sequential kahnSort path (evaluator.go) and the
// parallel scheduler path (evaluator_parallel.go's runOpsWithScheduler,
// backed by internal/parallel/scheduler.go) - for the same document. Both
// live paths compute dependency edges independently (evaluator.go's
// findDependency for kahnSort, resolveOpIDForDependency for the parallel
// path) and are kept "in lockstep by hand", per evaluator_parallel.go's
// own doc comment; this suite is the guard against that hand-maintained
// lockstep drifting.

// depDifferentialCorpus is the fixture corpus these tests run against:
// hand-built canonical shapes (chain/diamond/islands), plus the exact two
// documents from calc_sibling_dependency_parallel_test.go's
// TestCalcBareSiblingVariable_ParallelEngine_* tests - real regression
// fixtures for the exact "sequential and parallel evaluation derive
// different dependency graphs" bug class this cluster exists to prevent
// from recurring, reused here rather than duplicated by name only.
func depDifferentialCorpus() []struct {
	name string
	yaml string
} {
	return []struct {
		name string
		yaml string
	}{
		{"linear-chain", "a: (( grab c ))\nb: (( grab a ))\nc: hello\n"},
		{"diamond", "base: seed\nleft: (( grab base ))\nright: (( grab base ))\njoined: (( concat left right ))\n"},
		{"disjoint-islands", "island_a:\n  x: seed\n  y: (( grab island_a.x ))\nisland_b:\n  x: seed\n  y: (( grab island_b.x ))\n"},
		{"calc-bare-sibling", "sizing:\n  width: (( calc \"10 * 2\" ))\n  area: (( calc \"floor(width)\" ))\n"},
		{"calc-infix-sibling", "calculations:\n  total_with_tax: 108\n  discount_amount: 15\n  final_price: (( calculations.total_with_tax - calculations.discount_amount ))\n  final_price_int: (( calc \"floor(final_price)\" ))\n"},
		{"wide-fan-in", "a: 1\nb: 2\nc: 3\nsum: (( concat a b c ))\nrepeat1: (( grab sum ))\nrepeat2: (( grab sum ))\nrepeat3: (( grab sum ))\n"},
	}
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// runWithTimeout fails t if fn does not return promptly - the guard
// against DependencyGraph/BuildEvalPlan/TopologicalSort hanging on a
// cyclic document instead of returning an error, which the plan's C9a
// section requires explicitly.
func runWithTimeout(t *testing.T, fn func()) {
	t.Helper()
	const d = 5 * time.Second
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("timed out after %s - suspected hang", d)
	}
}

// parallelPathEdges independently derives the dependency edges the live
// parallel path (runOpsWithScheduler) would compute for ops, by calling
// its own resolveOpIDForDependency function directly - not a
// reimplementation of it - over the exact op set ev.DataFlow(phase)
// found. Returned as dependent-canonical-path -> sorted dependency-
// canonical-paths, matching DependencyGraph.Dependencies' shape.
func parallelPathEdges(ev *Evaluator, ops []*Opcall) map[string][]string {
	allLocs := make([]*tree.Cursor, 0, len(ops))
	for _, op := range ops {
		if op.Where() != nil {
			allLocs = append(allLocs, op.Where())
		}
	}

	opIDMap := make(map[string]string, len(ops))
	canonicalByID := make(map[string]string, len(ops))
	for _, op := range ops {
		if op.Where() == nil {
			continue
		}
		id := op.Where().String()
		opIDMap[id] = id
		if op.Canonical() != nil {
			canonicalByID[id] = op.Canonical().String()
			opIDMap[op.Canonical().String()] = id
		} else {
			canonicalByID[id] = id
		}
	}

	edges := make(map[string][]string)
	savedHere := ev.Here
	for _, op := range ops {
		if op.Where() == nil {
			continue
		}
		taskID := op.Where().String()
		ev.Here = op.Where()

		if op.Operator() != nil {
			depCursors := op.Dependencies(ev, allLocs)
			seen := make(map[string]bool)
			for _, depCursor := range depCursors {
				id, ok := resolveOpIDForDependency(ev, opIDMap, depCursor)
				if !ok || id == taskID || seen[id] {
					continue
				}
				seen[id] = true
				dependentPath := canonicalByID[taskID]
				depPath := canonicalByID[id]
				edges[dependentPath] = append(edges[dependentPath], depPath)
			}
		}
	}
	ev.Here = savedHere

	for k := range edges {
		sort.Strings(edges[k])
	}
	return edges
}

// TestDependencyGraph_DifferentialAgainstParallelPath asserts that for
// every fixture, DependencyGraph.Dependencies (built on the sequential
// path's findDependency, via computeDataFlowGraph) reports exactly the
// same edges the live parallel path's own resolveOpIDForDependency would
// compute for the same ops.
func TestDependencyGraph_DifferentialAgainstParallelPath(t *testing.T) {
	for _, fx := range depDifferentialCorpus() {
		t.Run(fx.name, func(t *testing.T) {
			engine := sequentialTestEngine(t)
			data := parseYAMLForTest(t, fx.yaml)

			graph, err := engine.BuildDependencyGraph(NewDocument(data), EvalPhase)
			if err != nil {
				t.Fatalf("BuildDependencyGraph: %v", err)
			}

			ev := engine.createEvaluator(data)
			ops, err := ev.DataFlow(EvalPhase)
			if err != nil {
				t.Fatalf("DataFlow: %v", err)
			}

			parEdges := parallelPathEdges(ev, ops)

			for _, node := range graph.Nodes() {
				seqDeps := graph.Dependencies(node.Path)
				parDeps := parEdges[node.Path]
				if !equalStringSets(seqDeps, parDeps) {
					t.Errorf("%s: sequential-path deps = %v, parallel-path deps = %v (must agree)", node.Path, seqDeps, parDeps)
				}
			}

			// And the reverse direction: no edge the parallel path reports
			// that the graph missed entirely (a node absent from the graph
			// would mean BuildDependencyGraph silently dropped an operator
			// DataFlow found).
			for path := range parEdges {
				if _, err := indexInNodes(graph, path); err != nil {
					t.Errorf("parallel path reports dependent %q, absent from DependencyGraph.Nodes()", path)
				}
			}
		})
	}
}

func indexInNodes(g *DependencyGraph, path string) (int, error) {
	for i, n := range g.Nodes() {
		if n.Path == path {
			return i, nil
		}
	}
	return -1, errNotFound
}

var errNotFound = &notFoundError{}

type notFoundError struct{}

func (*notFoundError) Error() string { return "not found" }

// TestDependencyGraph_DifferentialAgainstKahnSort asserts that DataFlow's
// live sequential order (backed by kahnSort) respects every dependency
// edge DependencyGraph reports: a dependency never appears later in
// DataFlow's flat order than something that depends on it.
func TestDependencyGraph_DifferentialAgainstKahnSort(t *testing.T) {
	for _, fx := range depDifferentialCorpus() {
		t.Run(fx.name, func(t *testing.T) {
			engine := sequentialTestEngine(t)
			data := parseYAMLForTest(t, fx.yaml)

			graph, err := engine.BuildDependencyGraph(NewDocument(data), EvalPhase)
			if err != nil {
				t.Fatalf("BuildDependencyGraph: %v", err)
			}

			ev := engine.createEvaluator(data)
			ops, err := ev.DataFlow(EvalPhase)
			if err != nil {
				t.Fatalf("DataFlow: %v", err)
			}

			pos := make(map[string]int, len(ops))
			for i, op := range ops {
				pos[op.Canonical().String()] = i
			}

			for _, node := range graph.Nodes() {
				for _, dep := range graph.Dependencies(node.Path) {
					depPos, depKnown := pos[dep]
					nodePos, nodeKnown := pos[node.Path]
					if !depKnown || !nodeKnown {
						t.Fatalf("DataFlow order is missing %q or %q found by DependencyGraph", dep, node.Path)
					}
					if depPos >= nodePos {
						t.Errorf("kahnSort order places dependency %q (pos %d) at or after dependent %q (pos %d)", dep, depPos, node.Path, nodePos)
					}
				}
			}
		})
	}
}

// TestBuildEvalPlan_MatchesLiveParallelScheduler asserts that BuildEvalPlan
// produces the same wave partition (by canonical path) as feeding the live
// parallel path's own edges (parallelPathEdges) into a fresh
// internal/parallel.Scheduler - the identical type runOpsWithScheduler
// itself builds and schedules. Since BuildEvalPlan is implemented on top
// of the same scheduler type, this differential test is really asserting
// the two callers feed it equivalent edges, which is what actually could
// drift.
func TestBuildEvalPlan_MatchesLiveParallelScheduler(t *testing.T) {
	for _, fx := range depDifferentialCorpus() {
		t.Run(fx.name, func(t *testing.T) {
			engine := sequentialTestEngine(t)
			data := parseYAMLForTest(t, fx.yaml)

			graph, err := engine.BuildDependencyGraph(NewDocument(data), EvalPhase)
			if err != nil {
				t.Fatalf("BuildDependencyGraph: %v", err)
			}

			ev := engine.createEvaluator(data)
			ops, err := ev.DataFlow(EvalPhase)
			if err != nil {
				t.Fatalf("DataFlow: %v", err)
			}
			parEdges := parallelPathEdges(ev, ops)

			liveScheduler := parallel.NewScheduler()
			for _, op := range ops {
				path := op.Canonical().String()
				if err := liveScheduler.AddTask(&parallel.Task{ID: path, Dependencies: parEdges[path]}); err != nil {
					t.Fatalf("AddTask(%s): %v", path, err)
				}
			}
			liveWaves, err := liveScheduler.Schedule()
			if err != nil {
				t.Fatalf("live scheduler Schedule: %v", err)
			}

			var waves []EvalWave
			runWithTimeout(t, func() {
				waves = BuildEvalPlan(graph)
			})

			if len(waves) != len(liveWaves) {
				t.Fatalf("BuildEvalPlan produced %d waves, live scheduler produced %d", len(waves), len(liveWaves))
			}
			for i := range waves {
				got := pathsOfRefs(waves[i].Operators)
				var want []string
				for _, task := range liveWaves[i] {
					want = append(want, task.ID)
				}
				if !equalStringSets(got, want) {
					t.Errorf("wave %d: BuildEvalPlan = %v, live scheduler = %v", i, got, want)
				}
			}
		})
	}
}

// TestEvaluateParallel_DifferentialAgainstSequentialEvaluate runs every
// fixture through the sequential engine.Evaluate and through
// engine.EvaluateParallel (fed the BuildDependencyGraph/BuildEvalPlan plan
// for the same document) and asserts byte-identical YAML output, several
// times each to guard against a fix that only wins a goroutine-scheduling
// race (see calc_sibling_dependency_parallel_test.go's identical
// rationale for its own repeated-iteration test).
func TestEvaluateParallel_DifferentialAgainstSequentialEvaluate(t *testing.T) {
	for _, fx := range depDifferentialCorpus() {
		t.Run(fx.name, func(t *testing.T) {
			seqEngine := sequentialTestEngine(t)
			seqData := parseYAMLForTest(t, fx.yaml)
			seqResult, err := seqEngine.Evaluate(context.Background(), NewDocument(seqData))
			if err != nil {
				t.Fatalf("sequential Evaluate: %v", err)
			}
			wantYAML, err := seqResult.ToYAML()
			if err != nil {
				t.Fatalf("sequential ToYAML: %v", err)
			}

			for i := 0; i < 10; i++ {
				parEngine := newParallelEngine(t)
				parData := parseYAMLForTest(t, fx.yaml)
				parDoc := NewDocument(parData)

				graph, err := parEngine.BuildDependencyGraph(parDoc, EvalPhase)
				if err != nil {
					t.Fatalf("iteration %d: BuildDependencyGraph: %v", i, err)
				}
				waves := BuildEvalPlan(graph)

				parResult, err := parEngine.EvaluateParallel(context.Background(), parDoc, waves)
				if err != nil {
					t.Fatalf("iteration %d: EvaluateParallel: %v", i, err)
				}
				gotYAML, err := parResult.ToYAML()
				if err != nil {
					t.Fatalf("iteration %d: parallel ToYAML: %v", i, err)
				}

				if !bytes.Equal(gotYAML, wantYAML) {
					t.Fatalf("iteration %d: EvaluateParallel output differs from sequential Evaluate:\nsequential:\n%s\nparallel:\n%s", i, wantYAML, gotYAML)
				}
			}
		})
	}
}

// TestDependencyGraph_CycleMatchesLiveEvaluatorErrorClass asserts a cyclic
// document produces a comparable error from both the live sequential
// evaluator (Evaluate/DataFlow's kahnSort) and the public
// DependencyGraph/TopologicalSort projection, and that neither hangs.
func TestDependencyGraph_CycleMatchesLiveEvaluatorErrorClass(t *testing.T) {
	yaml := "a: (( grab b ))\nb: (( grab a ))\n"

	engine := sequentialTestEngine(t)
	data := parseYAMLForTest(t, yaml)

	var liveErr error
	runWithTimeout(t, func() {
		_, liveErr = engine.Evaluate(context.Background(), NewDocument(data))
	})
	if liveErr == nil {
		t.Fatal("live sequential Evaluate on a cyclic document did not return an error")
	}
	if !strings.Contains(liveErr.Error(), "cycle") {
		t.Errorf("live Evaluate error = %q, want it to mention a cycle", liveErr.Error())
	}

	var graph *DependencyGraph
	var buildErr error
	runWithTimeout(t, func() {
		graph, buildErr = engine.BuildDependencyGraph(NewDocument(parseYAMLForTest(t, yaml)), EvalPhase)
	})
	if buildErr != nil {
		t.Fatalf("BuildDependencyGraph on a cyclic document: %v", buildErr)
	}

	var cycles [][]string
	runWithTimeout(t, func() {
		cycles = graph.DetectCycles()
	})
	if len(cycles) == 0 {
		t.Error("DetectCycles found no cycle for a document the live evaluator rejected as cyclic")
	}

	var sortErr error
	runWithTimeout(t, func() {
		_, sortErr = graph.TopologicalSort()
	})
	if sortErr == nil {
		t.Error("TopologicalSort on a cyclic graph did not return an error")
	}

	var waves []EvalWave
	runWithTimeout(t, func() {
		waves = BuildEvalPlan(graph)
	})
	if waves != nil {
		t.Errorf("BuildEvalPlan on a cyclic graph = %v, want nil", waves)
	}
}

// TestEvaluateParallel_RaceRepeated exercises EvaluateParallel's real
// worker-pool dispatch across many independent operators, repeated, so
// -race has a realistic chance to surface any shared-state bug in the
// forced-parallel path added for this cluster.
func TestEvaluateParallel_RaceRepeated(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("v")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(": (( concat \"item-\" \"")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("\" ))\n")
	}
	yaml := b.String()

	for i := 0; i < 10; i++ {
		engine := newParallelEngine(t)
		data := parseYAMLForTest(t, yaml)
		doc := NewDocument(data)

		result, err := engine.EvaluateParallel(context.Background(), doc, nil)
		if err != nil {
			t.Fatalf("iteration %d: EvaluateParallel: %v", i, err)
		}
		got, err := result.Get("v0")
		if err != nil {
			t.Fatalf("iteration %d: Get(v0): %v", i, err)
		}
		if got != "item-0" {
			t.Errorf("iteration %d: v0 = %v, want item-0", i, got)
		}
	}
}
