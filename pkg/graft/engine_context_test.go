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

	"github.com/fivetwenty-io/graft/internal/features"
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

// newModeEngine returns an engine that evaluates in parallel, with a
// worker pool, when parallel is set, and in sequence when it is not.
// NewEngine reads no environment, so a test that means to cover both
// modes picks the mode here.
func newModeEngine(t *testing.T, parallel bool) Engine {
	t.Helper()
	engine, err := NewEngine(WithParallel(parallel))
	if err != nil {
		t.Fatal(err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T", engine)
	}
	if got := de.IsFeatureEnabled(features.FeatureParallelEvaluation) && de.Pool != nil; got != parallel {
		t.Fatalf("engine runs in parallel = %v, want %v", got, parallel)
	}
	return engine
}

func TestParseContextBothModes(t *testing.T) {
	var results []string
	for _, parallel := range []bool{true, false} {
		engine := newModeEngine(t, parallel)
		calls := parseContextCalls(t, engine)
		var got []string
		for _, name := range []string{"yaml", "json", "file", "reader"} {
			doc, err := calls[name](context.Background())
			if err != nil {
				t.Fatalf("%s parallel=%v: %v", name, parallel, err)
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

func TestOutputAndDiffContextVariants(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	a := mustParseYAMLDoc(t, engine, "a: 1\n")
	b := mustParseYAMLDoc(t, engine, "a: 2\n")
	live := context.Background()
	dead, cancel := context.WithCancel(context.Background())
	cancel()

	if out, err := engine.ToYAMLContext(live, a.Clone()); err != nil || string(out) != "a: 1\n" {
		t.Errorf("ToYAMLContext = %q, %v", out, err)
	}
	if out, err := engine.ToJSONContext(live, a.Clone()); err != nil || !strings.Contains(string(out), `"a"`) {
		t.Errorf("ToJSONContext = %q, %v", out, err)
	}
	if out, err := engine.ToJSONIndentContext(live, a.Clone(), "  "); err != nil || !strings.Contains(string(out), "\n  \"a\"") {
		t.Errorf("ToJSONIndentContext = %q, %v", out, err)
	}
	if r, err := engine.DiffContext(live, a, b, nil); err != nil || r == nil || !r.HasChanges() {
		t.Errorf("DiffContext = %v, %v", r, err)
	}

	for name, call := range map[string]func() error{
		"ToYAMLContext":       func() error { _, err := engine.ToYAMLContext(dead, a.Clone()); return err },
		"ToJSONContext":       func() error { _, err := engine.ToJSONContext(dead, a.Clone()); return err },
		"ToJSONIndentContext": func() error { _, err := engine.ToJSONIndentContext(dead, a.Clone(), " "); return err },
	} {
		err := call()
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "encoding stopped") {
			t.Errorf("%s canceled: %v", name, err)
		}
	}
	r, err := engine.DiffContext(dead, a, b, nil)
	if r != nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "diff stopped") {
		t.Errorf("DiffContext canceled: %v, %v", r, err)
	}

	late, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	for name, call := range map[string]func() error{
		"ToYAMLContext":       func() error { _, err := engine.ToYAMLContext(late, a.Clone()); return err },
		"ToJSONContext":       func() error { _, err := engine.ToJSONContext(late, a.Clone()); return err },
		"ToJSONIndentContext": func() error { _, err := engine.ToJSONIndentContext(late, a.Clone(), " "); return err },
		"DiffContext":         func() error { _, err := engine.DiffContext(late, a, b, nil); return err },
	} {
		if err := call(); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s past its deadline: %v", name, err)
		}
	}
}

func TestOutputContextKeepsNilDocumentError(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ToYAMLContext(context.Background(), nil); err == nil {
		t.Error("ToYAMLContext(nil) returned no error")
	}
	if _, err := engine.ToJSONContext(context.Background(), nil); err == nil {
		t.Error("ToJSONContext(nil) returned no error")
	}
	if _, err := engine.ToJSONIndentContext(context.Background(), nil, " "); err == nil {
		t.Error("ToJSONIndentContext(nil) returned no error")
	}
	// DiffContext refuses a nil document, while DiffWithOptions reads
	// one as empty and keeps the result it always gave.
	b := mustParseYAMLDoc(t, engine, "a: 1\n")
	if r, err := engine.DiffContext(context.Background(), nil, b, nil); err == nil || r != nil {
		t.Errorf("DiffContext(nil) = %v, %v, want an error", r, err)
	}
	if r := engine.DiffWithOptions(nil, b, nil); r == nil || r.HasChanges() {
		t.Errorf("DiffWithOptions(nil) = %v, want an empty result", r)
	}
}

func TestToYAMLContextStopsEvaluation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op := &stopper{cancel: cancel}
	engine, err := NewEngine(WithCustomOperator("stopper", op), WithParallel(false))
	if err != nil {
		t.Fatal(err)
	}
	doc := mustParseYAMLDoc(t, engine, "a: (( stopper ))\nb: (( stopper \"a\" ))\n")
	if _, err := engine.ToYAMLContext(ctx, doc); !errors.Is(err, context.Canceled) || op.runs.Load() != 1 {
		t.Errorf("err = %v, runs = %d", err, op.runs.Load())
	}
}

func TestOutputContextBothModes(t *testing.T) {
	var results []string
	for _, parallel := range []bool{true, false} {
		engine := newModeEngine(t, parallel)
		ctx := context.Background()
		base := mustParseYAMLDoc(t, engine, "n: 1\nx: (( concat \"v\" meta.n ))\nmeta:\n  n: 7\n")
		overlay := mustParseYAMLDoc(t, engine, "y: (( grab x ))\nmeta:\n  n: 8\n")
		merged, err := engine.Merge(ctx, base, overlay).Execute()
		if err != nil {
			t.Fatalf("parallel=%v merge: %v", parallel, err)
		}
		want := mustParseYAMLDoc(t, engine, "n: 1\nx: v8\ny: v8\nmeta:\n  n: 8\n")
		diff, err := engine.DiffContext(ctx, merged.Clone(), want, nil)
		if err != nil {
			t.Fatalf("parallel=%v diff: %v", parallel, err)
		}
		if diff.HasChanges() {
			t.Errorf("parallel=%v: merged document differs from the expected one", parallel)
		}
		yml, err := engine.ToYAMLContext(ctx, merged.Clone())
		if err != nil {
			t.Fatalf("parallel=%v yaml: %v", parallel, err)
		}
		js, err := engine.ToJSONContext(ctx, merged.Clone())
		if err != nil {
			t.Fatalf("parallel=%v json: %v", parallel, err)
		}
		ind, err := engine.ToJSONIndentContext(ctx, merged.Clone(), "\t")
		if err != nil {
			t.Fatalf("parallel=%v indent: %v", parallel, err)
		}
		results = append(results, fmt.Sprintf("%v|%s|%s|%s", diff.HasChanges(), yml, js, ind))
	}
	if results[0] != results[1] {
		t.Errorf("parallel %q, sequential %q", results[0], results[1])
	}
}
