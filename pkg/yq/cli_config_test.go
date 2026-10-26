package yq_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
	"github.com/fivetwenty-io/graft/pkg/yq"
)

// This file proves that the graft yq flags in the spec's flag table can be
// built from pkg/yq alone. It imports nothing under internal/, so a flag
// the command can express and the facade cannot fails here first. Each task
// that adds a flag adds its row.
//
// A few flags never reach the facade, because the command handles them
// before any expression runs. They are -V and --version, which print
// version lines; -v, which turns on tracing the command owns; --repl, which
// opens the debug REPL; the composition flags, such as --merge and --eval,
// which run graft's merge before the query; -i, which rewrites a file; and
// --from-file and --expression, which only say where the expression text
// comes from. The choice between -C and -M when neither is given follows
// the terminal, and -p and -o auto follow the file name.

// cliInput is one input file, by name and content.
type cliInput struct{ name, text string }

// cliCase is one flag combination, written as the options the command
// builds from it.
type cliCase struct {
	flags      string // the command line this row stands for
	expr       string
	inputs     []cliInput
	nullInput  bool // -n
	all        bool // eval-all
	exitStatus bool // -e
	decode     yq.DecodeOptions
	encode     yq.EncodeOptions
	opts       []yq.Option
	want       string
	wantExit   int
}

// runCLI does what the command does with c: it opens a Decoder per input,
// runs the expression over each document or, for eval-all, over all of
// them, and encodes every output.
func runCLI(t *testing.T, c cliCase) (string, int) {
	t.Helper()
	ev, err := yq.Compile(c.expr, c.opts...)
	if err != nil {
		return "", yq.ExitCode(false, false, err, c.exitStatus)
	}
	var docs []*value.Value
	for i, in := range c.inputs {
		opts := c.decode
		opts.Filename, opts.FileIndex = in.name, i
		dec := yq.NewDecoder(strings.NewReader(in.text), opts)
		for {
			doc, derr := dec.Next()
			if errors.Is(derr, io.EOF) {
				break
			}
			if derr != nil {
				return "", yq.ExitCode(false, false, derr, c.exitStatus)
			}
			docs = append(docs, doc)
		}
	}
	var out bytes.Buffer
	enc := yq.NewEncoder(&out, c.encode)
	produced, anyTruthy := false, false
	drain := func(seq func(yield func(*value.Value, error) bool)) error {
		for v, rerr := range seq {
			if rerr != nil {
				return rerr
			}
			produced = true
			if v.Kind() != value.KindNull && v.Kind() != value.KindFalse {
				anyTruthy = true
			}
			if eerr := enc.Encode(v); eerr != nil {
				return eerr
			}
		}
		return nil
	}
	ctx := context.Background()
	switch {
	case c.nullInput:
		err = drain(ev.Eval(ctx, value.Null()))
	case c.all:
		err = drain(ev.EvalAll(ctx, docs))
	default:
		for _, doc := range docs {
			if err = drain(ev.Eval(ctx, doc)); err != nil {
				break
			}
		}
	}
	if ferr := enc.Flush(); ferr != nil && err == nil {
		err = ferr
	}
	return out.String(), yq.ExitCode(produced, anyTruthy, err, c.exitStatus)
}

func stdin(text string) []cliInput { return []cliInput{{text: text}} }

