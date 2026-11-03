package graft

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// readParams is the wrapped value every read test looks into.
func readParams() map[string]interface{} {
	return map[string]interface{}{
		"size":   "large",
		"name":   "db",
		"list":   []interface{}{"a", "b"},
		"nested": map[string]interface{}{"k": "v", "op": `(( vault "secret/never:here" ))`},
		"jobs":   []interface{}{map[string]interface{}{"name": "web", "port": 80}},
		"tmpl":   map[string]interface{}{"t1": "x", "t2": "(( grab never.here ))"},
		"secret": "do-not-print",
	}
}

func TestLiteralReadsSeeThroughWrapper(t *testing.T) {
	plan := `
meta:
  size: (( grab params.size ))
  port: (( grab params.jobs.web.port ))
  fallback: (( grab params.missing || "dflt" ))
  first: (( grab params.missing || params.name ))
  name: (( concat params.name "-svc" ))
  keys: (( keys params.nested ))
  joined: (( join "," params.list ))
  both: (( grab params.list params.list ))
  ternary: '(( params.size == "large" ? "big" : "small" ))'
  stringified: (( stringify params.list ))
injected:
  own: 1
  <<<: (( inject params.tmpl ))
`
	want := map[string]interface{}{
		"size":        "large",
		"port":        80,
		"fallback":    "dflt",
		"first":       "db",
		"name":        "db-svc",
		"keys":        []interface{}{"k", "op"},
		"joined":      "a,b",
		"both":        []interface{}{"a", "b", "a", "b"},
		"ternary":     "big",
		"stringified": "- a\n- b\n",
	}
	for _, mode := range literalEngineModes(t) {
		t.Run(mode.name, func(t *testing.T) {
			got, err := mode.eng.Merge(context.Background(), mustParse(t, mode.eng, plan), userDoc(readParams())).Execute()
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			meta, err := got.GetMap("meta")
			if err != nil {
				t.Fatal(err)
			}
			if keys, ok := meta["keys"].([]interface{}); ok {
				sort.Slice(keys, func(i, j int) bool { return keys[i].(string) < keys[j].(string) })
			}
			for k, w := range want {
				if !reflect.DeepEqual(meta[k], w) {
					t.Errorf("meta.%s = %#v, want %#v", k, meta[k], w)
				}
			}
			injected, err := got.GetMap("injected")
			if err != nil {
				t.Fatal(err)
			}
			wantInjected := map[string]interface{}{"own": 1, "t1": "x", "t2": "(( grab never.here ))"}
			if !reflect.DeepEqual(injected, wantInjected) {
				t.Errorf("injected = %#v, want %#v", injected, wantInjected)
			}
			for _, path := range []string{"params.nested.op", "params.jobs.web.port", "params.jobs.0.name", "params.list.1"} {
				if !got.Has(path) {
					t.Errorf("Has(%q) = false through a wrapper", path)
				}
			}
			if hasLiteral(got.RawData()) {
				t.Fatal("RawData returned a LiteralValue")
			}
		})
	}
}

func TestLiteralGrabbedValueStaysLiteral(t *testing.T) {
	plan := `
meta:
  whole: (( grab params ))
  inner: (( grab params.nested ))
  op: (( grab params.nested.op ))
  first: (( grab params.missing || params.tmpl ))
  computed: (( grab (concat "params." "nested") ))
  items: (( grab params.list params.list ))
injected:
  <<<: (( inject params.tmpl ))
injectedOr:
  <<<: (( inject params.missing || params.tmpl ))
injectedNested:
  <<<: (( inject (grab params.tmpl) ))
`
	wantLiteral := []string{
		"meta.whole", "meta.inner", "meta.op", "meta.first", "meta.computed", "meta.items.0", "meta.items.3",
		"injected.t2", "injectedOr.t1", "injectedOr.t2", "injectedNested.t1", "injectedNested.t2",
	}
	for _, mode := range literalEngineModes(t) {
		t.Run(mode.name, func(t *testing.T) {
			got, err := mode.eng.Merge(context.Background(), mustParse(t, mode.eng, plan), userDoc(readParams())).Execute()
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			root := treeOf(got)
			for _, path := range wantLiteral {
				c, err := tree.ParseCursor(path)
				if err != nil {
					t.Fatal(err)
				}
				v, err := c.Resolve(root)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				if _, ok := v.(LiteralValue); !ok {
					t.Errorf("%s is %T, want a LiteralValue", path, v)
				}
			}

			// A second pass over the result still leaves the copies alone.
			again, err := mode.eng.Evaluate(context.Background(), got)
			if err != nil {
				t.Fatalf("Evaluate of the merged result: %v", err)
			}
			if op, _ := again.GetString("meta.op"); op != `(( vault "secret/never:here" ))` {
				t.Fatalf("meta.op = %q after a second evaluation", op)
			}
			if t2, _ := again.GetString("injected.t2"); t2 != "(( grab never.here ))" {
				t.Fatalf("injected.t2 = %q after a second evaluation", t2)
			}
		})
	}
}

