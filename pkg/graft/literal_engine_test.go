package graft

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// literalEngineModes are the evaluation paths every literal test runs
// on: the sequential evaluator, and the parallel scheduler that
// WithParallel(true) turns on.
func literalEngineModes(t *testing.T) []struct {
	name string
	eng  Engine
} {
	t.Helper()
	seq, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	par, err := NewEngine(WithParallel(true))
	if err != nil {
		t.Fatal(err)
	}
	return []struct {
		name string
		eng  Engine
	}{
		{name: "sequential", eng: seq},
		{name: "parallel", eng: par},
	}
}

func mustParse(t *testing.T, eng Engine, src string) Document {
	t.Helper()
	doc, err := eng.ParseYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// userDoc is a document whose params value the caller wraps, the shape
// a service broker builds from a provision request.
func userDoc(params interface{}) Document {
	return NewDocument(map[string]interface{}{"params": NewLiteral(params)})
}

func TestLiteralMergeReplacesWhole(t *testing.T) {
	tests := []struct {
		name string
		docs func(t *testing.T, eng Engine) []Document
		want interface{}
	}{
		{
			name: "nested map replaces a plan default whole",
			docs: func(t *testing.T, eng Engine) []Document {
				return []Document{
					mustParse(t, eng, "params:\n  a:\n    b: 1\n    c: 2\n  d: 3\n"),
					userDoc(map[string]interface{}{"a": map[string]interface{}{"x": 9}}),
				}
			},
			want: map[string]interface{}{"a": map[string]interface{}{"x": 9}},
		},
		{
			name: "plan map merged over a wrapped value replaces it whole",
			docs: func(t *testing.T, eng Engine) []Document {
				return []Document{
					userDoc(map[string]interface{}{"a": 1, "b": 2}),
					mustParse(t, eng, "params:\n  c: 3\n"),
				}
			},
			want: map[string]interface{}{"c": 3},
		},
		{
			name: "wrapped list of named maps replaces a plan list with no name merge",
			docs: func(t *testing.T, eng Engine) []Document {
				return []Document{
					mustParse(t, eng, "params:\n- name: a\n  v: 1\n- name: b\n  v: 2\n"),
					userDoc([]interface{}{map[string]interface{}{"name": "a", "w": 9}}),
				}
			},
			want: []interface{}{map[string]interface{}{"name": "a", "w": 9}},
		},
		{
			name: "append marker inside a wrapped list is text",
			docs: func(t *testing.T, eng Engine) []Document {
				return []Document{
					mustParse(t, eng, "params:\n- base\n"),
					userDoc([]interface{}{"(( append ))", "x"}),
				}
			},
			want: []interface{}{"(( append ))", "x"},
		},
		{
			name: "replace marker inside a wrapped list is text",
			docs: func(t *testing.T, eng Engine) []Document {
				return []Document{
					mustParse(t, eng, "params:\n- base\n"),
					userDoc([]interface{}{"(( replace ))", "y"}),
				}
			},
			want: []interface{}{"(( replace ))", "y"},
		},
		{
			name: "wrapped scalar replaces a plan map",
			docs: func(t *testing.T, eng Engine) []Document {
				return []Document{mustParse(t, eng, "params:\n  a: 1\n"), userDoc("s")}
			},
			want: "s",
		},
		{
			name: "wrapped nil replaces a plan map",
			docs: func(t *testing.T, eng Engine) []Document {
				return []Document{mustParse(t, eng, "params:\n  a: 1\n"), userDoc(nil)}
			},
			want: nil,
		},
		{
			name: "plan scalar replaces a wrapped map",
			docs: func(t *testing.T, eng Engine) []Document {
				return []Document{userDoc(map[string]interface{}{"a": 1}), mustParse(t, eng, "params: 7\n")}
			},
			want: 7,
		},
		{
			name: "wrapped map merged over a plan list of named maps elsewhere",
			docs: func(t *testing.T, eng Engine) []Document {
				return []Document{
					mustParse(t, eng, "jobs:\n- name: a\n  v: 1\nparams:\n  keep: false\n"),
					userDoc(map[string]interface{}{"jobs": []interface{}{map[string]interface{}{"name": "a", "v": 2}}}),
				}
			},
			want: map[string]interface{}{"jobs": []interface{}{map[string]interface{}{"name": "a", "v": 2}}},
		},
	}
	for _, mode := range literalEngineModes(t) {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				got, err := mode.eng.Merge(context.Background(), tt.docs(t, mode.eng)...).Execute()
				if err != nil {
					t.Fatalf("Execute: %v", err)
				}
				params, err := got.Get("params")
				if err != nil {
					t.Fatalf("Get params: %v", err)
				}
				if !reflect.DeepEqual(params, tt.want) {
					t.Fatalf("params = %#v, want %#v", params, tt.want)
				}
				if hasLiteral(got.RawData()) {
					t.Fatal("RawData returned a LiteralValue")
				}
			})
		}
	}
}