func TestCLIFlagsBuildFromThePublicPackage(t *testing.T) {
	two := "a: 1\n---\na: 2\n"
	cases := []cliCase{
		{flags: "(identity keeps comments and styles)", expr: `.`, inputs: stdin("# top\na: 1 # one\nb: [x, y]\n"), want: "# top\na: 1 # one\nb: [x, y]\n"},
		{flags: "(scalar per document)", expr: `.a`, inputs: stdin(two), encode: yq.EncodeOptions{UnwrapScalar: true}, want: "1\n---\n2\n"},
		{flags: "-n", expr: `.a = 1`, inputs: stdin("a: 9\n"), nullInput: true, want: "a: 1\n"},
		{flags: "-N", expr: `.`, inputs: stdin(two), encode: yq.EncodeOptions{NoDocSeparators: true}, want: "a: 1\na: 2\n"},
		{flags: "-I 4", expr: `.`, inputs: stdin("a:\n  b: 1\n"), encode: yq.EncodeOptions{Indent: 4}, want: "a:\n    b: 1\n"},
		{flags: "-P", expr: `.`, inputs: stdin("a: {b: [1]}\n"), encode: yq.EncodeOptions{PrettyPrint: true}, want: "a:\n  b:\n    - 1\n"},
		{flags: "-r=false", expr: `.`, inputs: stdin("\"hi\"\n"), want: "\"hi\"\n"},
		{flags: "-r", expr: `.`, inputs: stdin("\"hi\"\n"), encode: yq.EncodeOptions{UnwrapScalar: true}, want: "hi\n"},
		{flags: "-o json", expr: `.a`, inputs: stdin("a: {b: 1}\n"), encode: yq.EncodeOptions{Format: yq.FormatJSON}, want: "{\n  \"b\": 1\n}\n"},
		{flags: "-o json -I 0", expr: `.`, inputs: stdin("a: {b: 1}\n"), encode: yq.EncodeOptions{Format: yq.FormatJSON, Indent: -1}, want: "{\"a\":{\"b\":1}}\n"},
		{flags: "-o json -I 0 (two documents)", expr: `.`, inputs: stdin(two), encode: yq.EncodeOptions{Format: yq.FormatJSON, Indent: -1}, want: "{\"a\":1}\n{\"a\":2}\n"},
		{flags: "-p json -o yaml", expr: `.`, inputs: stdin(`{"a": [1, 2]}`), decode: yq.DecodeOptions{Format: yq.FormatJSON}, want: "a:\n  - 1\n  - 2\n"},
		{flags: "eval-all", expr: `[.a]`, inputs: stdin(two), all: true, encode: yq.EncodeOptions{Format: yq.FormatJSON, Indent: -1}, want: "[1,2]\n"},
		{flags: "eval-all (two files)", expr: `[.a]`, inputs: []cliInput{{"a.yaml", "a: 1\n"}, {"b.yaml", "a: 2\n"}}, all: true, encode: yq.EncodeOptions{Format: yq.FormatJSON, Indent: -1}, want: "[1,2]\n"},
		{flags: "eval (two files)", expr: `.a`, inputs: []cliInput{{"a.yaml", "a: 1\n"}, {"b.yaml", "a: 2\n"}}, encode: yq.EncodeOptions{UnwrapScalar: true}, want: "1\n---\n2\n"},
		{flags: "filename and file_index", expr: `filename + (file_index | tostring)`, inputs: []cliInput{{"a.yaml", "a: 1\n"}, {"b.yaml", "a: 2\n"}}, encode: yq.EncodeOptions{UnwrapScalar: true}, want: "a.yaml0\nb.yaml1\n"},
		{flags: "-S", expr: `.`, inputs: stdin("b: 1\na: 2\n"), encode: yq.EncodeOptions{SortKeys: sortkeys.Lexical}, want: "a: 2\nb: 1\n"},
		{flags: "--sort-keys=none", expr: `.`, inputs: stdin("b: 1\na: 2\n"), encode: yq.EncodeOptions{SortKeys: sortkeys.None}, want: "b: 1\na: 2\n"},
		{flags: "--float-numbers", expr: `.a + .b`, inputs: stdin("a: 0.1\nb: 0.2\n"), opts: []yq.Option{yq.WithFloatNumbers()}, want: "0.30000000000000004\n"},
		{flags: "(exact numbers)", expr: `.a + .b`, inputs: stdin("a: 0.1\nb: 0.2\n"), want: "0.3\n"},
		// Stored and not honored yet: this row pins that the option changes
		// nothing, and it must change when the parser starts reading it.
		{flags: "--string-interpolation=false (stored, not honored)", expr: `.`, inputs: stdin("a: 1\n"), opts: []yq.Option{yq.WithStringInterpolation(false)}, want: "a: 1\n"},
		{flags: "--header-preprocess=false", expr: `.`, inputs: stdin("a: 1\n"), opts: []yq.Option{yq.WithHeaderPreprocess(false)}, want: "a: 1\n"},
		{flags: "--yaml-fix-merge-anchor-to-spec", expr: `.`, inputs: stdin("a: 1\n"), opts: []yq.Option{yq.WithMergeAnchorFix(true)}, want: "a: 1\n"},
		{flags: "--security-disable-env-ops", expr: `strenv(HOME)`, inputs: stdin("a: 1\n"), opts: []yq.Option{yq.WithEnvOps(false)}, wantExit: 1},
		{flags: "--security-disable-file-ops", expr: `load("x.yaml")`, inputs: stdin("a: 1\n"), opts: []yq.Option{yq.WithFileOps(false)}, wantExit: 1},
		{flags: "--security-enable-system-operator", expr: `.a`, inputs: stdin("a: 1\n"), opts: []yq.Option{yq.WithSystemOps(true)}, want: "1\n"},
		{flags: "(environment)", expr: `strenv(WHO)`, inputs: stdin("a: 1\n"), opts: []yq.Option{yq.WithEnv([]string{"WHO=x"})}, encode: yq.EncodeOptions{UnwrapScalar: true}, want: "x\n"},
		{flags: "-e (value)", expr: `.a`, inputs: stdin("a: 1\n"), exitStatus: true, want: "1\n"},
		{flags: "-e ([true, false])", expr: `.[]`, inputs: stdin("[true, false]\n"), exitStatus: true, encode: yq.EncodeOptions{UnwrapScalar: true}, want: "true\nfalse\n"},
		{flags: "-e ([false, true])", expr: `.[]`, inputs: stdin("[false, true]\n"), exitStatus: true, encode: yq.EncodeOptions{UnwrapScalar: true}, want: "false\ntrue\n"},
		{flags: "-e (null and false)", expr: `.[]`, inputs: stdin("[null, false]\n"), exitStatus: true, encode: yq.EncodeOptions{UnwrapScalar: true}, want: "null\nfalse\n", wantExit: 1},
		{flags: "-e (nothing)", expr: `.[]`, inputs: stdin("[]\n"), exitStatus: true, wantExit: 1},
		{flags: "(compile error)", expr: `.a | | .b`, inputs: stdin("a: 1\n"), wantExit: 1},
		{flags: "(runtime error)", expr: `error("x")`, inputs: stdin("a: 1\n"), wantExit: 1},
		{flags: "(invalid input)", expr: `.`, inputs: stdin("a: [1\n"), wantExit: 1},
	}
	for _, c := range cases {
		got, exit := runCLI(t, c)
		if got != c.want || exit != c.wantExit {
			t.Errorf("%s: got %q exit %d, want %q exit %d", c.flags, got, exit, c.want, c.wantExit)
		}
	}
}
