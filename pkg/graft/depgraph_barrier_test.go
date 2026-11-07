package graft

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/parallel"

	yamlv3 "github.com/goccy/go-yaml"
)

// expandedEdges is the dependency graph written out one edge at a time:
// every hard edge, then each soft edge the sort kept, which runs from a
// barrier member that sorted before the waiter to the waiter. The tests
// below hold the graph and plan APIs to it, so keeping each barrier
// whole can never change what those APIs report.
func expandedEdges(dfg *dataFlowGraph) [][]*Opcall {
	g := append([][]*Opcall(nil), dfg.hard...)
	pos := make(map[*Opcall]int, len(dfg.order))
	for i, node := range dfg.order {
		if _, ok := pos[node]; !ok {
			pos[node] = i
		}
	}
	waits := make(map[*Opcall][]*softBarrier)
	for _, bar := range dfg.soft {
		for _, a := range bar.waiters {
			waits[a] = append(waits[a], bar)
		}
	}
	done := make(map[*Opcall]bool, len(pos))
	for _, a := range dfg.order {
		if done[a] {
			continue
		}
		done[a] = true
		var kept []*Opcall
		seen := make(map[*Opcall]bool)
		for _, bar := range waits[a] {
			for _, b := range bar.members {
				if at, ok := pos[b]; ok && b != a && !seen[b] && at < pos[a] {
					seen[b] = true
					kept = append(kept, b)
				}
			}
		}
		sort.Slice(kept, func(i, j int) bool { return pos[kept[i]] < pos[kept[j]] })
		for _, b := range kept {
			g = append(g, []*Opcall{b, a})
		}
	}
	return g
}

// expandedGraph is the reference form of a DependencyGraph's edges,
// built from expandedEdges.
type expandedGraph struct {
	deps, dependents map[string][]string
}

func newExpandedGraph(dfg *dataFlowGraph) *expandedGraph {
	x := &expandedGraph{deps: map[string][]string{}, dependents: map[string][]string{}}
	seen := map[[2]string]bool{}
	for _, pair := range expandedEdges(dfg) {
		dep, dependent := pair[0].canonical.String(), pair[1].canonical.String()
		if seen[[2]string{dep, dependent}] {
			continue
		}
		seen[[2]string{dep, dependent}] = true
		x.deps[dependent] = append(x.deps[dependent], dep)
		x.dependents[dep] = append(x.dependents[dep], dependent)
	}
	for _, m := range []map[string][]string{x.deps, x.dependents} {
		for k := range m {
			sort.Strings(m[k])
		}
	}
	return x
}

// referencePlan schedules every node with every expanded dependency, as
// BuildEvalPlan did before barriers were kept whole, and returns each
// wave's paths in order, or nil when the scheduler refuses the graph.
func (x *expandedGraph) referencePlan(order []string) [][]string {
	s := parallel.NewScheduler()
	for _, path := range order {
		_ = s.AddTask(&parallel.Task{ID: path, Dependencies: append([]string(nil), x.deps[path]...)})
	}
	waves, err := s.Schedule()
	if err != nil {
		return nil
	}
	out := make([][]string, len(waves))
	for i, wave := range waves {
		for _, task := range wave {
			out[i] = append(out[i], task.ID)
		}
		sort.Strings(out[i])
	}
	return out
}

// referenceCheck is validateEvalPlan's per-phase check run over the
// expanded edges, in the order they were once listed.
func referenceCheck(dfg *dataFlowGraph, position map[string]int) error {
	for _, pair := range expandedEdges(dfg) {
		depPath := pair[0].canonical.String()
		dependentPath := pair[1].canonical.String()
		depPos, depKnown := position[depPath]
		dependentPos, dependentKnown := position[dependentPath]
		if !depKnown || !dependentKnown {
			continue
		}
		if depPos >= dependentPos {
			return fmt.Errorf("%w: %s (wave %d) is not scheduled strictly before its dependent %s (wave %d)",
				ErrInvalidEvalPlan, depPath, depPos, dependentPath, dependentPos)
		}
	}
	return nil
}

