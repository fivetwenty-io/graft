package yaml_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// failOnce takes half of the bytes on its failing call and all of them
// on every other call.
type failOnce struct {
	buf    bytes.Buffer
	calls  int
	failOn int
}

func (f *failOnce) Write(p []byte) (int, error) {
	f.calls++
	if f.calls == f.failOn {
		n := len(p) / 2
		f.buf.Write(p[:n])
		return n, errors.New("disk full")
	}
	return f.buf.Write(p)
}

func TestEncoderAfterAPartialWrite(t *testing.T) {
	w := &failOnce{failOn: 2}
	enc := yaml.NewEncoder(w)
	if err := enc.Encode(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]int{"b": 2}); err == nil {
		t.Fatal("the failing write returned no error")
	}
	if err := enc.Encode(map[string]int{"c": 3}); err != nil {
		t.Fatal(err)
	}
	if got, want := w.buf.String(), "a: 1\n---\n---\nc: 3\n"; got != want {
		t.Errorf("stream = %q, want %q", got, want)
	}
}

func TestEncoderAfterAFailedFirstDocument(t *testing.T) {
	w := &failOnce{failOn: 1}
	enc := yaml.NewEncoder(w)
	if err := enc.Encode(map[string]int{"a": 1}); err == nil {
		t.Fatal("the failing write returned no error")
	}
	if err := enc.Encode(map[string]int{"b": 2}); err != nil {
		t.Fatal(err)
	}
	if got, want := w.buf.String(), "a:b: 2\n"; got != want {
		t.Errorf("stream = %q, want %q (no separator after a failed first document)", got, want)
	}
}
