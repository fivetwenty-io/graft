package graft_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// The texts a user can put in a wrapped value. If any of them is ever
// evaluated, its marker shows up in the output.
const (
	leakConcat = `(( concat "EVAL" "UATED" ))`
	leakGrab   = `(( grab some.path ))`
	leakVault  = `(( vault "secret/x:y" ))`
)

// leakMarkers are what the texts above turn into when they are evaluated.
var leakMarkers = []string{"EVALUATED", "GRABBED", "VAULTED"}

// countingVault stands in for Vault. Every path it is asked for is
// recorded, and it answers every request as not found, so a || fallback
// after a vault call takes the other side.
type countingVault struct {
	mu    sync.Mutex
	paths []string
}

func (v *countingVault) Name() string { return "vault" }

func (v *countingVault) Get(_ context.Context, path string) (interface{}, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.paths = append(v.paths, path)
	if strings.Contains(path, "secret/x") {
		return "VAULTED", nil
	}
	return nil, graft.ErrBackendNotFound
}

func (v *countingVault) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
	return graft.SequentialGetBatch(ctx, paths, v.Get)
}

func (v *countingVault) Health(context.Context) error { return nil }

func (v *countingVault) Close() error { return nil }

func (v *countingVault) asked(fragment string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, p := range v.paths {
		if strings.Contains(p, fragment) {
			return true
		}
	}
	return false
}

