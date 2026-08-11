package controlflow

import (
	"context"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers grab, concat, ==, &&, etc.
)

// runMergeYAML runs src through the full pipeline (control-flow expansion,
// YAML parse, merge, evaluate) via the same engine.ParseYAML entry point the
// CLI uses, and returns the final document's raw data.
func runMergeYAML(t *testing.T, src string) map[string]interface{} {
	t.Helper()
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if doc == nil {
		return map[string]interface{}{}
	}
	result, err := engine.Merge(context.Background(), doc).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %#v", result.RawData())
	}
	return data
}

// runMergeYAMLFiles is runMergeYAML for a multi-file merge, the shape every
// real graft invocation takes: each source is expanded and parsed on its own,
// then all of them are merged left to right.
func runMergeYAMLFiles(t *testing.T, srcs ...string) map[string]interface{} {
	t.Helper()
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	docs := make([]graft.Document, 0, len(srcs))
	for i, src := range srcs {
		doc, perr := engine.ParseYAML([]byte(src))
		if perr != nil {
			t.Fatalf("ParseYAML(srcs[%d]): %v", i, perr)
		}
		docs = append(docs, doc)
	}
	result, err := engine.Merge(context.Background(), docs...).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %#v", result.RawData())
	}
	return data
}

// runMergeYAMLErr is runMergeYAML's counterpart for tests that expect a
// pipeline error (ParseYAML or Execute).
func runMergeYAMLErr(t *testing.T, src string) error {
	t.Helper()
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		return err
	}
	if doc == nil {
		return nil
	}
	_, err = engine.Merge(context.Background(), doc).Execute()
	return err
}

func TestExpand_NoMarkers_ByteIdentical(t *testing.T) {
	srcs := []string{
		"a: 1\nb: (( grab a ))\n",
		"x: (( 1 + 2 ))\n",
		"",
		"just: text\nwith:\n  nested: [1, 2, 3]\n",
	}
	for _, src := range srcs {
		out, err := Expand([]byte(src))
		if err != nil {
			t.Fatalf("Expand(%q): unexpected error: %v", src, err)
		}
		if string(out) != src {
			t.Errorf("Expand(%q) = %q, want byte-identical passthrough", src, out)
		}
	}
}

func TestExpand_SimpleIfElse_TrueBranch(t *testing.T) {
	src := `environment: production

(( if environment == "production" ))
database:
  host: db.prod.example.com
  replicas: 3
  ssl: true
(( else ))
database:
  host: localhost
  replicas: 1
  ssl: false
(( fi ))
`
	data := runMergeYAML(t, src)
	db, ok := data["database"].(map[string]interface{})
	if !ok {
		t.Fatalf("database = %#v, want a map", data["database"])
	}
	if db["host"] != "db.prod.example.com" {
		t.Errorf("database.host = %v, want db.prod.example.com", db["host"])
	}
	if _, present := data["replicas"]; present {
		t.Error("top-level replicas should not exist; it's under database")
	}
}

func TestExpand_SimpleIfElse_FalseBranch(t *testing.T) {
	src := `environment: staging

(( if environment == "production" ))
database:
  host: db.prod.example.com
(( else ))
database:
  host: localhost
(( fi ))
`
	data := runMergeYAML(t, src)
	db := data["database"].(map[string]interface{})
	if db["host"] != "localhost" {
		t.Errorf("database.host = %v, want localhost", db["host"])
	}
}

func TestExpand_MultiBranchElif(t *testing.T) {
	src := `environment: staging

(( if environment == "production" ))
resources:
  replicas: 5
(( elif environment == "staging" ))
resources:
  replicas: 2
(( elif environment == "development" ))
resources:
  replicas: 1
(( else ))
resources:
  replicas: 1
(( fi ))
`
	data := runMergeYAML(t, src)
	res := data["resources"].(map[string]interface{})
	if v := stringifyForCase(res["replicas"]); v != "2" {
		t.Errorf("resources.replicas = %v, want 2", res["replicas"])
	}
}

func TestExpand_NoElse_NoMatchEmitsNothing(t *testing.T) {
	src := `features:
  metrics: false

(( if features.metrics ))
metrics:
  enabled: true
(( fi ))
`
	data := runMergeYAML(t, src)
	if _, present := data["metrics"]; present {
		t.Errorf("metrics block should be entirely absent, got %#v", data["metrics"])
	}
}

func TestExpand_NestedConditionals(t *testing.T) {
	src := `features:
  auth_enabled: true
  auth_type: oauth

(( if features.auth_enabled ))
auth:
  (( if features.auth_type == "oauth" ))
  provider: oauth2
  (( elif features.auth_type == "saml" ))
  provider: saml
  (( else ))
  provider: basic
  (( fi ))
(( fi ))
`
	data := runMergeYAML(t, src)
	auth := data["auth"].(map[string]interface{})
	if auth["provider"] != "oauth2" {
		t.Errorf("auth.provider = %v, want oauth2", auth["provider"])
	}
}

func TestExpand_BooleanExpressions_NegationAndOr(t *testing.T) {
	src := `is_public: false
requires_auth: true

(( if !is_public || requires_auth ))
security:
  enabled: true
(( fi ))
`
	data := runMergeYAML(t, src)
	sec, ok := data["security"].(map[string]interface{})
	if !ok || sec["enabled"] != true {
		t.Errorf("security = %#v, want {enabled: true}", data["security"])
	}
}

func TestExpand_CompoundConditions(t *testing.T) {
	src := `environment: production
region: us-east

(( if environment == "production" && region == "us-east" ))
database:
  primary: true
(( elif environment == "production" && region == "eu-west" ))
database:
  primary: false
(( else ))
database:
  primary: false
(( fi ))
`
	data := runMergeYAML(t, src)
	db := data["database"].(map[string]interface{})
	if db["primary"] != true {
		t.Errorf("database.primary = %v, want true", db["primary"])
	}
}

func TestExpand_UnclosedBlock_Errors(t *testing.T) {
	err := runMergeYAMLErr(t, "(( if a ))\nx: 1\n")
	if err == nil {
		t.Fatal("expected an error for an unclosed if block")
	}
}

// TestExpand_ConcurrentCallsAreIndependent pins that Expand carries no shared
// mutable state across calls. graft is usable as a library from several
// goroutines at once, and every expander field (the uid counter, the
// accumulated __graft_loop bindings) has to be per-call for the generated
// paths to stay stable.
func TestExpand_ConcurrentCallsAreIndependent(t *testing.T) {
	srcs := []string{
		"svcs: [a, b]\nout:\n(( for s in svcs ))\n- (( grab s ))\n(( done ))\n",
		"env: prod\n(( if env == \"prod\" ))\np: 1\n(( fi ))\n",
		"n: 2\nout:\n(( case n ))\n(( when 2 ))\nx: 1\n(( esac ))\n",
		"out:\n(( for i in range 1 20 ))\n- (( grab i ))\n(( done ))\n",
		"plain: doc\nno: controlflow\n",
	}
	want := make([]string, len(srcs))
	for i, s := range srcs {
		out, err := Expand([]byte(s))
		if err != nil {
			t.Fatalf("srcs[%d]: %v", i, err)
		}
		want[i] = string(out)
	}

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				for i, s := range srcs {
					out, err := Expand([]byte(s))
					if err != nil {
						t.Errorf("srcs[%d]: %v", i, err)
						return
					}
					if string(out) != want[i] {
						t.Errorf("srcs[%d]: concurrent expansion diverged\n got: %q\nwant: %q", i, string(out), want[i])
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}
