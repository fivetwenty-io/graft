package graft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var _ Engine = (*MockEngine)(nil)

// cancelingReader cancels after handing out its first chunk.
type cancelingReader struct {
	r      io.Reader
	cancel context.CancelFunc
}

func (c *cancelingReader) Read(p []byte) (int, error) {
	if len(p) > 4 {
		p = p[:4]
	}
	n, err := c.r.Read(p)
	c.cancel()
	return n, err
}

func parseContextCalls(t *testing.T, engine Engine) map[string]func(context.Context) (Document, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "in.yml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]func(context.Context) (Document, error){
		"yaml": func(ctx context.Context) (Document, error) { return engine.ParseYAMLContext(ctx, []byte("a: 1\n")) },
		"json": func(ctx context.Context) (Document, error) { return engine.ParseJSONContext(ctx, []byte(`{"a": 1}`)) },
		"file": func(ctx context.Context) (Document, error) { return engine.ParseFileContext(ctx, path) },
		"reader": func(ctx context.Context) (Document, error) {
			return engine.ParseReaderContext(ctx, strings.NewReader("a: 1\n"))
		},
	}
}

func TestParseContextVariants(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	live := context.Background()
	dead, cancel := context.WithCancel(context.Background())
	cancel()

	for name, call := range parseContextCalls(t, engine) {
		doc, err := call(live)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		} else if v, _ := doc.GetInt("a"); v != 1 {
			t.Errorf("%s: a = %d", name, v)
		}
		_, err = call(dead)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s with a canceled context: %v", name, err)
			continue
		}
		if strings.Contains(err.Error(), "evaluation stopped") || !strings.Contains(err.Error(), "parse stopped") {
			t.Errorf("%s: message %q should say parse stopped", name, err)
		}
	}
}

func TestParseContextDeadline(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	if _, err := engine.ParseYAMLContext(ctx, []byte("a: 1\n")); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func TestParseContextBothModes(t *testing.T) {
	var results []string
	for _, parallel := range []string{"true", "false"} {
		t.Setenv("GRAFT_FEATURE_PARALLEL", parallel)
		engine, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}
		calls := parseContextCalls(t, engine)
		var got []string
		for _, name := range []string{"yaml", "json", "file", "reader"} {
			doc, err := calls[name](context.Background())
			if err != nil {
				t.Fatalf("%s parallel=%s: %v", name, parallel, err)
			}
			v, _ := doc.GetInt("a")
			got = append(got, fmt.Sprintf("%s=%d", name, v))
		}
		results = append(results, strings.Join(got, ","))
	}
	if results[0] != results[1] {
		t.Errorf("parallel %q, sequential %q", results[0], results[1])
	}
}

func TestPlainParseMatchesContextParse(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"", "a: [", "- 1\n", "a: 1\n"} {
		p, perr := engine.ParseYAML([]byte(in))
		c, cerr := engine.ParseYAMLContext(context.Background(), []byte(in))
		if (perr == nil) != (cerr == nil) || (perr != nil && perr.Error() != cerr.Error()) || (p == nil) != (c == nil) {
			t.Errorf("input %q: plain (%v, %v) differs from context (%v, %v)", in, p, perr, c, cerr)
		}
	}
}

func TestParseReaderContextStopsMidRead(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &cancelingReader{r: strings.NewReader(strings.Repeat("k: v\n", 100)), cancel: cancel}
	if _, err := engine.ParseReaderContext(ctx, r); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

// eofWithData hands out all its data together with io.EOF, and runs
// after once that final Read has happened.
type eofWithData struct {
	data  string
	after func()
}

func (e *eofWithData) Read(p []byte) (int, error) {
	n := copy(p, e.data)
	if e.after != nil {
		e.after()
	}
	return n, io.EOF
}

func TestParseReaderContextKeepsDataReturnedWithEOF(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := engine.ParseReaderContext(context.Background(), &eofWithData{data: "a: 1\nb: 2\n"})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := doc.GetInt("a")
	b, _ := doc.GetInt("b")
	if a != 1 || b != 2 {
		t.Errorf("a = %d, b = %d", a, b)
	}
}

func TestParseReaderContextCanceledAfterFinalRead(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	doc, err := engine.ParseReaderContext(ctx, &eofWithData{data: "a: 1\n", after: cancel})
	if doc != nil || !errors.Is(err, context.Canceled) {
		t.Errorf("doc = %v, err = %v", doc, err)
	}
}
