package jq_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/jq"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

// This file proves that every graft jq flag in the spec's flag table can
// be built from pkg/jq alone. It imports nothing under internal/, so a
// flag the command can express and the facade cannot fails here first.
// Each task that adds a flag adds its row.
//
// A few flags never reach the facade, because the command handles them
// before any program runs. They are -V and --version, which print version
// lines; --repl, which opens the debug REPL; the composition flags, such as
// --merge and --eval, which run graft's merge before the query; --indent
// above 7, which is a usage error; and the choice between -C and -M when
// neither is given, which follows the terminal and NO_COLOR.

// cliInput is one input file, by name and content.
type cliInput struct{ name, text string }

// cliCase is one flag combination, written as the options the command
// builds from it.
type cliCase struct {
	flags      string // the command line this row stands for
	program    string
	inputs     []cliInput // stdin when the name is empty
	nullInput  bool       // -n
	slurp      bool       // -s
	exitStatus bool       // -e
	decode     jq.DecodeOptions
	encode     jq.EncodeOptions
	opts       []jq.Option
	want       string
	wantExit   int
}

// runCLI does what the command does with c: it opens a Decoder per input,
// slurps them when -s is set, runs the program over each input or once
// over null with -n, and encodes every output.
func runCLI(t *testing.T, c cliCase) (string, int) {
	t.Helper()
	var decoders []*jq.Decoder
	var names []string
	for _, in := range c.inputs {
		opts := c.decode
		opts.Filename = in.name
		decoders = append(decoders, jq.NewDecoder(strings.NewReader(in.text), opts))
		names = append(names, in.name)
	}
	if c.slurp {
		decoders = []*jq.Decoder{jq.Slurp(decoders...)}
		names = []string{""}
	}
	cur := 0
	next := func() (*value.Value, error) {
		for cur < len(decoders) {
			v, err := decoders[cur].Next()
			if !errors.Is(err, io.EOF) {
				return v, err
			}
			cur++
		}
		return nil, io.EOF
	}
	filename := func() string {
		if cur < len(names) {
			return names[cur]
		}
		return ""
	}
	opts := append([]jq.Option{jq.WithInputs(next), jq.WithInputFilename(filename)}, c.opts...)
	q, err := jq.Compile(c.program, opts...)
	if err != nil {
		return "", jq.ExitCode(nil, false, err, c.exitStatus)
	}
	var out bytes.Buffer
	enc := jq.NewEncoder(&out, c.encode)
	var last *value.Value
	produced := false
	runOne := func(in *value.Value) error {
		for v, err := range q.Run(context.Background(), in) {
			if err != nil {
				return err
			}
			last, produced = v, true
			if err := enc.Encode(v); err != nil {
				return err
			}
		}
		return nil
	}
	if c.nullInput {
		err = runOne(value.Null())
	} else {
		for {
			in, nerr := next()
			if errors.Is(nerr, io.EOF) {
				break
			}
			if nerr != nil {
				err = nerr
				break
			}
			if err = runOne(in); err != nil {
				break
			}
		}
	}
	if ferr := enc.Flush(); ferr != nil && err == nil {
		err = ferr
	}
	return out.String(), jq.ExitCode(last, produced, err, c.exitStatus)
}

// stdin is one unnamed input.
func stdin(text string) []cliInput { return []cliInput{{text: text}} }

