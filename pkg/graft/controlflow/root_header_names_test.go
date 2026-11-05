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