func TestLiteralIsNeverEvaluated(t *testing.T) {
	inner := map[string]interface{}{
		"vault":     `(( vault "secret/never:here" ))`,
		"grab":      "(( grab no.such.path ))",
		"malformed": "(( concat ",
		"unclosed":  "(( ",
		"deep": []interface{}{
			map[string]interface{}{"name": "x", "op": "(( awsparam \"/never\" ))"},
			"(( prune ))",
			"(( sort ))",
			"(( param \"never\" ))",
			"(( static_ips 0 1 ))",
		},
	}
	plan := "meta:\n  x: (( grab params.vault ))\nparams:\n  default: true\n"
	for _, mode := range literalEngineModes(t) {
		t.Run(mode.name, func(t *testing.T) {
			got, err := mode.eng.Merge(context.Background(), mustParse(t, mode.eng, plan), userDoc(inner)).Execute()
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			params, err := got.Get("params")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(params, inner) {
				t.Fatalf("params = %#v, want %#v", params, inner)
			}
			x, err := got.GetString("meta.x")
			if err != nil {
				t.Fatal(err)
			}
			if x != inner["vault"] {
				t.Fatalf("meta.x = %q, want the text it grabbed", x)
			}
		})
	}
}

func TestLiteralEvaluateAndEvaluateParallel(t *testing.T) {
	inner := map[string]interface{}{"op": `(( vault "secret/never:here" ))`, "bad": "(( "}
	data := func() map[string]interface{} {
		return map[string]interface{}{
			"params": NewLiteral(inner),
			"meta":   map[string]interface{}{"size": "(( grab params.op ))"},
		}
	}
	for _, mode := range literalEngineModes(t) {
		t.Run(mode.name+"/Evaluate", func(t *testing.T) {
			got, err := mode.eng.Evaluate(context.Background(), NewDocument(data()))
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if v, _ := got.Get("params"); !reflect.DeepEqual(v, inner) {
				t.Fatalf("params = %#v", v)
			}
		})
	}
	eng, err := NewEngine(WithParallel(true))
	if err != nil {
		t.Fatal(err)
	}
	de, ok := eng.(*DefaultEngine)
	if !ok {
		t.Fatal("NewEngine did not return a *DefaultEngine")
	}
	g, err := de.BuildDependencyGraph(NewDocument(data()), EvalPhase)
	if err != nil {
		t.Fatalf("BuildDependencyGraph: %v", err)
	}
	for _, n := range g.Nodes() {
		if n.Path != "meta.size" {
			t.Fatalf("dependency graph has an operator at %s, inside a literal", n.Path)
		}
	}
	got, err := de.EvaluateParallel(context.Background(), NewDocument(data()), BuildEvalPlan(g))
	if err != nil {
		t.Fatalf("EvaluateParallel: %v", err)
	}
	if v, _ := got.Get("params"); !reflect.DeepEqual(v, inner) {
		t.Fatalf("params = %#v", v)
	}
}

func TestLiteralSkipEvaluationAndPrune(t *testing.T) {
	eng, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	plan := mustParse(t, eng, "params:\n  a: 1\nother: 2\n")
	got, err := eng.Merge(context.Background(), plan, userDoc(map[string]interface{}{"b": "(( "})).SkipEvaluation().Execute()
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := got.Get("params"); !reflect.DeepEqual(v, map[string]interface{}{"b": "(( "}) {
		t.Fatalf("params = %#v", v)
	}

	got, err = eng.Merge(context.Background(), plan, userDoc(map[string]interface{}{"b": 1})).WithPrune("params").Execute()
	if err != nil {
		t.Fatal(err)
	}
	if got.Has("params") {
		t.Fatal("WithPrune left a wrapped value in place")
	}
}

