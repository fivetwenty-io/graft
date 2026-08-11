package controlflow

import (
	"testing"
)

func TestExpand_ForLoop_BasicArrayIteration(t *testing.T) {
	src := `services:
  - api
  - worker
  - scheduler

deployments:
(( for svc in services ))
  - name: (( grab svc ))
    image: (( concat "myapp/" svc ":latest" ))
    replicas: 1
(( done ))
`
	data := runMergeYAML(t, src)
	deployments, ok := data["deployments"].([]interface{})
	if !ok || len(deployments) != 3 {
		t.Fatalf("deployments = %#v, want a 3-element list", data["deployments"])
	}
	want := []string{"api", "worker", "scheduler"}
	wantImage := []string{"myapp/api:latest", "myapp/worker:latest", "myapp/scheduler:latest"}
	for i, raw := range deployments {
		d := raw.(map[string]interface{})
		if d["name"] != want[i] {
			t.Errorf("deployments[%d].name = %v, want %v", i, d["name"], want[i])
		}
		if d["image"] != wantImage[i] {
			t.Errorf("deployments[%d].image = %v, want %v", i, d["image"], wantImage[i])
		}
	}

	if _, present := data["__graft_loop"]; present {
		t.Error("__graft_loop must be pruned from final output")
	}
}

func TestExpand_ForLoop_IterateWithIndex(t *testing.T) {
	src := `items:
  - alpha
  - beta
  - gamma

indexed:
(( for idx, item in items ))
  - index: (( grab idx ))
    value: (( grab item ))
(( done ))
`
	data := runMergeYAML(t, src)
	indexed := data["indexed"].([]interface{})
	if len(indexed) != 3 {
		t.Fatalf("indexed = %#v, want 3 entries", indexed)
	}
	wantVal := []string{"alpha", "beta", "gamma"}
	for i, raw := range indexed {
		d := raw.(map[string]interface{})
		if stringifyForCase(d["index"]) != stringifyForCase(i) {
			t.Errorf("indexed[%d].index = %v, want %d", i, d["index"], i)
		}
		if d["value"] != wantVal[i] {
			t.Errorf("indexed[%d].value = %v, want %v", i, d["value"], wantVal[i])
		}
	}
}

func TestExpand_ForLoop_IterateOverMap_TwoVars(t *testing.T) {
	src := `environment_vars:
  DATABASE_URL: postgres://localhost/myapp
  REDIS_URL: redis://localhost:6379
  LOG_LEVEL: info

env:
(( for key, value in environment_vars ))
  - name: (( grab key ))
    value: (( grab value ))
(( done ))
`
	data := runMergeYAML(t, src)
	env := data["env"].([]interface{})
	if len(env) != 3 {
		t.Fatalf("env = %#v, want 3 entries", env)
	}
	// Sorted key order (C-6/C-8): DATABASE_URL, LOG_LEVEL, REDIS_URL.
	wantNames := []string{"DATABASE_URL", "LOG_LEVEL", "REDIS_URL"}
	for i, raw := range env {
		d := raw.(map[string]interface{})
		if d["name"] != wantNames[i] {
			t.Errorf("env[%d].name = %v, want %v", i, d["name"], wantNames[i])
		}
	}
}

func TestExpand_ForLoop_SingleVarOverMap_BindsValue(t *testing.T) {
	// C-8: a single loop variable over a map binds the *value*, iterating
	// keys in sorted order.
	src := `counts:
  b: 2
  a: 1
  c: 3

totals:
(( for v in counts ))
  - (( grab v ))
(( done ))
`
	data := runMergeYAML(t, src)
	totals := data["totals"].([]interface{})
	if len(totals) != 3 {
		t.Fatalf("totals = %#v, want 3 entries", totals)
	}
	want := []string{"1", "2", "3"} // sorted by key a,b,c -> values 1,2,3
	for i, raw := range totals {
		if stringifyForCase(raw) != want[i] {
			t.Errorf("totals[%d] = %v, want %v", i, raw, want[i])
		}
	}
}

func TestExpand_ForLoop_LoopsWithConditionals(t *testing.T) {
	src := `services:
  - name: api
    public: true
    port: 8080
  - name: worker
    public: false
    port: 8081
  - name: admin
    public: true
    port: 8082

ingress:
  rules:
  (( for svc in services ))
    (( if svc.public ))
    - host: (( concat svc.name ".example.com" ))
      port: (( grab svc.port ))
    (( fi ))
  (( done ))
`
	data := runMergeYAML(t, src)
	ingress := data["ingress"].(map[string]interface{})
	rules := ingress["rules"].([]interface{})
	if len(rules) != 2 {
		t.Fatalf("rules = %#v, want 2 entries (only public services)", rules)
	}
	r0 := rules[0].(map[string]interface{})
	r1 := rules[1].(map[string]interface{})
	if r0["host"] != "api.example.com" || r1["host"] != "admin.example.com" {
		t.Errorf("rules hosts = %v, %v; want api.example.com, admin.example.com", r0["host"], r1["host"])
	}
}

