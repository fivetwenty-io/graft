package yaml_test

import (
	"bytes"
	"errors"
	"testing"

	goyaml "github.com/goccy/go-yaml"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func encodeAll(t *testing.T, opts *yaml.EncodeOptions, vs ...any) string {
	t.Helper()
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	if opts != nil {
		e = yaml.NewEncoderWith(&b, *opts)
	}
	for _, v := range vs {
		if err := e.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	before := b.Len()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if b.Len() != before {
		t.Fatalf("Close wrote %q", b.String()[before:])
	}
	return b.String()
}

func goccyAll(t *testing.T, opts []goyaml.EncodeOption, vs ...any) string {
	t.Helper()
	var b bytes.Buffer
	e := goyaml.NewEncoder(&b, opts...)
	for _, v := range vs {
		if err := e.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestEncoderJoinsDocumentsAsGoccyDoes(t *testing.T) {
	t.Parallel()
	streams := map[string][]any{
		"none":       nil,
		"one":        {layoutDoc()},
		"two":        {layoutDoc(), layoutDoc()},
		"scalars":    {"s", 1, []any{}},
		"empty maps": {map[string]any{}, map[string]any{}},
		"nulls":      {nil, nil},
	}
	for name, vs := range streams {
		got := encodeAll(t, nil, vs...)
		if want := goccyAll(t, []goyaml.EncodeOption{goyaml.Indent(2)}, vs...); got != want {
			t.Errorf("%s: got %q, goccy wrote %q", name, got, want)
		}
	}
	if got := encodeAll(t, nil, "s", 1, []any{}); got != "s\n---\n1\n---\n[]\n" {
		t.Errorf("scalars: got %q", got)
	}
	opts := yaml.EncodeOptions{Indent: 4, IndentSequence: true}
	got := encodeAll(t, &opts, layoutDoc(), layoutNested())
	want := goccyAll(t, []goyaml.EncodeOption{goyaml.Indent(4), goyaml.IndentSequence(true)}, layoutDoc(), layoutNested())
	if got != want {
		t.Errorf("indent 4: got %q, goccy wrote %q", got, want)
	}
}

func TestEncoderOneDocumentIsMarshal(t *testing.T) {
	t.Parallel()
	for _, v := range []any{layoutDoc(), "s", nil, []any{}, yaml.MapSlice{{Key: "b", Value: 1}, {Key: "a", Value: 2}}} {
		opts := yaml.EncodeOptions{Indent: 4}
		want, err := yaml.MarshalWith(v, opts)
		if err != nil {
			t.Fatal(err)
		}
		if got := encodeAll(t, &opts, v); got != string(want) {
			t.Errorf("got %q, Marshal gave %q", got, want)
		}
	}
}

func TestEncoderFailedEncodeWritesNothing(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	if err := e.Encode(make(chan int)); err == nil || b.Len() != 0 {
		t.Fatalf("got %v and %q, want an error and no output", err, b.String())
	}
	if err := e.Encode(map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := e.Encode(make(chan int)); err == nil {
		t.Fatal("got no error")
	}
	if err := e.Encode(map[string]any{"b": 2}); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "a: 1\n---\nb: 2\n" {
		t.Errorf("got %q", got)
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestEncoderReportsWriterAndCloseErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	if err := yaml.NewEncoder(failingWriter{boom}).Encode("x"); !errors.Is(err, boom) {
		t.Errorf("got %v, want the writer's error", err)
	}
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Errorf("a second Close: %v", err)
	}
	if err := e.Encode("x"); err == nil || err.Error() != "yaml: Encode called after Close" || b.Len() != 0 {
		t.Errorf("Encode after Close: got %v and %q", err, b.String())
	}
	if err := yaml.NewEncoderWith(&b, yaml.EncodeOptions{Indent: 1}).Encode("x"); err == nil {
		t.Error("indent 1: got no error")
	}
}

func TestEncoderStreamIsMarshalPlusSeparators(t *testing.T) {
	values := []any{map[string]any{"a": 1, "l": []any{"x"}}, "s", nil, []any{1, "x\ny"}, yaml.MapSlice{{Key: "b", Value: 1}, {Key: "a", Value: 2}}}
	for _, opts := range []yaml.EncodeOptions{{}, {Indent: 3}, {Indent: 3, IndentSequence: true}} {
		var want bytes.Buffer
		for i, v := range values {
			doc, err := yaml.MarshalWith(v, opts)
			if err != nil {
				t.Fatal(err)
			}
			if i > 0 {
				want.WriteString("---\n")
			}
			want.Write(doc)
		}
		var got bytes.Buffer
		enc := yaml.NewEncoderWith(&got, opts)
		for _, v := range values {
			if err := enc.Encode(v); err != nil {
				t.Fatal(err)
			}
		}
		if got.String() != want.String() {
			t.Errorf("%+v stream:\n%q\nwant\n%q", opts, got.String(), want.String())
		}
	}
}
