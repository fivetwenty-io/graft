package graft_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// codeProbe is in every text a literal below holds, and nowhere in the
// plan except the meta key the expression literals name. No error may
// quote it.
const codeProbe = "zq7"

// askedBackend stands in for an outside service. It records every path
// it is asked for and answers each one, so a test can tell whether a
// literal chose what the engine read.
type askedBackend struct {
	name  string
	mu    sync.Mutex
	paths []string
}

func (b *askedBackend) Name() string { return b.name }

func (b *askedBackend) Get(_ context.Context, path string) (interface{}, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.paths = append(b.paths, path)
	return "BACKENDREAD", nil
}

func (b *askedBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
	return graft.SequentialGetBatch(ctx, paths, b.Get)
}

func (b *askedBackend) Health(context.Context) error { return nil }

func (b *askedBackend) Close() error { return nil }

func (b *askedBackend) askedFor() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.paths...)
}

// codeEngine builds an engine whose vault, awsparam, awssecret, and nats
// operators read from askedBackends instead of the real services.
func codeEngine(t *testing.T, parallel bool) (graft.Engine, []*askedBackend) {
	t.Helper()
	backends := []*askedBackend{{name: "vault"}, {name: "awsparam"}, {name: "awssecret"}, {name: "nats"}}
	opts := []graft.EngineOption{graft.WithBackendRegistry(true)}
	for _, b := range backends {
		opts = append(opts, graft.WithBackend(b))
	}
	if parallel {
		opts = append(opts, graft.WithParallel(true))
	}
	eng, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return eng, backends
}

// codeParams is the literal every case reads from. Each text in it holds
// codeProbe.
func codeParams(t *testing.T) map[string]interface{} {
	t.Helper()
	dir := filepath.Join(t.TempDir(), codeProbe)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("FILEREAD"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "load.yml"), []byte("x: FILEREAD\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]interface{}{
		"e":    "meta.zq7secret * 2",
		"p":    "meta.zq7secret",
		"bad":  "zq7 ((( * ",
		"l":    []interface{}{"meta.zq7secret", " * 2"},
		"m":    map[string]interface{}{"e": "meta.zq7secret * 2", "p": "meta.zq7secret"},
		"n":    21,
		"ns":   "21",
		"nf":   2.5,
		"nsf":  " 2.5e1 ",
		"neg":  "-5",
		"f":    false,
		"v":    "secret/zq7:key",
		"vp":   "secret/zq7",
		"aws":  "zq7/param",
		"nats": "kv:zq7/key",
		"url":  "nats://127.0.0.1:1/zq7",
		"cfg":  map[string]interface{}{"url": "nats://127.0.0.1:1/zq7"},
		"file": file,
		"yml":  filepath.Join(dir, "load.yml"),
		"dir":  dir,
		"key":  "zq7secret",
		"pl":   []interface{}{"meta.zq7secret"},
		"km":   map[string]interface{}{"meta.zq7secret": 1},
		"b64":  "bWV0YS56cTdzZWNyZXQgKiAy",
		"ip":   "10.0.0.1",
	}
}

// codePlan holds the value an expression literal points at, and a copy
// of a wrapped map made by (( grab )). Each case's expression goes at out.
const codePlan = `meta:
  zq7secret: 21
  m: (( grab params.m ))
`

type codeCase struct {
	name  string
	out   string // the expression at out, or the lines under it
	extra string // more top-level plan lines
	want  string // the value out must have; "" means the call is refused
}

