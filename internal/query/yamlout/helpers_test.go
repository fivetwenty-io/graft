package yamlout

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func wantEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func lit(s string) *value.Value { return value.NumLit(decimal.MustParse(s), s) }

func dec(s string) *value.Value { return value.NumDec(decimal.MustParse(s)) }

func pair(k string, v *value.Value) value.Pair { return value.Pair{Key: k, Value: v} }

func arr(vs ...*value.Value) *value.Value { return value.Arr(vs...) }

func obj(ps ...value.Pair) *value.Value { return value.Obj(ps...) }

// sample is {"b":1,"a":{"10":1,"2":2,"x":[1,{"y":null}]},"e":[],"o":{}}.
func sample() *value.Value {
	return obj(
		pair("b", lit("1")),
		pair("a", obj(
			pair("10", lit("1")),
			pair("2", lit("2")),
			pair("x", arr(lit("1"), obj(pair("y", value.Null())))),
		)),
		pair("e", arr()),
		pair("o", obj()),
	)
}

func emitAll(t *testing.T, vs []*value.Value, o Options) string {
	t.Helper()
	var buf bytes.Buffer
	e := New(&buf, o)
	for _, v := range vs {
		if err := e.Write(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func emitOne(t *testing.T, v *value.Value, o Options) string {
	t.Helper()
	return emitAll(t, []*value.Value{v}, o)
}

// globTestdata returns the testdata files that match pattern, failing when
// there are none.
func globTestdata(t *testing.T, pattern string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", pattern))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no testdata matches %s", pattern)
	}
	return files
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// mustLoad reads every YAML document in text.
func mustLoad(t *testing.T, name, text string, opts load.LoadOptions) []load.Document {
	t.Helper()
	opts.Format = load.FormatYAML
	r := load.Open(load.Source{Name: name, Reader: strings.NewReader(text)}, 0, opts)
	var docs []load.Document
	for {
		d, err := r.Next()
		if errors.Is(err, io.EOF) {
			return docs
		}
		if err != nil {
			t.Fatalf("loading %s: %v", name, err)
		}
		docs = append(docs, d)
	}
}

func mustLoadFile(t *testing.T, path string, opts load.LoadOptions) []load.Document {
	t.Helper()
	return mustLoad(t, path, mustReadFile(t, path), opts)
}

func mustLoadOne(t *testing.T, text string, opts load.LoadOptions) *value.Value {
	t.Helper()
	docs := mustLoad(t, "inline", text, opts)
	if len(docs) != 1 {
		t.Fatalf("want one document in %q, got %d", text, len(docs))
	}
	return docs[0].Value
}

// emitDocs writes each document's value through WriteDocument, so leading
// content and separators come out the way yq prints them.
func emitDocs(t *testing.T, docs []load.Document, o Options) string {
	t.Helper()
	var buf bytes.Buffer
	e := New(&buf, o)
	for _, d := range docs {
		if err := e.WriteDocument(d, d.Value); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// withComments returns v carrying the three comments.
func withComments(v *value.Value, head, line, foot string) *value.Value {
	return v.WithMeta(&value.Meta{HeadComment: head, LineComment: line, FootComment: foot})
}
