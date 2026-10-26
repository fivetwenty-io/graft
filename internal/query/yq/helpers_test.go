package yq

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/internal/query/yamlout"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func mustParse(t *testing.T, src string, g parser.Grammar) ast.Node {
	t.Helper()
	n, err := parser.Parse(src, g)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	return n
}

func wantEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func wantErrContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), sub)
	}
}

// mustLoad reads every document of a YAML stream with its presentation
// details, as yq loads its input.
func mustLoad(t *testing.T, src string) []load.Document {
	t.Helper()
	r := load.Open(load.Source{Name: "test", Reader: strings.NewReader(src)}, 0, load.LoadOptions{Format: load.FormatYAML, KeepMeta: true})
	var docs []load.Document
	for {
		d, err := r.Next()
		if errors.Is(err, io.EOF) {
			return docs
		}
		if err != nil {
			t.Fatalf("loading %q: %v", src, err)
		}
		docs = append(docs, d)
	}
}

// runYQ runs expr over the documents of in, one at a time or, with all
// set, together as eval-all does, and prints the outputs as yq does.
func runYQ(t *testing.T, opts Options, expr, in string, all bool) (string, error) {
	t.Helper()
	d := New(opts)
	docs := mustLoad(t, in)
	var buf bytes.Buffer
	enc := yamlout.New(&buf, yamlout.Options{Indent: 2, UnwrapScalar: true})
	emit := func(doc load.Document, v *value.Value) error { return enc.WriteDocument(doc, v) }
	var err error
	if all {
		err = EvalAll(d, expr, docs, emit)
	} else {
		for _, doc := range docs {
			if err = Eval(d, expr, doc, emit); err != nil {
				break
			}
		}
	}
	if ferr := enc.Flush(); err == nil {
		err = ferr
	}
	return buf.String(), err
}

func wantYQ(t *testing.T, expr, in, want string) {
	t.Helper()
	got, err := runYQ(t, Options{}, expr, in, false)
	if err != nil {
		t.Fatalf("%s on %q: %v", expr, in, err)
	}
	if got != want {
		t.Fatalf("%s on %q:\ngot  %q\nwant %q", expr, in, got, want)
	}
}

func wantYQAll(t *testing.T, expr, in, want string) {
	t.Helper()
	got, err := runYQ(t, Options{}, expr, in, true)
	if err != nil {
		t.Fatalf("eval-all %s on %q: %v", expr, in, err)
	}
	if got != want {
		t.Fatalf("eval-all %s on %q:\ngot  %q\nwant %q", expr, in, got, want)
	}
}

// wantYQError checks the message FormatError gives for the error that
// ends the run.
func wantYQError(t *testing.T, expr, in, want string) {
	t.Helper()
	_, err := runYQ(t, Options{}, expr, in, false)
	if err == nil {
		t.Fatalf("%s on %q: no error, want %q", expr, in, want)
	}
	wantEqual(t, New(Options{}).FormatError(err), want)
}