// TestLiteralIsNeverCode checks that a value from a literal is only ever
// data. Where an operator would read it as an expression, a path, or the
// name of something outside to read, the merge fails with an error that
// quotes none of the literal's text, and no backend is asked for
// anything. Where it is data, the operator uses it as data.
func TestLiteralIsNeverCode(t *testing.T) {
	runCodeCases(t, []codeCase{
		{name: "calc probe", out: "(( calc params.e ))"},
		{name: "calc path", out: "(( calc params.p ))"},
		{name: "calc malformed", out: "(( calc params.bad ))"},
		{name: "calc number", out: "(( calc params.n ))", want: "21"},
		{name: "calc numeric string", out: "(( calc params.ns ))", want: "21"},
		{name: "calc float", out: "(( calc params.nf ))", want: "2.5"},
		{name: "calc spaced exponent string", out: "(( calc params.nsf ))", want: "25"},
		{name: "calc negative string", out: "(( calc params.neg ))", want: "-5"},
		{name: "calc bool", out: "(( calc params.f ))"},
		{name: "calc or", out: "(( calc nope.x || params.e ))"},
		{name: "calc grabbed map", out: "(( calc meta.m.e ))"},
		{name: "calc through concat", out: `(( calc (concat params.e "") ))`},
		{name: "calc through join", out: `(( calc (join "" params.l) ))`},
		{name: "calc through base64", out: "(( calc (base64-decode (base64 params.e)) ))"},
		{name: "calc through stringify", out: "(( calc (stringify params.e) ))"},
		{name: "calc through plus", out: `(( calc params.p + " * 2" ))`},
		{name: "calc through ternary", out: `'(( calc (params.f ? "1" : params.e) ))'`},
		{name: "calc through the tree", extra: "t: (( concat params.e \"\" ))\n", out: "(( calc t ))"},
		{name: "calc through the tree twice", extra: "t: (( concat params.e \"\" ))\nu: (( concat t \"\" ))\n", out: "(( calc u ))"},
		{name: "calc reads a literal number", out: `(( calc "params.n * 2" ))`, want: "42"},
		{name: "refusal is not a fallback", out: `(( concat (calc params.e) || "x" "" ))`},
		{name: "plain calc unchanged", extra: "pe: meta.zq7secret * 2\n", out: "(( calc pe ))", want: "42"},
		{name: "bare or refusal is not a fallback", out: "(( (calc params.e) || meta.zq7secret ))"},
		{name: "inject refusal is not a fallback", out: "\n  <<<: (( inject (calc params.e) || meta.m ))"},
		{name: "raw_env refusal is not a fallback", out: `(( raw_env $GRAFT_C4_UNSET || (calc params.e) || "x" ))`},
		{name: "grab computed path", out: `(( grab (concat params.p "") ))`},
		{name: "grab computed key", out: `(( grab (concat "meta." params.key) ))`},
		{name: "grab computed or", out: `(( grab nope.x || (concat params.p "") ))`},
		{name: "grab computed refusal is not a fallback", out: `(( grab (concat params.p "") || meta.zq7secret ))`},
		{name: "grab computed through join", out: `(( grab (join "" params.l) ))`},
		{name: "grab computed malformed", out: `(( grab (concat params.bad "") ))`},
		{name: "grab reference is data", out: "(( grab params.p ))", want: "meta.zq7secret"},
		{name: "grab of grab is data", out: "(( grab (grab params.p) ))", want: "meta.zq7secret"},
		{name: "grab of grabbed map is data", out: "(( grab meta.m.p ))", want: "meta.zq7secret"},
		{name: "grab of the tree is data", extra: "t: (( concat params.p \"\" ))\n", out: "(( grab t ))", want: "meta.zq7secret"},
		{name: "grab bracket key", out: "(( grab meta[params.key] ))"},
		{name: "grab bracket key inside a literal map", out: "(( grab meta[params.m.p] ))"},
		{name: "plain grab bracket key unchanged", extra: "k: zq7secret\n", out: "(( grab meta[k] ))", want: "21"},
		{name: "plain grab computed unchanged", out: `(( grab (concat "meta." "zq7secret") ))`, want: "21"},
	})
}

