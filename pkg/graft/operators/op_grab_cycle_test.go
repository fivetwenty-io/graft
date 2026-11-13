package operators_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// evaluateWithin evaluates src on an engine with the parallel evaluator
// on or off, and fails the test when evaluation takes longer than 10s,
// so a cycle that never settles fails instead of hanging the suite. A
// result that holds itself is returned as an error, because rendering
// it would recurse without end.
func evaluateWithin(t *testing.T, parallel bool, src string) (graft.Document, error) {
	t.Helper()
	engine, err := graft.NewEngine(graft.WithParallel(parallel))
	if err != nil {
		t.Fatal(err)
	}
	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		t.Fatalf("engine is %T, want *graft.DefaultEngine", engine)
	}
	if got := de.GetWorkerPool() != nil; got != parallel {
		t.Fatalf("worker pool present = %v, want %v", got, parallel)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		doc graft.Document
		err error
	}
	// Evaluate shares the deadline, so a regression that hangs inside
	// it stops there instead of running on after the test fails.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan result, 1)
	go func() {
		out, err := engine.Evaluate(ctx, doc)
		if err == nil {
			err = graft.CheckForCycles(out.RawData(), 64)
		}
		done <- result{out, err}
	}()
	select {
	case r := <-done:
		return r.doc, r.err
	case <-ctx.Done():
		t.Fatal("Evaluate did not finish within 10s")
		return nil, nil
	}
}

func TestGrabOfAnAncestorIsACycle(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"direct child", "q:\n  b: (( grab q ))\n", "$.q.b reads $.q"},
		{"deeper", "q:\n  c:\n    y: (( grab q ))\n", "$.q.c.y reads $.q"},
		{"two at once", "q:\n  b: (( grab q ))\n  c:\n    y: (( grab q ))\n", "reads $.q"},
		{"named item", "jobs:\n- name: api\n  self: (( grab jobs.api ))\n", "reads $.jobs.api"},
		{"indexed item", "jobs:\n- name: api\n  self: (( grab jobs.0 ))\n", "reads $.jobs.0"},
		{"predicate item", "jobs:\n- name: api\n  self: (( grab jobs[name=api] ))\n", "reads $.jobs.name=api"},
		{"second of two paths", "a: 1\nq:\n  b: (( grab a q ))\n", "$.q.b reads $.q"},
		{"computed path", "k: q\nq:\n  b: (( grab (( concat k \"\" )) ))\n", "$.q.b reads $.q"},
		{"nested in another call", "q:\n  b: (( concat \"x\" (( grab q )) ))\n", "$.q.b reads $.q"},
		{"bare reference after ||", "q:\n  c: (( nope || q ))\n", "$.q.c reads $.q"},
		{"nested grab after a bare ||", "q:\n  x: 1\n  c: (( nope || (( grab q )) ))\n", "$.q.c reads $.q"},
		{"bare reference on both sides of ||", "q:\n  c: (( q || q ))\n", "$.q.c reads $.q"},
		{"ternary branch", "q:\n  t: \"(( true ? q : 0 ))\"\n", "$.q.t reads $.q"},
		{"ternary branches, two edges", "q:\n  t: \"(( false ? 0 : q ))\"\n  c:\n    u: \"(( true ? q : 0 ))\"\n", "reads $.q"},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				_, err := evaluateWithin(t, parallel, tc.src)
				if err == nil || !strings.Contains(err.Error(), "cycle detected") || !strings.Contains(err.Error(), "which contains it") {
					t.Fatalf("err = %v, want a cycle error", err)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("err = %v, want it to say %q", err, tc.want)
				}
			})
		}
	}
}

func TestGrabOfASiblingStillWorks(t *testing.T) {
	src := "q:\n  a: 1\n  b: (( grab q.a ))\n" +
		"qa:\n  x: (( grab q ))\n  y: (( grab q.a ))\n" +
		"jobs:\n- name: api\n  peer: (( grab jobs.web.port ))\n- name: web\n  port: 80\n" +
		"r: (( grab q ))\n" +
		"f:\n  x: (( grab f || \"fallback\" ))\n"
	for _, parallel := range []bool{true, false} {
		out, err := evaluateWithin(t, parallel, src)
		if err != nil {
			t.Fatalf("parallel=%v: %v", parallel, err)
		}
		if v, _ := out.GetInt("q.b"); v != 1 {
			t.Errorf("parallel=%v: q.b = %d", parallel, v)
		}
		if v, _ := out.GetInt("qa.x.a"); v != 1 {
			t.Errorf("parallel=%v: qa.x.a = %d", parallel, v)
		}
		if v, _ := out.GetInt("qa.y"); v != 1 {
			t.Errorf("parallel=%v: qa.y = %d", parallel, v)
		}
		if v, _ := out.GetInt("jobs.0.peer"); v != 80 {
			t.Errorf("parallel=%v: jobs.0.peer = %d", parallel, v)
		}
		if v, _ := out.GetInt("r.b"); v != 1 {
			t.Errorf("parallel=%v: r.b = %d", parallel, v)
		}
		if v, _ := out.GetString("f.x"); v != "fallback" {
			t.Errorf("parallel=%v: f.x = %q", parallel, v)
		}
	}
}

func TestBareReferenceToAnAncestorFallsBack(t *testing.T) {
	cases := map[string]struct {
		src   string
		paths []string
	}{
		"one edge": {"q:\n  a: 1\n  c: (( q || \"fb\" ))\n", []string{"q.c"}},
		"two edges": {"q:\n  a: 1\n  b: (( q || \"fb\" ))\n  c:\n    y: (( q || \"fb\" ))\n",
			[]string{"q.b", "q.c.y"}},
	}
	for name, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", name, parallel), func(t *testing.T) {
				out, err := evaluateWithin(t, parallel, tc.src)
				if err != nil {
					t.Fatal(err)
				}
				for _, path := range tc.paths {
					if v, err := out.GetString(path); err != nil || v != "fb" {
						t.Errorf("%s = %q, %v, want fb", path, v, err)
					}
				}
				if v, _ := out.GetInt("q.a"); v != 1 {
					t.Errorf("q.a = %d, want 1", v)
				}
			})
		}
	}
}
