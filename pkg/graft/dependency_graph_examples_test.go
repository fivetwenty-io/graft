package graft_test

// Testable examples backing docs/architecture/evaluation.md's
// DependencyGraph/BuildEvalPlan/EvaluateParallel section (the C9a cluster
// of the graft library-API plan). Each pattern shown there has a
// compiling counterpart here (go test ./pkg/graft/ -run Example), so a
// doc snippet that stops compiling against the real API fails this suite
// instead of shipping silently wrong.

import (
	"context"
	"fmt"
	"sort"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// ExampleDefaultEngine_BuildDependencyGraph builds a dependency graph for
// a small document with a diamond-shaped dependency (two operators both
// depend on the same value, and a third depends on both) and inspects it.
func ExampleDefaultEngine_BuildDependencyGraph() {
	engine, err := graft.NewEngine()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		fmt.Println("not a *DefaultEngine")
		return
	}

	doc, err := engine.ParseYAML([]byte("base: seed\nleft: (( grab base ))\nright: (( grab base ))\njoined: (( concat left right ))\n"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	graphResult, err := de.BuildDependencyGraph(doc, graft.EvalPhase)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	deps := graphResult.Dependencies("joined")
	sort.Strings(deps)
	fmt.Println("joined depends on:", deps)

	cycles := graphResult.DetectCycles()
	fmt.Println("cycles:", len(cycles))
	// Output:
	// joined depends on: [left right]
	// cycles: 0
}

// ExampleBuildEvalPlan groups a DependencyGraph's operator calls into
// dependency-respecting waves. left and right have no dependency on each
// other, so they land in the same wave; joined depends on both, so it
// lands in a later one.
func ExampleBuildEvalPlan() {
	engine, err := graft.NewEngine()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		fmt.Println("not a *DefaultEngine")
		return
	}

	doc, err := engine.ParseYAML([]byte("base: seed\nleft: (( grab base ))\nright: (( grab base ))\njoined: (( concat left right ))\n"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	graphResult, err := de.BuildDependencyGraph(doc, graft.EvalPhase)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	waves := graft.BuildEvalPlan(graphResult)
	for i, wave := range waves {
		var paths []string
		for _, op := range wave.Operators {
			paths = append(paths, op.Path)
		}
		sort.Strings(paths)
		fmt.Printf("wave %d: %v\n", i, paths)
	}
	// Output:
	// wave 0: [left right]
	// wave 1: [joined]
}

// ExampleDefaultEngine_EvaluateParallel evaluates a document via the
// engine's real parallel execution path, using a plan built from
// BuildDependencyGraph/BuildEvalPlan. The engine must be configured with
// a WorkerPool (WithParallel(true) provisions a default one) - without
// one, EvaluateParallel returns ErrNoWorkerPool rather than silently
// evaluating sequentially.
func ExampleDefaultEngine_EvaluateParallel() {
	engine, err := graft.NewEngine(graft.WithParallel(true))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		fmt.Println("not a *DefaultEngine")
		return
	}

	doc, err := engine.ParseYAML([]byte("x: hello\ny: (( grab x ))\n"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	graphResult, err := de.BuildDependencyGraph(doc, graft.EvalPhase)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	waves := graft.BuildEvalPlan(graphResult)

	result, err := de.EvaluateParallel(context.Background(), doc, waves)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(result.String("y"))
	// Output:
	// hello
}