// TestLiteralStaysLiteralThroughBuilders checks that every operator that
// builds a value from a literal's text gives back a value that is still
// a literal, so no chain of them turns the text into code or a name. Each
// builder is tried under an operator that would read its result as code
// or as a name, and once as plain data to show it still builds the value.
func TestLiteralStaysLiteralThroughBuilders(t *testing.T) {
	runCodeCases(t, []codeCase{
		{name: "split", out: `(( grab (join "." (split "." params.p)) ))`},
		{name: "flatten", out: `(( calc (join "" (flatten params.l)) ))`},
		{name: "uniq", out: `(( calc (join "" (uniq params.l)) ))`},
		{name: "shuffle", out: `(( grab (join "" (shuffle params.pl)) ))`},
		{name: "cartesian-product", out: `(( grab (join "" (cartesian-product params.pl)) ))`},
		{name: "keys", out: `(( grab (join "" (keys params.km)) ))`},
		{name: "base64-decode", out: "(( calc (base64-decode params.b64) ))"},
		{name: "stringify", out: "(( vault (stringify params.v) ))"},
		{name: "ips", out: `(( vault (concat "secret/" (ips params.ip 1) ":key") ))`},
		{name: "inject", extra: "t:\n  <<<: (( inject params.m ))\n", out: "(( calc t.e ))"},
		{name: "raw_env default", out: "(( calc (raw_env $GRAFT_C4_UNSET || params.e) ))"},
		{name: "or in the tree", extra: "t: (( nope.x || params.e ))\n", out: "(( calc t ))"},
		{name: "ternary in the tree", extra: "t: '(( params.f ? \"1\" : params.e ))'\n", out: "(( calc t ))"},
		{name: "concat into awsparam", out: `(( awsparam (concat "/" params.aws) ))`},
		{name: "join into nats", out: `(( nats (join "" params.pl) ))`},
		{name: "split is still data", out: `(( join "," (split "." params.p) ))`, want: "meta,zq7secret"},
		{name: "keys is still data", out: "(( join \"\" (keys params.km) ))", want: "meta.zq7secret"},
		{name: "base64-decode is still data", out: "(( base64-decode params.b64 ))", want: "meta.zq7secret * 2"},
		{name: "ips is still data", out: "(( ips params.ip 1 ))", want: "10.0.0.2"},
	})
}

// TestLiteralNeverNamesASecret checks that a literal never chooses which
// secret (( vault )) or (( vault-try )) reads, and that the backend is
// not asked for anything when the call is refused.
func TestLiteralNeverNamesASecret(t *testing.T) {
	runCodeCases(t, []codeCase{
		{name: "vault path", out: "(( vault params.v ))"},
		{name: "vault path pieces", out: `(( vault params.vp ":key" ))`},
		{name: "vault key segment", out: `(( vault "secret/" params.key ":key" ))`},
		{name: "vault through concat", out: `(( vault (concat params.vp ":key") ))`},
		{name: "vault through join", out: `(( vault (join "" params.l) ))`},
		{name: "vault through the tree", extra: "t: (( concat params.vp \":key\" ))\n", out: "(( vault t ))"},
		{name: "vault grabbed map", extra: "t: (( grab params.m ))\n", out: `(( vault t.p ":key" ))`},
		{name: "vault refusal is not a fallback", out: `(( vault params.v || "default" ))`},
		{name: "vault semicolon paths", out: `(( vault "secret/plain:key; " params.v ))`},
		{name: "vault-try path", out: `(( vault-try params.v "default" ))`},
		{name: "vault-try refusal is not skipped", out: `(( vault-try params.v "secret/plain:key" "default" ))`},
		{name: "vault-try through concat", out: `(( vault-try (concat params.vp ":key") "default" ))`},
		{name: "vault default is data", out: `(( vault nope.x || params.v ))`, want: "secret/zq7:key"},
		{name: "vault-try default is data", out: "(( vault-try nope.x params.v ))", want: "secret/zq7:key"},
		{name: "plain vault unchanged", out: `(( vault "secret/plain:key" ))`, want: "BACKENDREAD"},
		{name: "plain vault-try unchanged", out: `(( vault-try "secret/plain:key" "default" ))`, want: "BACKENDREAD"},
	})
}

// TestLiteralNeverNamesAnAWSValue checks that a literal never chooses
// which parameter or secret (( awsparam )) or (( awssecret )) reads.
func TestLiteralNeverNamesAnAWSValue(t *testing.T) {
	runCodeCases(t, []codeCase{
		{name: "awsparam key", out: "(( awsparam params.aws ))"},
		{name: "awsparam key piece", out: `(( awsparam "/" params.aws ))`},
		{name: "awsparam through concat", out: `(( awsparam (concat "/" params.aws) ))`},
		{name: "awsparam through the tree", extra: "t: (( concat params.aws \"\" ))\n", out: "(( awsparam t ))"},
		{name: "awssecret key", out: "(( awssecret params.aws ))"},
		{name: "awssecret subkey", out: `(( awssecret "plain/secret?key=" params.key ))`},
		{name: "awssecret through join", out: `(( awssecret (join "" params.l) ))`},
		{name: "awssecret refusal is not a fallback", out: `(( awssecret (concat params.aws "") || "default" ))`},
		{name: "plain awsparam unchanged", out: `(( awsparam "/plain/param" ))`, want: "BACKENDREAD"},
		{name: "plain awssecret unchanged", out: `(( awssecret "plain/secret" ))`, want: "BACKENDREAD"},
	})
}

