package yq

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func wantEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func wantContains(t *testing.T, got, sub string) {
	t.Helper()
	if !strings.Contains(got, sub) {
		t.Errorf("%q does not contain %q", got, sub)
	}
}

// wantErrAs checks that err holds an error of target's type and sets
// target to it.
func wantErrAs(t *testing.T, err error, target any) {
	t.Helper()
	if !errors.As(err, target) {
		t.Fatalf("got %T (%v), want %T", err, err, target)
	}
}

func mustCompile(t *testing.T, src string, opts ...Option) *Evaluator {
	t.Helper()
	ev, err := Compile(src, opts...)
	if err != nil {
		t.Fatalf("compiling %s: %v", src, err)
	}
	return ev
}

// collect gathers the outputs of a run and the error that ended it.
func collect(seq func(yield func(*value.Value, error) bool)) ([]*value.Value, error) {
	var out []*value.Value
	for v, err := range seq {
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
	return out, nil
}

// decodeAll reads every YAML document in text.
func decodeAll(t *testing.T, text string) []*value.Value {
	t.Helper()
	dec := NewDecoder(strings.NewReader(text), DecodeOptions{Filename: "in.yaml"})
	var docs []*value.Value
	for {
		v, err := dec.Next()
		if errors.Is(err, io.EOF) {
			return docs
		}
		if err != nil {
			t.Fatalf("decoding %q: %v", text, err)
		}
		docs = append(docs, v)
	}
}

func decodeOne(t *testing.T, text string) *value.Value {
	t.Helper()
	return decodeAll(t, text)[0]
}

// render runs src over the documents of in, one at a time or, with all
// set, together, and returns the YAML the outputs print as.
func render(t *testing.T, src, in string, all bool, opts ...Option) string {
	t.Helper()
	ev := mustCompile(t, src, opts...)
	docs := decodeAll(t, in)
	var buf bytes.Buffer
	enc := NewEncoder(&buf, EncodeOptions{UnwrapScalar: true})
	emit := func(seq func(yield func(*value.Value, error) bool)) {
		for v, err := range seq {
			if err != nil {
				t.Fatalf("running %s: %v", src, err)
			}
			if err := enc.Encode(v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if all {
		emit(ev.EvalAll(t.Context(), docs))
	} else {
		for _, d := range docs {
			emit(ev.Eval(t.Context(), d))
		}
	}
	if err := enc.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