// planPaths returns each wave's paths in order.
func planPaths(waves []EvalWave) [][]string {
	if waves == nil {
		return nil
	}
	out := make([][]string, len(waves))
	for i, wave := range waves {
		for _, ref := range wave.Operators {
			out[i] = append(out[i], ref.Path)
		}
	}
	return out
}

// barrierCorpus returns documents whose operators wait on soft
// barriers: siblings that each wait on the others, chains, nested maps,
// subtree grabs, a cycle beside the barriers, and random mixes.
func barrierCorpus() []string {
	docs := []string{
		"aa: (( grab tgt[z] ))\ntgt:\n  x: (( concat \"computed\" \"-value\" ))\n  y: plain\nz: (( concat \"x\" \"\" ))\n",
		"key: (( concat \"a\" \"\" ))\nmeta:\n  a: 1\n  pick1: (( grab meta[key] ))\n  pick2: (( grab meta[key] ))\n",
		"k: (( concat \"x\" \"\" ))\nk2: (( concat \"p\" \"1\" ))\nmeta:\n  x: (( concat \"v\" \"1\" ))\n  p1: (( grab meta[k] ))\n  p2: (( grab meta[k2] ))\n",
		"meta:\n  a: (( concat \"v\" \"1\" ))\n  b: (( grab meta.a ))\nout: (( grab meta ))\nother: (( grab meta ))\n",
		"a: (( grab b ))\nb: (( grab a ))\nk: (( concat \"x\" \"\" ))\nm:\n  x: (( concat \"v\" \"\" ))\n  y: (( grab m[k] ))\nw: (( grab m[k] ))\nv: (( grab m ))\n",
		"x: (( grab x ))\nk: (( concat \"a\" \"\" ))\nm:\n  a: (( concat \"v\" \"\" ))\no: (( grab m[k] ))\n",
	}
	for seed := int64(1); seed <= 60; seed++ {
		docs = append(docs, randomBarrierDoc(seededRand(seed)))
	}
	return docs
}

// randomBarrierDoc returns a document of computed-key grabs, subtree
// grabs, and plain grabs over a map with a nested map inside it.
func randomBarrierDoc(rng *rand.Rand) string {
	n := 2 + rng.Intn(8)
	var b strings.Builder
	keys := 1 + rng.Intn(3)
	for i := 0; i < keys; i++ {
		fmt.Fprintf(&b, "k%d: (( concat \"a\" \"%d\" ))\n", i, rng.Intn(n))
	}
	b.WriteString("meta:\n")
	for i := 0; i < n; i++ {
		switch rng.Intn(4) {
		case 0:
			fmt.Fprintf(&b, "  a%d: %d\n", i, i)
		case 1:
			fmt.Fprintf(&b, "  a%d: (( grab meta[k%d] ))\n", i, rng.Intn(keys))
		case 2:
			fmt.Fprintf(&b, "  a%d: (( grab meta.sub.s%d ))\n", i, rng.Intn(3))
		default:
			fmt.Fprintf(&b, "  a%d: (( concat \"v\" \"%d\" ))\n", i, i)
		}
	}
	b.WriteString("  sub:\n")
	for i := 0; i < 3; i++ {
		if rng.Intn(2) == 0 {
			fmt.Fprintf(&b, "    s%d: (( concat \"s\" \"%d\" ))\n", i, i)
		} else {
			fmt.Fprintf(&b, "    s%d: (( grab meta[k%d] ))\n", i, rng.Intn(keys))
		}
	}
	b.WriteString("out:\n")
	for i := 0; i < 1+rng.Intn(8); i++ {
		switch rng.Intn(4) {
		case 0:
			fmt.Fprintf(&b, "  o%d: (( grab meta[k%d] ))\n", i, rng.Intn(keys))
		case 1:
			fmt.Fprintf(&b, "  o%d: (( grab meta.sub ))\n", i)
		case 2:
			fmt.Fprintf(&b, "  o%d: (( grab meta[k%d] || \"d\" ))\n", i, rng.Intn(keys))
		default:
			fmt.Fprintf(&b, "  o%d: (( grab meta.a%d ))\n", i, rng.Intn(n))
		}
	}
	return b.String()
}