// TestLiteralNeverNamesANATSValue checks that a literal never chooses
// which store or key (( nats )) reads, nor which server it asks.
func TestLiteralNeverNamesANATSValue(t *testing.T) {
	runCodeCases(t, []codeCase{
		{name: "nats path", out: "(( nats params.nats ))"},
		{name: "nats through concat", out: `(( nats (concat "kv:" params.vp) ))`},
		{name: "nats through the tree", extra: "t: (( concat params.nats \"\" ))\n", out: "(( nats t ))"},
		{name: "nats refusal is not a fallback", out: `(( nats params.nats || "default" ))`},
		{name: "nats server", out: `(( nats "kv:plain/key" params.url ))`},
		{name: "nats settings", out: `(( nats "kv:plain/key" params.cfg ))`},
		{name: "nats grabbed settings", extra: "t: (( grab params.cfg ))\n", out: `(( nats "kv:plain/key" t ))`},
		{name: "plain nats unchanged", out: `(( nats "kv:plain/key" ))`, want: "BACKENDREAD"},
	})
}

// TestLiteralNeverPicksANATSServer checks the built-in NATS client, with
// no backend registered, refuses a server from a literal before it dials.
func TestLiteralNeverPicksANATSServer(t *testing.T) {
	for _, mode := range leakModes {
		t.Run(mode.name, func(t *testing.T) {
			opts := []graft.EngineOption{}
			if mode.parallel {
				opts = append(opts, graft.WithParallel(true))
			}
			eng, err := graft.NewEngine(opts...)
			if err != nil {
				t.Fatal(err)
			}
			planDoc, err := eng.ParseYAML([]byte(codePlan + "out: (( nats \"kv:plain/key\" params.url ))\n"))
			if err != nil {
				t.Fatal(err)
			}
			user := graft.NewDocument(map[string]interface{}{"params": graft.NewLiteral(codeParams(t))})
			_, err = eng.Merge(context.Background(), planDoc, user).Execute()
			assertRefused(t, err)
		})
	}
}

// TestLiteralNeverNamesAFile checks that a literal never chooses which
// file (( file )) reads or which file or URL (( load )) reads.
func TestLiteralNeverNamesAFile(t *testing.T) {
	runCodeCases(t, []codeCase{
		{name: "file path", out: "(( file params.file ))"},
		{name: "file through concat", out: `(( file (concat params.dir "/file.txt") ))`},
		{name: "file through the tree", extra: "t: (( concat params.file \"\" ))\n", out: "(( file t ))"},
		{name: "file directory", out: `(( file params.dir "file.txt" ))`},
		{name: "file name", out: `(( file "/" params.file ))`},
		{name: "file refusal is not a fallback", out: `(( file params.file || "default" ))`},
		{name: "load path", out: "(( load params.yml ))"},
		{name: "load through concat", out: `(( load (concat params.dir "/load.yml") ))`},
		{name: "load refusal is not a fallback", out: `(( load params.yml || "default" ))`},
		{name: "load URL", out: "(( load params.url ))"},
	})
}

// staticsPlan gives (( static_ips )) one network to allocate from. The
// network name under the instance group is filled in by each case.
const staticsPlan = `networks:
- name: net1
  subnets:
  - static: [10.0.0.10 - 10.0.0.20]
    az: z1
instance_groups:
- name: ig
  instances: 1
  azs: [z1]
  networks:
  - name: %s
    static_ips: (( static_ips 0 ))
`