func TestExpand_ForLoop_ShadowingAndNesting(t *testing.T) {
	// Outer loop variable "x" must remain visible (and correctly rewritten)
	// inside an inner loop that binds a different name, and the inner
	// loop's own variable must not leak to sibling outer iterations.
	src := `outers:
  - a
  - b
inners:
  - 1
  - 2

result:
(( for x in outers ))
  (( for y in inners ))
  - outer: (( grab x ))
    inner: (( grab y ))
  (( done ))
(( done ))
`
	data := runMergeYAML(t, src)
	result := data["result"].([]interface{})
	if len(result) != 4 {
		t.Fatalf("result = %#v, want 4 entries (2 outer x 2 inner)", result)
	}
	type pair struct{ outer, inner string }
	want := []pair{{"a", "1"}, {"a", "2"}, {"b", "1"}, {"b", "2"}}
	for i, raw := range result {
		d := raw.(map[string]interface{})
		got := pair{stringifyForCase(d["outer"]), stringifyForCase(d["inner"])}
		if got != want[i] {
			t.Errorf("result[%d] = %+v, want %+v", i, got, want[i])
		}
	}
}

func TestExpand_ForLoop_EmptyCollectionEmitsNothing(t *testing.T) {
	src := `items: []

out:
(( for x in items ))
  - (( grab x ))
(( done ))
placeholder: 1
`
	data := runMergeYAML(t, src)
	if v, present := data["out"]; present && v != nil {
		if lst, ok := v.([]interface{}); !ok || len(lst) != 0 {
			t.Errorf("out = %#v, want nil, an empty list, or absent", v)
		}
	}
}

func TestExpand_ForLoop_MappingKeyBody(t *testing.T) {
	// C-9: a for-body may emit mapping keys, not just list items — spliced
	// text is shape-agnostic. A single-element iterable keeps the emitted
	// document unambiguous (a body run more than once would repeat the same
	// static key, which is a document-authoring concern, not something this
	// test is about).
	src := `services:
  - api

(( for svc in services ))
service_name: (( grab svc ))
(( done ))
`
	data := runMergeYAML(t, src)
	if data["service_name"] != "api" {
		t.Errorf("service_name = %#v, want api", data["service_name"])
	}
}

// TestExpand_ForLoop_QuotedLiteralContainingVarName pins that the loop-
// variable rewrite skips quoted string literals. A body line whose literal
// text happens to contain the bound name (a URL, a hostname, a prefix) must
// come through verbatim — rewriting inside the quotes would splice the
// hidden __graft_loop path into user data.
func TestExpand_ForLoop_QuotedLiteralContainingVarName(t *testing.T) {
	src := `services:
  - api
  - worker
urls:
(( for name in services ))
- (( concat "https://name.example.com/" name ))
(( done ))
`
	data := runMergeYAML(t, src)
	urls, ok := data["urls"].([]interface{})
	if !ok || len(urls) != 2 {
		t.Fatalf("urls = %#v, want 2 entries", data["urls"])
	}
	want := []string{"https://name.example.com/api", "https://name.example.com/worker"}
	for i, w := range want {
		if urls[i] != w {
			t.Errorf("urls[%d] = %v, want %v", i, urls[i], w)
		}
	}
}

// TestExpand_ForLoop_SeparateFilesSameVarName pins that loops in different
// merged files do not overwrite each other's bindings. Each file is expanded
// by its own expander, so loop-instance identifiers have to be unique across
// files as well as within one: with a per-expander counter both files' first
// loop claims the same __graft_loop slot, and merging the second file over
// the first silently rewrites the first file's already-emitted references.
func TestExpand_ForLoop_SeparateFilesSameVarName(t *testing.T) {
	first := `first_src:
  - alpha
  - beta
first_out:
(( for item in first_src ))
- (( grab item ))
(( done ))
`
	second := `second_src:
  - gamma
  - delta
  - epsilon
second_out:
(( for item in second_src ))
- (( grab item ))
(( done ))
`
	data := runMergeYAMLFiles(t, first, second)

	firstOut, ok := data["first_out"].([]interface{})
	if !ok {
		t.Fatalf("first_out = %#v, want a list", data["first_out"])
	}
	if len(firstOut) != 2 || firstOut[0] != "alpha" || firstOut[1] != "beta" {
		t.Errorf("first_out = %#v, want [alpha beta]", firstOut)
	}

	secondOut, ok := data["second_out"].([]interface{})
	if !ok {
		t.Fatalf("second_out = %#v, want a list", data["second_out"])
	}
	if len(secondOut) != 3 || secondOut[0] != "gamma" || secondOut[2] != "epsilon" {
		t.Errorf("second_out = %#v, want [gamma delta epsilon]", secondOut)
	}
}
