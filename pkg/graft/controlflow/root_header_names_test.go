package controlflow

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/errtext"
	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// nestedHeaderDoc has a top-level x and a loop element that both lack
// the key "nope", so a "$.x.nope" header inside a loop over x fails the
// same way it would outside the loop.
const nestedHeaderDoc = `x:
  list: [t1, t2]
  kind: top
items:
- list: [e1]
  kind: elem
`

func runRootRefMergeErr(t *testing.T, opts []graft.EngineOption, src string) error {
	t.Helper()
	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		return err
	}
	_, err = engine.Merge(context.Background(), doc).Execute()
	return err
}

// TestRootReferenceHeaderErrorsNameTheWrittenPath pins that a header
// nested in a loop, whose "$." path cannot be resolved, reports the path
// as the user wrote it, in both its plain and its detailed text, and
// never names a key that graft made up.
func TestRootReferenceHeaderErrorsNameTheWrittenPath(t *testing.T) {
	cases := []struct{ name, body string }{
		{"for", "(( for y in $.x.nope ))\n- (( grab y ))\n(( done ))\n"},
		{"if", "(( if $.x.nope ))\n- t\n(( fi ))\n"},
		{"range", "(( for i in range 1 $.x.nope ))\n- (( grab i ))\n(( done ))\n"},
		{"case", "(( case $.x.nope ))\n(( when \"top\" ))\n- t\n(( esac ))\n"},
		{"if beside the bound path", "(( if $.x.nope == x.kind ))\n- t\n(( fi ))\n"},
		{"for beside the bound path", "(( for y in $.x.nope || x.nope ))\n- (( grab y ))\n(( done ))\n"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				src := nestedHeaderDoc + "out:\n(( for x in items ))\n" + tc.body + "(( done ))\n"
				err := runRootRefMergeErr(t, mode.opts, src)
				if err == nil {
					t.Fatal("merge succeeded, want an error")
				}
				for _, text := range []string{err.Error(), errtext.Detail(err)} {
					if strings.Contains(text, "__graft") {
						t.Fatalf("error names an internal key:\n%s", text)
					}
					if !strings.Contains(text, "x.nope") {
						t.Fatalf("error does not name the path x.nope:\n%s", text)
					}
				}
			})
		}
	}
}

// TestRootReferenceHeaderKeepsDocumentKeys pins that a document key
// spelled like graft's internal names still reads as itself in a header
// that also holds a "$." path naming a loop variable.
func TestRootReferenceHeaderKeepsDocumentKeys(t *testing.T) {
	const doc = nestedHeaderDoc + `__graft_root:
  k: [u1]
__graft_root_1:
  k: [u2]
`
	cases := []struct{ name, header, want string }{
		{"root path beside the document key", "$.x.nope || $.__graft_root.k", "[u1]"},
		{"bound path beside the document key", "$.x.nope || x.nope || $.__graft_root.k", "[u1]"},
		{"second document key", "$.x.nope || x.nope || $.__graft_root_1.k", "[u2]"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				src := doc + fmt.Sprintf("out:\n(( for x in items ))\n(( for v in %s ))\n- (( grab v ))\n(( done ))\n(( done ))\n", tc.header)
				data := runRootRefMerge(t, mode.opts, src)
				if got := fmt.Sprint(data["out"]); got != tc.want {
					t.Fatalf("out = %v, want %s", got, tc.want)
				}
			})
		}
	}
}

// TestRootReferenceHeaderTraceNamesTheWrittenPath pins that the trace of
// a nested header whose "$." path names the loop variable shows the
// path as written, as it does outside a loop.
func TestRootReferenceHeaderTraceNamesTheWrittenPath(t *testing.T) {
	debugOn, traceOn, writer := log.DebugOn, log.TraceOn, log.Writer
	defer func() { log.DebugOn, log.TraceOn, log.Writer = debugOn, traceOn, writer }()

	var buf bytes.Buffer
	opts := []graft.EngineOption{graft.WithTraceOutput(&buf), graft.WithTraceLevel(graft.TraceLevelDebug)}
	src := nestedHeaderDoc + "out:\n(( for x in items ))\n(( for y in $.x.list ))\n- (( grab y ))\n(( done ))\n(( done ))\n"
	data := runRootRefMerge(t, opts, src)
	if got := fmt.Sprint(data["out"]); got != "[t1 t2]" {
		t.Fatalf("out = %v, want [t1 t2]", got)
	}
	trace := buf.String()
	if !strings.Contains(trace, "resolving reference x.list") {
		t.Fatalf("trace does not show the reference x.list being resolved:\n%s", trace)
	}
	if strings.Contains(trace, "__graft_root") {
		t.Fatalf("trace names an internal key:\n%s", trace)
	}
}