// leakParams is the wrapped value every leak case reads from.
func leakParams(t *testing.T) map[string]interface{} {
	t.Helper()
	dir := t.TempDir()
	loadFile := filepath.Join(dir, "load.yml")
	if err := os.WriteFile(loadFile, []byte("x: "+leakConcat+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	textFile := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(textFile, []byte(leakConcat), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]interface{}{
		"s":    leakConcat,
		"g":    leakGrab,
		"v":    leakVault,
		"l":    []interface{}{leakConcat},
		"m":    map[string]interface{}{"deep": leakConcat, "g": leakGrab, "v": leakVault},
		"k":    map[string]interface{}{leakConcat: 1},
		"b64":  base64.StdEncoding.EncodeToString([]byte(leakConcat)),
		"n":    5,
		"f":    false,
		"ip":   "10.0.0.1",
		"load": loadFile,
		"file": textFile,
	}
}

// leakModes are the two evaluators every leak case runs under.
var leakModes = []struct {
	name     string
	parallel bool
}{{"sequential", false}, {"parallel", true}}

// leakEngine builds an engine with a fresh counting Vault backend, so
// each case sees only the Vault requests it caused.
func leakEngine(t *testing.T, parallel bool) (graft.Engine, *countingVault) {
	t.Helper()
	v := &countingVault{}
	opts := []graft.EngineOption{graft.WithBackend(v), graft.WithBackendRegistry(true)}
	if parallel {
		opts = append(opts, graft.WithParallel(true))
	}
	eng, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return eng, v
}

// TestLiteralNeverLeaksThroughOperators runs every operator that can read
// a wrapped value, through a direct reference, a || fallback, and a
// nested call. Whatever the operator produces, no text from the wrapped
// value is evaluated, in the merge itself or in a second Evaluate of its
// result, and the Vault backend is never asked for the path in it.
func TestLiteralNeverLeaksThroughOperators(t *testing.T) {
	tests := []struct {
		name     string
		out      string
		contains string
	}{
		{name: "grab reference", out: "(( grab params.s ))", contains: leakConcat},
		{name: "grab or", out: "(( grab nope.x || params.s ))", contains: leakConcat},
		{name: "grab nested", out: `(( grab (concat "params." "s") ))`, contains: leakConcat},
		{name: "grab of grab text", out: "(( grab params.g ))", contains: leakGrab},
		{name: "grab of vault text", out: "(( grab params.v ))", contains: leakVault},
		{name: "grab or vault text", out: "(( grab nope.x || params.v ))", contains: leakVault},
		{name: "grab chain", out: "(( grab chain.v ))", contains: leakVault},
		{name: "grab chain of grab text", out: "(( grab chain.g ))", contains: leakGrab},
		{name: "inject reference", out: "\n  <<<: (( inject params.m ))", contains: leakVault},
		{name: "inject or", out: "\n  <<<: (( inject nope.x || params.m ))", contains: leakVault},
		{name: "inject or grab text", out: "\n  <<<: (( inject nope.x || params.m ))", contains: leakGrab},
		{name: "concat reference", out: `(( concat params.s "" ))`, contains: leakConcat},
		{name: "concat or", out: `(( concat nope.x || params.s "" ))`, contains: leakConcat},
		{name: "concat nested", out: `(( concat (grab params.s) "" ))`, contains: leakConcat},
		{name: "concat nested computed", out: `(( concat (grab (concat "params." "v")) "" ))`, contains: leakVault},
		{name: "join reference", out: `(( join "" params.l ))`, contains: leakConcat},
		{name: "join or", out: `(( join "" nope.x || params.l ))`, contains: leakConcat},
		{name: "join nested", out: `(( join "" (grab params.l) ))`, contains: leakConcat},
		{name: "keys reference", out: "(( keys params.k ))", contains: leakConcat},
		{name: "keys or", out: "(( keys nope.x || params.k ))", contains: leakConcat},
		{name: "keys nested", out: "(( keys (grab params.k) ))", contains: leakConcat},
		{name: "cartesian-product", out: "(( cartesian-product params.l ))", contains: leakConcat},
		{name: "cartesian-product or", out: "(( cartesian-product nope.x || params.l ))", contains: leakConcat},
		{name: "flatten", out: "(( flatten params.l ))", contains: leakConcat},
		{name: "uniq", out: "(( uniq params.l ))", contains: leakConcat},
		{name: "shuffle", out: "(( shuffle params.l ))", contains: leakConcat},
		{name: "split", out: `(( split "|" params.s ))`, contains: leakConcat},
		{name: "stringify", out: "(( stringify params.s ))", contains: "UATED"},
		{name: "stringify or", out: "(( stringify nope.x || params.s ))", contains: "UATED"},
		{name: "base64", out: "(( base64 params.s ))"},
		{name: "base64-decode", out: "(( base64-decode params.b64 ))", contains: leakConcat},
		{name: "base64-decode or", out: "(( base64-decode nope.x || params.b64 ))", contains: leakConcat},
		{name: "load", out: "(( load params.load ))", contains: leakConcat},
		{name: "file", out: "(( file params.file ))", contains: leakConcat},
		{name: "defer", out: "(( defer grab params.s ))"},
		{name: "calc", out: `(( calc "params.n + 1" ))`},
		{name: "ips", out: "(( ips params.ip 1 ))"},
		{name: "negate", out: "(( ! params.f ))"},
		{name: "type", out: "(( type params.s ))"},
		{name: "empty", out: `(( empty "map" ))`},
		{name: "ternary", out: `'(( params.n == 5 ? params.s : "no" ))'`, contains: leakConcat},
		{name: "bare or", out: "(( nope.x || params.s ))", contains: leakConcat},
		{name: "vault or", out: `(( vault "plan/x:y" || params.s ))`, contains: leakConcat},
		{name: "vault or vault text", out: `(( vault "plan/x:y" || params.v ))`, contains: leakVault},
		{name: "vault-try", out: `(( vault-try "plan/x:y" params.s ))`, contains: leakConcat},
	}
	for _, mode := range leakModes {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				eng, vault := leakEngine(t, mode.parallel)
				plan := "some:\n  path: GRABBED\nchain: (( grab params.m ))\nout: " + tt.out + "\n"
				planDoc, err := eng.ParseYAML([]byte(plan))
				if err != nil {
					t.Fatal(err)
				}
				user := graft.NewDocument(map[string]interface{}{"params": graft.NewLiteral(leakParams(t))})
				got, err := eng.Merge(context.Background(), planDoc, user).Execute()
				if err != nil {
					t.Fatalf("Execute: %v", err)
				}
				assertNoLeak(t, "Execute", got, tt.contains)

				again, err := eng.Evaluate(context.Background(), got)
				if err != nil {
					t.Fatalf("second Evaluate: %v", err)
				}
				assertNoLeak(t, "second Evaluate", again, tt.contains)

				if vault.asked("secret/x") {
					t.Fatal("the Vault backend was asked for a path inside a literal")
				}
			})
		}
	}
}

func assertNoLeak(t *testing.T, pass string, doc graft.Document, contains string) {
	t.Helper()
	// The plan's own some.path holds a marker, so only the keys that can
	// carry literal content are checked.
	shown := map[string]interface{}{}
	for _, key := range []string{"params", "chain", "out"} {
		if v, err := doc.Get(key); err == nil {
			shown[key] = v
		}
	}
	out, err := graft.MarshalYAML(shown)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range leakMarkers {
		if strings.Contains(string(out), marker) {
			t.Fatalf("%s evaluated literal text (found %s):\n%s", pass, marker, out)
		}
	}
	if contains == "" {
		return
	}
	value, err := doc.Get("out")
	if err != nil {
		t.Fatal(err)
	}
	text, err := graft.MarshalYAML(map[string]interface{}{"out": value})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), strings.Trim(contains, "()")) {
		t.Fatalf("%s: out does not hold the literal text %q:\n%s", pass, contains, text)
	}
}
