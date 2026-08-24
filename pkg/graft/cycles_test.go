package graft

import (
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// threeNodeCycle models a.yml's "meta.a: (( grab meta.b ))" and friends:
// each node grabs the next, so each node's dependency is the next one.
// Edges run dependency -> dependent, so dependents[meta.b] = [meta.a].
func threeNodeCycle() (map[string][]string, []string) {
	dependents := map[string][]string{
		"meta.a": {"meta.c"},
		"meta.b": {"meta.a"},
		"meta.c": {"meta.b"},
	}
	return dependents, []string{"meta.a", "meta.b", "meta.c"}
}

func TestExtractCycleReturnsReferenceOrderRotatedToSmallest(t *testing.T) {
	dependents, order := threeNodeCycle()

	got := extractCycle(dependents, order)

	// Reference order reads "this expression references that path", the
	// opposite of dependency order. Rotation then starts the chain at
	// the lexicographically smallest path.
	want := []string{"meta.a", "meta.b", "meta.c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractCycle() = %v, want %v", got, want)
	}
}

func TestExtractCycleTwoNodeStartsAtSmallestPath(t *testing.T) {
	// b.yml: meta.bar: (( grab meta.foo ))
	// a.yml: meta.foo: (( grab meta.bar ))
	dependents := map[string][]string{
		"meta.foo": {"meta.bar"},
		"meta.bar": {"meta.foo"},
	}
	order := []string{"meta.bar", "meta.foo"}

	got := extractCycle(dependents, order)

	want := []string{"meta.bar", "meta.foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractCycle() = %v, want %v (rotation picks the smallest path, not the first file)", got, want)
	}
}

func TestExtractCycleIsDeterministicAcrossRuns(t *testing.T) {
	dependents, order := threeNodeCycle()

	first := extractCycle(dependents, order)
	for i := 0; i < 20; i++ {
		got := extractCycle(dependents, order)
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d returned %v, first run returned %v", i, got, first)
		}
	}
}

func TestExtractCycleExcludesBlockedBystanders(t *testing.T) {
	// meta.foo and meta.bar form the cycle. bystander depends on
	// meta.foo, and deeper depends on bystander: both are blocked
	// forever by the cycle without being part of it. Reporting Kahn's
	// survivors would name all four.
	dependents := map[string][]string{
		"meta.foo":  {"meta.bar", "bystander"},
		"meta.bar":  {"meta.foo"},
		"bystander": {"deeper"},
	}
	order := []string{"bystander", "deeper", "meta.bar", "meta.foo"}

	got := extractCycle(dependents, order)

	want := []string{"meta.bar", "meta.foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractCycle() = %v, want %v", got, want)
	}
	for _, p := range got {
		if p == "bystander" || p == "deeper" {
			t.Errorf("extractCycle() named the innocent node %q", p)
		}
	}
}

func TestExtractCycleSelfReference(t *testing.T) {
	dependents := map[string][]string{"meta.a": {"meta.a"}}
	order := []string{"meta.a"}

	got := extractCycle(dependents, order)

	want := []string{"meta.a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractCycle() = %v, want %v", got, want)
	}
}

func TestExtractCycleAcyclicReturnsNil(t *testing.T) {
	dependents := map[string][]string{
		"meta.a": {"meta.b"},
		"meta.b": {"meta.c"},
	}
	order := []string{"meta.a", "meta.b", "meta.c"}

	if got := extractCycle(dependents, order); got != nil {
		t.Errorf("extractCycle() = %v, want nil for an acyclic graph", got)
	}
}

func TestDependentsFromEdgesDeduplicatesAndSorts(t *testing.T) {
	a := opcallAtPath(t, "meta.a")
	b := opcallAtPath(t, "meta.b")
	c := opcallAtPath(t, "meta.c")

	// The same (dependency, dependent) pair twice, plus a second edge
	// out of a, given out of sorted order.
	edges := [][]*Opcall{{a, c}, {a, b}, {a, b}}

	dependents, order := dependentsFromEdges(edges)

	if got := dependents["meta.a"]; !reflect.DeepEqual(got, []string{"meta.b", "meta.c"}) {
		t.Errorf("dependents[meta.a] = %v, want [meta.b meta.c] deduplicated and sorted", got)
	}
	if !reflect.DeepEqual(order, []string{"meta.a", "meta.b", "meta.c"}) {
		t.Errorf("order = %v, want a sorted node list", order)
	}
}

// opcallAtPath builds a minimal *Opcall positioned at path, for tests
// that exercise edge projection without running a real merge.
func opcallAtPath(t *testing.T, path string) *Opcall {
	t.Helper()
	cur, err := tree.ParseCursor(path)
	if err != nil {
		t.Fatalf("ParseCursor(%q) error = %v", path, err)
	}
	return &Opcall{where: cur, canonical: cur, src: "(( grab " + path + " ))"}
}