// TestRootReferenceHeaderAliasIsNotReadable pins that a header cannot
// reach the prescan scope by naming a key that the document lacks. A
// header that reads a loop variable both with and without "$." needs an
// extra key for the document root, and a path the user wrote must never
// land on it.
func TestRootReferenceHeaderAliasIsNotReadable(t *testing.T) {
	for _, mode := range rootRefModes {
		t.Run(mode.name, func(t *testing.T) {
			src := nestedHeaderDoc + "out:\n(( for x in items ))\n(( for v in $.x.nope || x.nope || $.__graft_root.items ))\n- (( grab v ))\n(( done ))\n(( done ))\n"
			err := runRootRefMergeErr(t, mode.opts, src)
			if err == nil {
				t.Fatal("merge succeeded, want the missing key __graft_root to fail")
			}
			if !strings.Contains(err.Error(), "`$.__graft_root` could not be found") || strings.Contains(err.Error(), "__graft_root_") {
				t.Fatalf("error does not name the key the user wrote:\n%s", err)
			}
		})
	}
}

// TestRootReferenceHeaderErrorKeepsEchoedText pins that taking the root
// alias out of an error touches only the paths the engine rewrote. Here
// a document value holds the alias's name, and grab echoes the path it
// built from that value, which must reach the message as built.
func TestRootReferenceHeaderErrorKeepsEchoedText(t *testing.T) {
	for _, mode := range rootRefModes {
		t.Run(mode.name, func(t *testing.T) {
			src := nestedHeaderDoc + "note: __graft_root.x\nout:\n(( for x in items ))\n(( if $.x.kind != x.kind && (grab (concat note \".zz\")) ))\n- a\n(( fi ))\n(( done ))\n"
			err := runRootRefMergeErr(t, mode.opts, src)
			if err == nil {
				t.Fatal("merge succeeded, want the built path to fail")
			}
			if !strings.Contains(err.Error(), "`__graft_root.x.zz`") {
				t.Fatalf("error does not name the path grab built:\n%s", err)
			}
		})
	}
}

// TestWithoutAliasStripsOnlyRewrittenPaths pins the rule withoutAlias
// follows. It takes the alias out of a path in backticks that starts
// with the alias and goes on as a "$." path the engine rewrote, or as a
// leading part of one, and leaves every other mention alone.
func TestWithoutAliasStripsOnlyRewrittenPaths(t *testing.T) {
	v := view{alias: "__graft_root", rewritten: []string{"x.nope.deep", "x.kind"}}
	cases := []struct{ in, want string }{
		{"unable to resolve `__graft_root.x.nope.deep`: `$.__graft_root.x.nope` could not be found",
			"unable to resolve `x.nope.deep`: `$.x.nope` could not be found"},
		{"`$.__graft_root.x.kind` is not a number", "`$.x.kind` is not a number"},
		{"unable to resolve `__graft_root.x.zz`: `$.__graft_root.x.zz` could not be found",
			"unable to resolve `__graft_root.x.zz`: `$.__graft_root.x.zz` could not be found"},
		{"the value __graft_root.x.kind came from the data", "the value __graft_root.x.kind came from the data"},
		{"`__graft_root.x.kinder`", "`__graft_root.x.kinder`"},
		{"\x1b[31m`\x1b[0m\x1b[36m$.__graft_root.x.nope\x1b[0m\x1b[31m` could not be found",
			"\x1b[31m`\x1b[0m\x1b[36m$.x.nope\x1b[0m\x1b[31m` could not be found"},
	}
	for _, tc := range cases {
		got := withoutAlias(errtext.WithPlain(fmt.Errorf("%s", tc.in), tc.in), v)
		if got.Error() != tc.want {
			t.Errorf("withoutAlias(%q)\n got %q\nwant %q", tc.in, got.Error(), tc.want)
		}
		if d := errtext.Detail(got); d != tc.want {
			t.Errorf("detail of withoutAlias(%q)\n got %q\nwant %q", tc.in, d, tc.want)
		}
	}
}