// decodeArg reads one value as --argjson and --jsonargs do.
func decodeArg(t *testing.T, text string) *value.Value {
	t.Helper()
	v, err := jq.NewDecoder(strings.NewReader(text), jq.DecodeOptions{}).Next()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCLIFlagsBuildFromThePublicPackage(t *testing.T) {
	dir := t.TempDir()
	progFile := filepath.Join(dir, "prog.jq")
	if err := os.WriteFile(progFile, []byte(".a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(progFile)
	if err != nil {
		t.Fatal(err)
	}
	slurpfile := jq.Slurp(jq.NewDecoder(strings.NewReader("---\n1\n---\na: 2\n"), jq.DecodeOptions{Filename: "s.yaml"}))
	slurped, err := slurpfile.Next()
	if err != nil {
		t.Fatal(err)
	}
	rawfile := jq.Slurp(jq.NewDecoder(strings.NewReader("line\n"), jq.DecodeOptions{Raw: true}))
	rawText, err := rawfile.Next()
	if err != nil {
		t.Fatal(err)
	}

	doc := `{"b":1,"a":"é"}`
	cases := []cliCase{
		{flags: "-n", program: `[inputs]`, inputs: stdin("1 2"), nullInput: true, encode: jq.EncodeOptions{Compact: true}, want: "[1,2]\n"},
		{flags: "-n (no inputs read)", program: `.`, inputs: stdin("1"), nullInput: true, want: "null\n"},
		{flags: "-R", program: `.`, inputs: stdin("a\nb\n"), decode: jq.DecodeOptions{Raw: true}, want: "\"a\"\n\"b\"\n"},
		{flags: "-s", program: `.`, inputs: []cliInput{{"a.json", "1 2\n"}, {"b.json", "3\n"}}, slurp: true, encode: jq.EncodeOptions{Compact: true}, want: "[1,2,3]\n"},
		{flags: "-R -s", program: `.`, inputs: stdin("a\nb\n"), decode: jq.DecodeOptions{Raw: true}, slurp: true, want: "\"a\\nb\\n\"\n"},
		{flags: "-n -s", program: `[inputs]`, inputs: stdin("1 2"), nullInput: true, slurp: true, encode: jq.EncodeOptions{Compact: true}, want: "[[1,2]]\n"},
		{flags: "-c", program: `.`, inputs: stdin(doc), encode: jq.EncodeOptions{Compact: true}, want: "{\"b\":1,\"a\":\"é\"}\n"},
		{flags: "-r", program: `.[]`, inputs: stdin(`["a",[1]]`), encode: jq.EncodeOptions{Raw: true, Compact: true}, want: "a\n[1]\n"},
		{flags: "--raw-output0", program: `.[]`, inputs: stdin(`["a","b"]`), encode: jq.EncodeOptions{RawNUL: true}, want: "a\x00b\x00"},
		{flags: "--raw-output0 (NUL in a string)", program: `.`, inputs: stdin(`"a\u0000b"`), encode: jq.EncodeOptions{RawNUL: true}, want: "", wantExit: 5},
		{flags: "-j", program: `.[]`, inputs: stdin(`["a",1]`), encode: jq.EncodeOptions{Join: true}, want: "a1"},
		{flags: "-a", program: `.a`, inputs: stdin(doc), encode: jq.EncodeOptions{ASCII: true}, want: "\"\\u00e9\"\n"},
		{flags: "-S", program: `.`, inputs: stdin(doc), encode: jq.EncodeOptions{Compact: true, SortKeys: sortkeys.Lexical}, want: "{\"a\":\"é\",\"b\":1}\n"},
		{flags: "--sort-keys=spruce", program: `.`, inputs: stdin(`{"b10":1,"b2":2}`), encode: jq.EncodeOptions{Compact: true, SortKeys: sortkeys.Spruce}, want: "{\"b2\":2,\"b10\":1}\n"},
		{flags: "--sort-keys=none", program: `.`, inputs: stdin(`{"b":1,"a":2}`), encode: jq.EncodeOptions{Compact: true, SortKeys: sortkeys.None}, want: "{\"b\":1,\"a\":2}\n"},
		{flags: "-o yaml", program: `.`, inputs: stdin(doc), encode: jq.EncodeOptions{Format: jq.FormatYAML}, want: "b: 1\na: é\n"},
		{flags: "-o yaml -r", program: `.a`, inputs: stdin(doc), encode: jq.EncodeOptions{Format: jq.FormatYAML, Raw: true}, want: "é\n"},
		{flags: "--float-numbers", program: `.[0] + .[1]`, inputs: stdin(`[0.1,0.2]`), opts: []jq.Option{jq.WithFloatNumbers()}, encode: jq.EncodeOptions{FloatNumbers: true}, want: "0.30000000000000004\n"},
		{flags: "(exact numbers)", program: `.[0] + .[1]`, inputs: stdin(`[0.1,0.2]`), want: "0.3\n"},
		{flags: "-C", program: `.`, inputs: stdin(`null`), encode: jq.EncodeOptions{Color: true}, want: "\x1b[0;90mnull\x1b[0m\n"},
		{flags: "-C with JQ_COLORS", program: `.`, inputs: stdin(`null`), encode: jq.EncodeOptions{Color: true, Colors: "0;31"}, want: "\x1b[0;31mnull\x1b[0m\n"},
		{flags: "-M", program: `.`, inputs: stdin(`null`), encode: jq.EncodeOptions{Color: false, Colors: "0;31"}, want: "null\n"},
		{flags: "--tab", program: `.`, inputs: stdin(`[1]`), encode: jq.EncodeOptions{Tab: true}, want: "[\n\t1\n]\n"},
		{flags: "--indent 0", program: `.`, inputs: stdin(`[1]`), encode: jq.EncodeOptions{Indent: -1}, want: "[\n1\n]\n"},
		{flags: "--indent 7", program: `.`, inputs: stdin(`[1]`), encode: jq.EncodeOptions{Indent: 7}, want: "[\n       1\n]\n"},
		{flags: "--unbuffered", program: `.`, inputs: stdin(`1 2`), encode: jq.EncodeOptions{Unbuffered: true}, want: "1\n2\n"},
		{flags: "-f FILE", program: string(program), inputs: stdin(`{"a":1}`), opts: []jq.Option{jq.WithProgramOrigin(dir)}, want: "1\n"},
		{flags: "-e (true)", program: `.`, inputs: stdin(`true`), exitStatus: true, want: "true\n"},
		{flags: "-e (false last)", program: `.[]`, inputs: stdin(`[1,false]`), exitStatus: true, want: "1\nfalse\n", wantExit: 1},
		{flags: "-e (null)", program: `.`, inputs: stdin(`null`), exitStatus: true, want: "null\n", wantExit: 1},
		{flags: "-e (no output)", program: `empty`, inputs: stdin(`1`), exitStatus: true, want: "", wantExit: 4},
		{flags: "(compile error)", program: `.[`, inputs: stdin(`1`), want: "", wantExit: 3},
		{flags: "(runtime error)", program: `error("x")`, inputs: stdin(`1`), want: "", wantExit: 5},
		{flags: "(invalid input)", program: `.`, inputs: stdin(`1 {`), want: "1\n", wantExit: 5},
		{flags: "(halt_error)", program: `"bye\n" | halt_error(7)`, inputs: stdin(`1`), want: "", wantExit: 7},
		{flags: "--arg", program: `$name`, inputs: stdin(`1`), opts: []jq.Option{jq.WithArg("name", value.Str("x"))}, want: "\"x\"\n"},
		{flags: "--argjson", program: `$v.k`, inputs: stdin(`1`), opts: []jq.Option{jq.WithArg("v", decodeArg(t, `{"k":[1]}`))}, encode: jq.EncodeOptions{Compact: true}, want: "[1]\n"},
		{flags: "--argjson (YAML)", program: `$v.k`, inputs: stdin(`1`), opts: []jq.Option{jq.WithArg("v", decodeArg(t, "k: 2\n"))}, want: "2\n"},
		{flags: "--slurpfile", program: `$f`, inputs: stdin(`1`), opts: []jq.Option{jq.WithArg("f", slurped)}, encode: jq.EncodeOptions{Compact: true}, want: "[1,{\"a\":2}]\n"},
		{flags: "--rawfile", program: `$f`, inputs: stdin(`1`), opts: []jq.Option{jq.WithArg("f", rawText)}, want: "\"line\\n\"\n"},
		{flags: "--args", program: `$ARGS`, inputs: stdin(`1`), opts: []jq.Option{jq.WithPositional([]*value.Value{value.Str("a"), value.Str("1")})}, encode: jq.EncodeOptions{Compact: true}, want: "{\"positional\":[\"a\",\"1\"],\"named\":{}}\n"},
		{flags: "--jsonargs", program: `$ARGS.positional`, inputs: stdin(`1`), opts: []jq.Option{jq.WithPositional([]*value.Value{decodeArg(t, `1`), decodeArg(t, `{"a":2}`)})}, encode: jq.EncodeOptions{Compact: true}, want: "[1,{\"a\":2}]\n"},
		{flags: "--input-format json", program: `.`, inputs: stdin(`a: 1`), decode: jq.DecodeOptions{Format: jq.FormatJSON}, want: "", wantExit: 5},
		{flags: "--input-format yaml", program: `.a`, inputs: stdin("a: 1\n"), decode: jq.DecodeOptions{Format: jq.FormatYAML}, want: "1\n"},
		{flags: "--input-format auto", program: `.a`, inputs: stdin("a: 1\n---\n{\"a\": 2}\n"), want: "1\n2\n"},
		{flags: "input_filename", program: `input_filename`, inputs: []cliInput{{"a.json", "1\n"}, {"b.json", "2\n"}}, encode: jq.EncodeOptions{Raw: true}, want: "a.json\nb.json\n"},
		{flags: "-L DIR", program: `.`, inputs: stdin(`1`), opts: []jq.Option{jq.WithModulePaths([]string{"lib", "~/.jq"})}, want: "1\n"},
		{flags: "--seq", program: `.`, inputs: stdin("\x1e1\n\x1e[2]\n"), decode: jq.DecodeOptions{Seq: true}, encode: jq.EncodeOptions{Seq: true, Compact: true}, want: "\x1e1\n\x1e[2]\n"},
		{flags: "--stream", program: `.`, inputs: stdin(`[1]`), decode: jq.DecodeOptions{Stream: true}, encode: jq.EncodeOptions{Compact: true}, want: "[[0],1]\n[[0]]\n"},
	}
	for _, c := range cases {
		got, exit := runCLI(t, c)
		if got != c.want || exit != c.wantExit {
			t.Errorf("%s: got %q exit %d, want %q exit %d", c.flags, got, exit, c.want, c.wantExit)
		}
	}
}

func TestCLIRunTestsBuildsFromThePublicPackage(t *testing.T) {
	var out bytes.Buffer
	passed, failed, err := jq.RunTestFile(strings.NewReader(".a\n{\"a\":1}\n1\n"), &out)
	if err != nil || passed != 1 || failed != 0 {
		t.Errorf("got %d passed, %d failed, %v", passed, failed, err)
	}
	if !strings.Contains(out.String(), "1 of 1 tests passed") {
		t.Errorf("unexpected summary %q", out.String())
	}
}

func TestCLIColorCheckBuildsFromThePublicPackage(t *testing.T) {
	if jq.CheckColors("0;31:nope") == nil {
		t.Error("an invalid JQ_COLORS passed the check, so the command could not warn as jq does")
	}
}