// TestLiteralNeverNamesANetwork checks that a literal never chooses
// which network (( static_ips )) allocates addresses from, while a plain
// network name allocates as before.
func TestLiteralNeverNamesANetwork(t *testing.T) {
	cases := []struct {
		name, network string
		refused       bool
	}{
		{name: "plain name", network: "net1"},
		{name: "plain name through grab", network: "(( grab meta.net ))"},
		{name: "literal name", network: "(( grab params.net ))", refused: true},
		{name: "literal name through concat", network: `(( concat params.net "" ))`, refused: true},
	}
	for _, mode := range leakModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				eng, _ := codeEngine(t, mode.parallel)
				plan := fmt.Sprintf(staticsPlan, tc.network) + "meta:\n  net: net1\n"
				planDoc, err := eng.ParseYAML([]byte(plan))
				if err != nil {
					t.Fatal(err)
				}
				params := map[string]interface{}{"net": "net1"}
				user := graft.NewDocument(map[string]interface{}{"params": graft.NewLiteral(params)})
				got, err := eng.Merge(context.Background(), planDoc, user).Execute()
				if tc.refused {
					assertRefused(t, err)
					return
				}
				if err != nil {
					t.Fatalf("Execute: %v", err)
				}
				ips, err := got.Get("instance_groups.ig.networks.0.static_ips")
				if err != nil {
					t.Fatal(err)
				}
				if s := fmt.Sprint(ips); s != "[10.0.0.10]" {
					t.Fatalf("static_ips is %s, want [10.0.0.10]", s)
				}
			})
		}
	}
}

func runCodeCases(t *testing.T, cases []codeCase) {
	t.Helper()
	for _, mode := range leakModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				eng, backends := codeEngine(t, mode.parallel)
				plan := codePlan + tc.extra + "out: " + tc.out + "\n"
				planDoc, err := eng.ParseYAML([]byte(plan))
				if err != nil {
					t.Fatal(err)
				}
				user := graft.NewDocument(map[string]interface{}{"params": graft.NewLiteral(codeParams(t))})
				got, err := eng.Merge(context.Background(), planDoc, user).Execute()
				for _, b := range backends {
					for _, p := range b.askedFor() {
						if strings.Contains(p, codeProbe) {
							t.Errorf("the %s backend was asked for %q, a name taken from a literal", b.name, p)
						}
					}
				}
				if tc.want == "" {
					assertRefused(t, err)
					return
				}
				if err != nil {
					t.Fatalf("Execute: %v", err)
				}
				out, err := got.Get("out")
				if err != nil {
					t.Fatal(err)
				}
				if s := fmt.Sprint(out); s != tc.want {
					t.Fatalf("out is %q, want %q", s, tc.want)
				}
			})
		}
	}
}

// assertRefused checks that err is the refusal of a value from a literal,
// that it names the path, and that no text it carries quotes the
// literal.
func assertRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Execute succeeded, but a value from a literal must never be read as code or as a name")
	}
	for _, text := range []string{err.Error(), graft.DetailedError(err), fmt.Sprintf("%+v", err)} {
		if strings.Contains(text, codeProbe) {
			t.Fatalf("the error quotes literal text: %q", text)
		}
	}
	assertChainPlain(t, err, codeProbe)
	if !errors.Is(err, graft.ErrLiteralRefused) {
		t.Fatalf("errors.Is(err, graft.ErrLiteralRefused) is false for %v", err)
	}
	if !strings.Contains(err.Error(), "cannot use a value from a literal as") {
		t.Fatalf("the error is not the literal refusal: %v", err)
	}
	var pathErr *graft.PathError
	if !errors.As(err, &pathErr) && !strings.Contains(err.Error(), "$.") {
		t.Fatalf("the error does not name a path: %v", err)
	}
}

// TestRefusalIsNeverWithheld checks that a refusal keeps its message
// when a literal happens to hold a word the message uses, here the
// operator's own name, since the refusal quotes nothing from the value.
func TestRefusalIsNeverWithheld(t *testing.T) {
	params := map[string]interface{}{"calc": "zq7", "literal": "zq7", "e": "meta.zq7secret * 2"}
	for _, mode := range leakModes {
		t.Run(mode.name, func(t *testing.T) {
			eng, _ := codeEngine(t, mode.parallel)
			planDoc, err := eng.ParseYAML([]byte(codePlan + "out: (( calc params.e ))\n"))
			if err != nil {
				t.Fatal(err)
			}
			user := graft.NewDocument(map[string]interface{}{"params": graft.NewLiteral(params)})
			_, err = eng.Merge(context.Background(), planDoc, user).Execute()
			assertRefused(t, err)
		})
	}
}

