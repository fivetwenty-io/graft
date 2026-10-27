package yq

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// resolveField reads field name of each document through one Resolver, as
// a run with a format that has no aliases does.
func resolveField(t *testing.T, src, name string) []*value.Value {
	t.Helper()
	res := New(Options{}).Resolver(nil)
	var out []*value.Value
	for _, doc := range mustLoad(t, src) {
		v, ok := doc.Value.Get(name)
		if !ok {
			v = doc.Value
		}
		got, err := res(doc.Value, v)
		if err != nil {
			t.Fatalf("resolving %q: %v", name, err)
		}
		out = append(out, got)
	}
	return out
}

func TestResolverAliasBeforeRedefinitionKeepsEarlierAnchor(t *testing.T) {
	src := "a: &x {k: 1}\n---\nb: *x\nc: &x {k: 2}\nd: *x\n"
	got := resolveField(t, src, "b")
	if v, _ := got[1].Get("k"); v == nil || v.Float() != 1 {
		t.Fatalf("b resolved to %v, want the anchor of the first document", got[1])
	}
	got = resolveField(t, src, "d")
	if v, _ := got[1].Get("k"); v == nil || v.Float() != 2 {
		t.Fatalf("d resolved to %v, want the redefinition", got[1])
	}
}

// resolveAllocs is the bytes allocated resolving n documents, each defining
// an anchor of its own and using the previous document's.
func resolveAllocs(t *testing.T, n int) uint64 {
	t.Helper()
	var sb strings.Builder
	for i := range n {
		if i > 0 {
			sb.WriteString("---\n")
		}
		fmt.Fprintf(&sb, "a: &x%d {k: %d}\nb: *x%d\n", i, i, max(i-1, 0))
	}
	docs := mustLoad(t, sb.String())
	res := New(Options{}).Resolver(nil)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for _, doc := range docs {
		if _, err := res(doc.Value, doc.Value); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestResolverCostGrowsLinearlyWithDistinctAnchors(t *testing.T) {
	small, large := resolveAllocs(t, 500), resolveAllocs(t, 2000)
	// Four times the documents costs about four times the bytes when each
	// document pays for its own anchors, and sixteen times when it pays
	// for every anchor before it.
	if ratio := float64(large) / float64(small); ratio > 8 {
		t.Fatalf("2000 documents allocated %d bytes against %d for 500, a ratio of %.1f, want under 8", large, small, ratio)
	}
}