// TestDependencyGraphMatchesExpandedEdges checks that every graph and
// plan API reports what the barriers mean written out edge by edge.
func TestDependencyGraphMatchesExpandedEdges(t *testing.T) {
	engine := sequentialTestEngine(t)
	withSoft, cyclicWithSoft := 0, 0
	for i, doc := range barrierCorpus() {
		data := parseYAMLForTest(t, doc)
		dfg, err := engine.createEvaluator(data).computeDataFlowGraph(EvalPhase)
		if err != nil {
			t.Fatalf("doc %d: %v", i, err)
		}
		x := newExpandedGraph(dfg)
		g := buildTestGraph(t, engine, doc)
		if len(g.waits) > 0 {
			withSoft++
			if dfg.sortErr != nil {
				cyclicWithSoft++
			}
		}

		paths := append([]string(nil), g.order...)
		for p := range x.dependents {
			paths = append(paths, p)
		}
		for _, p := range paths {
			if got, want := g.Dependencies(p), x.deps[p]; !slices.Equal(got, want) {
				t.Fatalf("doc %d: Dependencies(%s) = %v, want %v\n%s", i, p, got, want, doc)
			}
			if got, want := g.Dependents(p), x.dependents[p]; !slices.Equal(got, want) {
				t.Fatalf("doc %d: Dependents(%s) = %v, want %v\n%s", i, p, got, want, doc)
			}
		}

		ref := &DependencyGraph{phase: g.phase, order: g.order, nodes: g.nodes, deps: x.deps, dependents: x.dependents}
		if got, want := g.ToDOT(), ref.ToDOT(); got != want {
			t.Fatalf("doc %d: ToDOT differs\ngot:\n%s\nwant:\n%s", i, got, want)
		}
		if got, want := fmt.Sprint(g.DetectCycles()), fmt.Sprint(findCycles(x.dependents, g.order)); got != want {
			t.Fatalf("doc %d: DetectCycles = %s, want %s\n%s", i, got, want, doc)
		}
		if got, want := planPaths(BuildEvalPlan(g)), x.referencePlan(g.order); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("doc %d: BuildEvalPlan = %v, want %v\n%s", i, got, want, doc)
		}
	}
	if withSoft < 40 || cyclicWithSoft < 2 {
		t.Fatalf("%d documents kept soft edges and %d of those are cyclic, want at least 40 and 2", withSoft, cyclicWithSoft)
	}
}

// TestEvalPlanCheckMatchesExpandedEdges checks that validateEvalPlan's
// check accepts and rejects the same plans, with the same error, as a
// check over every expanded edge.
func TestEvalPlanCheckMatchesExpandedEdges(t *testing.T) {
	engine := sequentialTestEngine(t)
	rng := seededRand(7)
	rejected := 0
	for i, doc := range barrierCorpus() {
		data := parseYAMLForTest(t, doc)
		dfg, err := engine.createEvaluator(data).computeDataFlowGraph(EvalPhase)
		if err != nil {
			t.Fatalf("doc %d: %v", i, err)
		}
		g := buildTestGraph(t, engine, doc)
		plans := []map[string]int{positionOfWaves(BuildEvalPlan(g))}
		for j := 0; j < 20; j++ {
			position := map[string]int{}
			for _, p := range g.order {
				if rng.Intn(5) != 0 {
					position[p] = rng.Intn(4)
				}
			}
			plans = append(plans, position)
		}
		for j, position := range plans {
			got, want := checkPlanOrder(dfg, position), referenceCheck(dfg, position)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("doc %d plan %d: check = %v, want %v\n%s", i, j, got, want, doc)
			}
			if want != nil {
				rejected++
			}
		}
	}
	if rejected == 0 {
		t.Fatal("no plan was rejected, so the check was never exercised")
	}
}