// TestErrLiteralRefusedThroughWrappers checks that errors.Is finds
// graft.ErrLiteralRefused through every wrapper an engine puts around a
// refusal: the error list a merge with several failures returns, and the
// partial-evaluation error that Evaluate returns.
func TestErrLiteralRefusedThroughWrappers(t *testing.T) {
	const plan = codePlan + "out: (( calc params.e ))\nalso: (( vault params.v ))\n"
	for _, mode := range leakModes {
		t.Run(mode.name+"/merge", func(t *testing.T) {
			eng, _ := codeEngine(t, mode.parallel)
			planDoc, err := eng.ParseYAML([]byte(plan))
			if err != nil {
				t.Fatal(err)
			}
			user := graft.NewDocument(map[string]interface{}{"params": graft.NewLiteral(codeParams(t))})
			_, err = eng.Merge(context.Background(), planDoc, user).Execute()
			var multi graft.MultiError
			if !errors.As(err, &multi) {
				t.Fatalf("the error holds no graft.MultiError: %T %v", err, err)
			}
			if multi.Count() != 2 {
				t.Fatalf("the error lists %d failures, want 2: %v", multi.Count(), err)
			}
			for _, each := range multi.Errors {
				if !errors.Is(each, graft.ErrLiteralRefused) {
					t.Fatalf("errors.Is(each, graft.ErrLiteralRefused) is false for %v", each)
				}
			}
			assertRefused(t, err)
		})
		t.Run(mode.name+"/evaluate", func(t *testing.T) {
			eng, _ := codeEngine(t, mode.parallel)
			doc := graft.NewDocument(map[string]interface{}{
				"params": graft.NewLiteral(codeParams(t)),
				"meta":   map[string]interface{}{"zq7secret": 21},
				"out":    "(( calc params.e ))",
			})
			_, err := eng.Evaluate(context.Background(), doc)
			var partial *graft.PartialEvaluationError
			if !errors.As(err, &partial) {
				t.Fatalf("the error is not a *graft.PartialEvaluationError: %T %v", err, err)
			}
			if !errors.Is(err, graft.ErrLiteralRefused) {
				t.Fatalf("errors.Is(err, graft.ErrLiteralRefused) is false for %v", err)
			}
			assertRefused(t, err)
		})
	}
}

// TestLiteralTypedContainerIsNeverCode checks that the taint of a
// literal reaches the elements of a typed container inside it. A
// []string wrapped by NewLiteral holds an expression, and calc on that
// element's text refuses it without quoting it. A reference path does
// not index into a typed slice, for a literal or any other value, so
// the plans reach the element through the list, through join, and
// through grab.
func TestLiteralTypedContainerIsNeverCode(t *testing.T) {
	plans := []struct{ name, plan string }{
		{"calc on the list", "out: (( calc params.list ))\n"},
		{"calc on the joined element", "x: (( join \"\" params.list ))\nout: (( calc x ))\n"},
		{"calc on an inline join", "out: (( calc (join \"\" params.list) ))\n"},
		{"calc on an inline grab", "out: (( calc (grab params.list) ))\n"},
		{"calc on a top-level list", "x: (( join \"\" params.top ))\nout: (( calc x ))\n"},
	}
	for _, mode := range leakModes {
		for _, p := range plans {
			t.Run(mode.name+"/"+p.name, func(t *testing.T) {
				eng, _ := codeEngine(t, mode.parallel)
				planDoc, err := eng.ParseYAML([]byte("meta:\n  zq7secret: 21\n" + p.plan))
				if err != nil {
					t.Fatal(err)
				}
				user := graft.NewDocument(map[string]interface{}{
					"params": map[string]interface{}{
						"list": graft.NewLiteral([]string{"meta.zq7secret * 2"}),
						"top":  graft.NewLiteral([]string{"meta.zq7secret", " * 2"}),
					},
				})
				_, err = eng.Merge(context.Background(), planDoc, user).Execute()
				assertRefused(t, err)
			})
		}
	}
}