// TestLiteralSharedEngineConcurrent runs many merges of wrapped
// documents on one engine at once, for the race detector.
func TestLiteralSharedEngineConcurrent(t *testing.T) {
	for _, mode := range literalEngineModes(t) {
		t.Run(mode.name, func(t *testing.T) {
			plan := "meta:\n  size: (( grab params.size ))\n  name: (( concat params.name \"-svc\" ))\nparams:\n  size: 1\n  name: d\n"
			shared := NewLiteral(map[string]interface{}{"shared": "(( vault \"secret/x:y\" ))"})
			var wg sync.WaitGroup
			errs := make(chan error, 32)
			for i := range 32 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					user := NewDocument(map[string]interface{}{
						"params": NewLiteral(map[string]interface{}{
							"size":   i,
							"name":   fmt.Sprintf("n%d", i),
							"shared": shared,
							"op":     "(( grab meta.size ))",
						}),
					})
					planDoc, err := mode.eng.ParseYAML([]byte(plan))
					if err != nil {
						errs <- err
						return
					}
					got, err := mode.eng.Merge(context.Background(), planDoc, user).Execute()
					if err != nil {
						errs <- err
						return
					}
					if size, err := got.GetInt("meta.size"); err != nil || size != i {
						errs <- fmt.Errorf("meta.size = %d, want %d: %w", size, i, err)
						return
					}
					if name, err := got.GetString("meta.name"); err != nil || name != fmt.Sprintf("n%d-svc", i) {
						errs <- fmt.Errorf("meta.name = %q: %w", name, err)
						return
					}
					if op, err := got.GetString("params.op"); err != nil || op != "(( grab meta.size ))" {
						errs <- fmt.Errorf("params.op = %q: %w", op, err)
						return
					}
					if hasLiteral(got.RawData()) {
						errs <- errors.New("RawData returned a LiteralValue")
					}
				}(i)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}
		})
	}
}

func TestLiteralCopiesKeepWrapper(t *testing.T) {
	newInner := func() map[string]interface{} {
		return map[string]interface{}{"a": map[string]interface{}{"b": 1}}
	}
	tests := []struct {
		name string
		copy func(v interface{}) interface{}
	}{
		{name: "Document.Clone", copy: func(v interface{}) interface{} {
			c := NewDocument(map[string]interface{}{"k": v}).Clone()
			return treeOf(c).(map[string]interface{})["k"]
		}},
		{name: "DeepCopy", copy: DeepCopy},
		{name: "DeepCopyMap", copy: func(v interface{}) interface{} {
			return DeepCopyMap(map[string]interface{}{"k": v})["k"]
		}},
		{name: "deepCopyValue", copy: func(v interface{}) interface{} {
			c, err := deepCopyValue(v)
			if err != nil {
				t.Fatal(err)
			}
			return c
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := newInner()
			got := tt.copy(NewLiteral(inner))
			lit, ok := got.(LiteralValue)
			if !ok {
				t.Fatalf("copy is %T, want a LiteralValue", got)
			}
			if !reflect.DeepEqual(lit.Value(), newInner()) {
				t.Fatalf("copy wraps %#v", lit.Value())
			}
			inner["a"].(map[string]interface{})["b"] = 2
			if !reflect.DeepEqual(lit.Value(), newInner()) {
				t.Fatal("the copy shares a map with the original")
			}
		})
	}

	eng, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	inner := newInner()
	got, err := eng.Merge(context.Background(), mustParse(t, eng, "params: {}\n"), userDoc(inner)).Execute()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := treeOf(got).(map[string]interface{})["params"].(LiteralValue); !ok {
		t.Fatal("the merged document lost the wrapper")
	}
	inner["a"].(map[string]interface{})["b"] = 2
	if v, _ := got.Get("params"); !reflect.DeepEqual(v, newInner()) {
		t.Fatalf("the merge result shares a map with its input: %#v", v)
	}
}

func TestLiteralCycleIsAnError(t *testing.T) {
	cyclic := map[string]interface{}{}
	cyclic["self"] = cyclic
	eng, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Merge(context.Background(), userDoc(cyclic)).Execute()
	if err == nil {
		t.Fatal("Execute accepted a literal that contains itself")
	}
}

func TestLiteralHistoryIsPlain(t *testing.T) {
	eng, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Merge(context.Background(),
		mustParse(t, eng, "params:\n  a: 1\n"),
		userDoc(map[string]interface{}{"b": NewLiteral(2)}),
	).TrackHistory().Execute()
	if err != nil {
		t.Fatal(err)
	}
	entries := got.History().Timeline()
	if len(entries) == 0 {
		t.Fatal("no history was recorded")
	}
	for _, e := range entries {
		if hasLiteral(e.OldValue) || hasLiteral(e.NewValue) {
			t.Fatalf("history entry for %s holds a LiteralValue", e.Path)
		}
	}
}