// positionOfWaves maps each path in waves to its wave.
func positionOfWaves(waves []EvalWave) map[string]int {
	out := map[string]int{}
	for i, wave := range waves {
		for _, ref := range wave.Operators {
			out[ref.Path] = i
		}
	}
	return out
}

// computedKeyDoc returns n operators in one map and n grabs with a
// computed key into that map, so each grab waits on every operator in
// the map.
func computedKeyDoc(n int) string {
	var b strings.Builder
	b.WriteString("k: (( concat \"k\" \"1\" ))\nmeta:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  k%d: (( concat \"v\" \"%d\" ))\n", i, i)
	}
	b.WriteString("out:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  o%d: (( grab meta[k] ))\n", i)
	}
	return b.String()
}

// graphAPICost builds the graph and plan of computedKeyDoc(n), then
// evaluates it with EvaluateParallel against that plan, and returns the
// time and the bytes allocated.
func graphAPICost(t *testing.T, n int) (time.Duration, uint64) {
	t.Helper()
	engine := newParallelEngine(t)
	doc := NewDocument(parseYAMLForTest(t, computedKeyDoc(n)))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	g, err := engine.BuildDependencyGraph(doc, EvalPhase)
	if err != nil {
		t.Fatal(err)
	}
	waves := BuildEvalPlan(g)
	if len(waves) != 2 {
		t.Fatalf("plan has %d waves, want 2", len(waves))
	}
	if got := len(g.Dependencies("out.o0")); got != n+1 {
		t.Fatalf("out.o0 has %d dependencies, want %d", got, n+1)
	}
	got, err := engine.EvaluateParallel(context.Background(), doc, waves)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	if v, _ := got.Get("out.o0"); v != "v1" {
		t.Fatalf("out.o0 is %v, want v1", v)
	}
	return elapsed, after.TotalAlloc - before.TotalAlloc
}

// TestDependencyGraphSoftBarriersStayLinear checks that the graph and
// plan APIs keep each soft barrier whole. With n grabs that each wait on
// n operators, an edge for each pair makes the cost grow fourfold when n
// doubles, where a whole barrier makes it about double.
func TestDependencyGraphSoftBarriersStayLinear(t *testing.T) {
	smallTime, small := graphAPICost(t, 800)
	largeTime, large := graphAPICost(t, 1600)
	t.Logf("n=800: %v, %d MB; n=1600: %v, %d MB", smallTime, small>>20, largeTime, large>>20)
	if ratio := float64(large) / float64(small); ratio > 3 {
		t.Fatalf("doubling n multiplied the allocation by %.1f, want under 3", ratio)
	}
	if limit := uint64(1600) * 64 << 10; large > limit {
		t.Fatalf("n=1600 allocated %d MB, want under %d MB", large>>20, limit>>20)
	}
	if limit := largeGraphLimit(); largeTime > limit {
		t.Fatalf("n=1600 took %v, want under %v", largeTime, limit)
	}
}

// largeGraphLimit is the time the n=1600 graph, plan, and evaluation may
// take. The race detector slows the work several times over, so the
// bound under it only catches a return to a cost per pair.
func largeGraphLimit() time.Duration {
	if raceDetectorOn {
		return 10 * time.Second
	}
	return time.Second
}

// BenchmarkDependencyGraphComputedKeys measures the graph and plan APIs
// on 3200 grabs that each wait on the 3200 operators in one map.
func BenchmarkDependencyGraphComputedKeys(b *testing.B) {
	opts := defaultEngineOpts()
	engine := newEngineFromOptions(&opts)
	data := map[string]interface{}{}
	if err := yamlv3.Unmarshal([]byte(computedKeyDoc(3200)), &data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc := NewDocument(NormalizeMap(data))
		g, err := engine.BuildDependencyGraph(doc, EvalPhase)
		if err != nil {
			b.Fatal(err)
		}
		if waves := BuildEvalPlan(g); len(waves) != 2 {
			b.Fatalf("plan has %d waves, want 2", len(waves))
		}
	}
}
