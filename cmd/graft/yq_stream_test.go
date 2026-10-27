package main

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// firstWrite is a writer that closes seen the first time it is written to.
type firstWrite struct {
	mu   sync.Mutex
	buf  strings.Builder
	seen chan struct{}
	once sync.Once
}

func (w *firstWrite) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf.Write(p)
	w.mu.Unlock()
	w.once.Do(func() { close(w.seen) })
	return len(p), nil
}

func (w *firstWrite) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestYQPrintsEarlierDocumentsBeforeInputEnds feeds a few documents and then
// holds the input open, which only works if the earlier documents come out
// before the stream ends.
func TestYQPrintsEarlierDocumentsBeforeInputEnds(t *testing.T) {
	isolateHome(t)
	opts, err := parseYQArgs([]string{".a"}, personaYQ)
	if err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	out := &firstWrite{seen: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var errOut strings.Builder
		handleYQ(opts, pr, out, &errOut)
	}()
	go func() {
		_, _ = io.WriteString(pw, "a: 1\n---\na: 2\n---\na: 3\n---\na: 4\n")
	}()
	select {
	case <-out.seen:
	case <-time.After(20 * time.Second):
		_ = pw.Close()
		t.Fatal("nothing was printed while the input was still open")
	}
	if got := out.String(); !strings.HasPrefix(got, "1\n") {
		t.Errorf("first output = %q, want it to start with the first document", got)
	}
	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("yq did not finish after the input closed")
	}
	if got, want := out.String(), "1\n---\n2\n---\n3\n---\n4\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