func TestLiteralReadErrorsDoNotQuoteContent(t *testing.T) {
	doc := NewDocument(map[string]interface{}{"params": NewLiteral(readParams())})
	_, err := doc.Get("params.secret.deeper")
	if err == nil {
		t.Fatal("Get through a scalar inside a literal succeeded")
	}
	if strings.Contains(err.Error(), "do-not-print") {
		t.Fatalf("error quotes literal content: %v", err)
	}
	var mismatch tree.TypeMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error is %T, want a tree.TypeMismatchError", err)
	}

	eng, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Merge(context.Background(), mustParse(t, eng, "x: (( grab params.secret.deeper ))\n"), userDoc(readParams())).Execute()
	if err == nil {
		t.Fatal("grab through a scalar inside a literal succeeded")
	}
	if strings.Contains(err.Error(), "do-not-print") {
		t.Fatalf("error quotes literal content: %v", err)
	}
}

func TestLiteralCursorResolve(t *testing.T) {
	root := map[string]interface{}{
		"p":    NewLiteral(map[string]interface{}{"a": map[string]interface{}{"b": 1}, "s": "x"}),
		"list": []interface{}{NewLiteral(map[string]interface{}{"name": "n1", "v": 2})},
		"own":  NewLiteral("whole"),
	}
	tests := []struct {
		path        string
		want        interface{}
		wantLiteral bool
	}{
		{path: "p.a.b", want: 1, wantLiteral: true},
		{path: "p.a", want: map[string]interface{}{"b": 1}, wantLiteral: true},
		{path: "list.n1.v", want: 2, wantLiteral: true},
		{path: "list.name=n1.v", want: 2, wantLiteral: true},
		{path: "list.0", want: map[string]interface{}{"name": "n1", "v": 2}, wantLiteral: true},
		{path: "own", want: "whole", wantLiteral: true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			c, err := tree.ParseCursor(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := c.Resolve(root)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !reflect.DeepEqual(plainValue(raw), tt.want) {
				t.Fatalf("Resolve = %#v, want %#v", raw, tt.want)
			}
			kept, err := c.ResolveLiteral(root)
			if err != nil {
				t.Fatalf("ResolveLiteral: %v", err)
			}
			l, isLiteral := kept.(LiteralValue)
			if isLiteral != tt.wantLiteral {
				t.Fatalf("ResolveLiteral = %T, want a LiteralValue %v", kept, tt.wantLiteral)
			}
			if isLiteral && !reflect.DeepEqual(plainValue(l.Value()), tt.want) {
				t.Fatalf("ResolveLiteral wraps %#v, want %#v", l.Value(), tt.want)
			}
			if _, err := c.Canonical(root); err != nil {
				t.Fatalf("Canonical: %v", err)
			}
			found, err := tree.Find(root, tt.path)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if hasLiteral(found) || !reflect.DeepEqual(found, tt.want) {
				t.Fatalf("Find = %#v, want plain %#v", found, tt.want)
			}
		})
	}

	globbed, err := (&tree.Cursor{Nodes: []string{"p", "*"}}).Glob(root)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(globbed) != 2 {
		t.Fatalf("Glob found %d paths inside the literal, want 2", len(globbed))
	}
}
