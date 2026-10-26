package jq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/regex"
)

func TestCompileErrorIsTyped(t *testing.T) {
	_, err := Compile(`.[`)
	var ce *query.CompileError
	wantErrAs(t, err, &ce)
	wantEqual(t, ce.Line, 1)
	_, err = Compile(`$nope`)
	wantErrAs(t, err, &ce)
	wantEqual(t, ce.Msg, "$nope is not defined")
}

func TestParseChecksSyntaxBeforeCompiling(t *testing.T) {
	p, err := Parse(`.a | nosuchfn`)
	if err != nil {
		t.Fatalf("Parse resolves no names, got %v", err)
	}
	_, err = p.Compile()
	var ce *query.CompileError
	wantErrAs(t, err, &ce)
	wantEqual(t, ce.Msg, "nosuchfn/0 is not defined")
	_, err = Parse("1 +\n+")
	wantErrAs(t, err, &ce)
	wantEqual(t, ce.Line, 2)
	p, _ = Parse(`. + $n`)
	q, err := p.Compile(WithArg("n", value.Num(1)))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := q.RunAll(t.Context(), value.Num(2))
	wantJSON(t, out[0], `3`)
}

func TestRuntimeErrorCarriesValue(t *testing.T) {
	_, err := mustCompile(t, `error({"x":1})`).RunAll(context.Background(), value.Null())
	var re *query.RuntimeError
	wantErrAs(t, err, &re)
	wantEqual(t, re.Value.Kind(), value.KindObject)
}

func TestRunAllKeepsOutputsBeforeAnError(t *testing.T) {
	out, err := mustCompile(t, `1, 2, error("x")`).RunAll(t.Context(), value.Null())
	wantErrContains(t, err, "x")
	wantEqual(t, len(out), 2)
}

func TestHaltErrorEndsTheRun(t *testing.T) {
	_, err := mustCompile(t, `"bye\n" | halt_error(3)`).RunAll(t.Context(), value.Null())
	var he *query.HaltError
	wantErrAs(t, err, &he)
	wantEqual(t, he.Code, 3)
	wantEqual(t, he.Message.Str(), "bye\n")
}

func TestRunStopsWhenTheCallerStops(t *testing.T) {
	for _, src := range []string{
		`inputs`,
		`try inputs catch "caught"`,
		`(inputs | select(. > 0)) // "none"`,
		`label $out | inputs`,
		`[limit(5; inputs)][]`,
		`first(inputs), inputs`,
		`first(inputs), reduce inputs as $x (0; .)`,
		`inputs as $x | $x`,
		`path(inputs)? // 1, inputs`,
	} {
		reads := 0
		next := func() (*value.Value, error) {
			reads++
			if reads > 100 {
				return nil, io.EOF
			}
			return value.Num(float64(reads)), nil
		}
		q := mustCompile(t, src, WithInputs(next))
		got := 0
		for _, err := range q.Run(t.Context(), value.Null()) {
			if err != nil {
				t.Errorf("%s: %v", src, err)
			}
			got++
			break
		}
		wantEqual(t, got, 1)
		if reads > 6 {
			t.Errorf("%s read %d inputs after the caller stopped at the first output", src, reads)
		}
	}
}

func TestOneSequenceRangesFromManyGoroutines(t *testing.T) {
	// Run with -race: ranging one Seq2 concurrently must not write
	// anything the goroutines share, such as a nil input.
	seq := mustCompile(t, `. // 1`).Run(t.Context(), nil)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for v, err := range seq {
				if err != nil || compactJSON(t, v) != "1" {
					t.Errorf("got %v, %v", v, err)
				}
			}
		})
	}
	wg.Wait()
}

func TestRunHonorsTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := mustCompile(t, `last(repeat(1))`).RunAll(ctx, value.Null())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestQueryRunsAnyNumberOfTimes(t *testing.T) {
	q := mustCompile(t, `.[] * 2`)
	for range 2 {
		out, err := q.RunAll(t.Context(), mustDecode(t, `[1,2]`))
		if err != nil {
			t.Fatal(err)
		}
		wantEqual(t, len(out), 2)
		wantJSON(t, out[1], `4`)
	}
}

func TestNilInputIsNull(t *testing.T) {
	out, err := mustCompile(t, `type`).RunAll(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, out[0], `"null"`)
}

func TestSlurpCrossesDecoders(t *testing.T) {
	d := Slurp(NewDecoder(strings.NewReader("1 2"), DecodeOptions{}), NewDecoder(strings.NewReader("3"), DecodeOptions{}))
	v, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, v, `[1,2,3]`)
	if _, err := d.Next(); !errors.Is(err, io.EOF) {
		t.Fatal("slurp yields one value")
	}
	r := Slurp(NewDecoder(strings.NewReader("a\nb\n"), DecodeOptions{Raw: true}), NewDecoder(strings.NewReader("c"), DecodeOptions{Raw: true}))
	v, _ = r.Next()
	wantEqual(t, v.Str(), "a\nb\nc")
	empty := Slurp(NewDecoder(strings.NewReader(""), DecodeOptions{}))
	v, _ = empty.Next()
	wantJSON(t, v, `[]`)
	none, _ := Slurp().Next()
	wantJSON(t, none, `[]`)
}

func TestSlurpReportsABrokenInput(t *testing.T) {
	d := Slurp(NewDecoder(strings.NewReader("1 {"), DecodeOptions{Filename: "a.json"}))
	_, err := d.Next()
	wantErrContains(t, err, "a.json")
	_, again := d.Next()
	wantEqual(t, again, err)
}

func TestSlurpOfASlurpNests(t *testing.T) {
	inner := Slurp(NewDecoder(strings.NewReader("1 2"), DecodeOptions{}))
	v, _ := Slurp(inner, NewDecoder(strings.NewReader("3"), DecodeOptions{})).Next()
	wantJSON(t, v, `[[1,2],3]`)
}

func TestRawSlurpOfAnExhaustedDecoderIsEmpty(t *testing.T) {
	// printf '' | jq -Rs . prints "", so a raw source with nothing left
	// still slurps to one empty string.
	v, err := Slurp(NewDecoder(strings.NewReader(""), DecodeOptions{Raw: true})).Next()
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, v, `""`)
	d := NewDecoder(strings.NewReader("a\n"), DecodeOptions{Raw: true})
	for {
		if _, err := d.Next(); errors.Is(err, io.EOF) {
			break
		}
	}
	v, err = Slurp(d).Next()
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, v, `""`)
}

func TestDecoderIsLazy(t *testing.T) {
	d := NewDecoder(strings.NewReader("1\n{"), DecodeOptions{})
	v, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, v, `1`)
	_, err = d.Next()
	wantErrContains(t, err, "line 2")
}

func TestDecoderReadsEachFormat(t *testing.T) {
	yaml := NewDecoder(strings.NewReader("a: 1\nb: [x]\n"), DecodeOptions{})
	v, _ := yaml.Next()
	wantJSON(t, v, `{"a":1,"b":["x"]}`)
	_, err := NewDecoder(strings.NewReader("{a: 1}"), DecodeOptions{Format: FormatJSON, Filename: "in.json"}).Next()
	wantErrContains(t, err, "in.json")
	v, _ = NewDecoder(strings.NewReader("{a: 1}"), DecodeOptions{Format: FormatYAML}).Next()
	wantJSON(t, v, `{"a":1}`)
	raw := NewDecoder(strings.NewReader("x\ny\n"), DecodeOptions{Raw: true})
	_, _ = raw.Next()
	v, _ = raw.Next()
	wantEqual(t, v.Str(), "y")
	wantEqual(t, raw.Line(), 2)
	_, err = raw.Next()
	wantEqual(t, err, io.EOF)
}

func TestDecoderLimitsDepth(t *testing.T) {
	_, err := NewDecoder(strings.NewReader("[[[1]]]"), DecodeOptions{DepthLimit: 2}).Next()
	wantErrContains(t, err, "depth")
}

func TestInputsCallback(t *testing.T) {
	vals := []*value.Value{value.Num(1), value.Num(2)}
	next := func() (*value.Value, error) {
		if len(vals) == 0 {
			return nil, io.EOF
		}
		v := vals[0]
		vals = vals[1:]
		return v, nil
	}
	out, _ := mustCompile(t, `[inputs]`, WithInputs(next)).RunAll(context.Background(), value.Null())
	wantJSON(t, out[0], `[1,2]`)
}

func TestInputsFromADecoder(t *testing.T) {
	d := NewDecoder(strings.NewReader("1 2 3"), DecodeOptions{})
	first, _ := d.Next()
	out, _ := mustCompile(t, `[., input]`, WithInputs(d.Next)).RunAll(t.Context(), first)
	wantJSON(t, out[0], `[1,2]`)
	_, err := mustCompile(t, `input`).RunAll(t.Context(), value.Null())
	wantErrContains(t, err, "No more inputs")
}

func TestInputsPassAReadError(t *testing.T) {
	d := NewDecoder(strings.NewReader("1 {"), DecodeOptions{})
	_, err := mustCompile(t, `[inputs]`, WithInputs(d.Next)).RunAll(t.Context(), value.Null())
	wantErrContains(t, err, "line 1")
}

func TestInputFilename(t *testing.T) {
	wantEqual(t, outputs(t, `input_filename`, `1`), []string{`null`})
	name := func() string { return "<stdin>" }
	wantEqual(t, outputs(t, `input_filename`, `1`, WithInputFilename(name)), []string{`"<stdin>"`})
}

func TestEnvAndPositional(t *testing.T) {
	q := mustCompile(t, `$ENV.K, $ARGS.positional[0]`, WithEnv([]string{"K=v"}), WithPositional([]*value.Value{value.Str("p")}))
	out, _ := q.RunAll(context.Background(), value.Null())
	wantEqual(t, []string{out[0].Str(), out[1].Str()}, []string{"v", "p"})
}

func TestEnvReplacesTheProcessEnvironment(t *testing.T) {
	t.Setenv("GRAFT_PKG_JQ_TEST", "from the process")
	wantEqual(t, outputs(t, `$ENV.GRAFT_PKG_JQ_TEST`, `null`), []string{`"from the process"`})
	wantEqual(t, outputs(t, `env.GRAFT_PKG_JQ_TEST`, `null`, WithEnv([]string{"A=1=2", "junk"})), []string{`null`})
	wantEqual(t, outputs(t, `$ENV`, `null`, WithEnv([]string{"A=1=2", "junk"})), []string{`{"A":"1=2"}`})
}

func TestPositionalAccumulates(t *testing.T) {
	got := outputs(t, `$ARGS`, `null`, WithPositional([]*value.Value{value.Num(1)}), WithPositional([]*value.Value{value.Str("a")}))
	wantEqual(t, got, []string{`{"positional":[1,"a"],"named":{}}`})
}

func TestNamedArgsKeepTheFirstBinding(t *testing.T) {
	got := outputs(t, `$ARGS.named, $b`, `null`, WithArg("b", value.Str("1")), WithArg("a", value.Str("2")), WithArg("b", value.Str("3")))
	wantEqual(t, got, []string{`{"b":"1","a":"2"}`, `"1"`}) // jq 1.8.2 keeps the first --arg of a name
}

func TestNamedArgsNeverShadowTheBuiltinVariables(t *testing.T) {
	// jq 1.8.2 keeps $ENV and $ARGS when --arg reuses their names, and
	// lists the arguments only under $ARGS.named.
	got := outputs(t, `$ENV | type`, `null`, WithArg("ENV", value.Str("x")))
	wantEqual(t, got, []string{`"object"`})
	got = outputs(t, `$ARGS`, `null`, WithArg("ARGS", value.Str("x")))
	wantEqual(t, got, []string{`{"positional":[],"named":{"ARGS":"x"}}`})
	got = outputs(t, `[$ARGS.named, ($ENV | type)]`, `null`, WithArg("ARGS", value.Str("x")), WithArg("ENV", value.Str("y")))
	wantEqual(t, got, []string{`[{"ARGS":"x","ENV":"y"},"object"]`})
}

func TestStderrReceivesDebugAndStderr(t *testing.T) {
	var buf bytes.Buffer
	outputs(t, `debug | stderr | debug("m")`, `[1]`, WithStderr(&buf))
	wantEqual(t, buf.String(), "[\"DEBUG:\",[1]]\n[1][\"DEBUG:\",\"m\"]\n")
	outputs(t, `debug`, `1`, WithStderr(nil)) // nil discards
}

func TestDepthLimit(t *testing.T) {
	_, err := mustCompile(t, `def f: [f]; f`, WithDepthLimit(50)).RunAll(t.Context(), value.Null())
	wantErrContains(t, err, "exceeds depth limit of 50")
	_, err = Compile(`.`, WithDepthLimit(-1))
	wantErrContains(t, err, "depth limit")
}

func TestFloatNumbers(t *testing.T) {
	wantEqual(t, outputs(t, `.[0] + .[1]`, `[0.1, 0.2]`), []string{`0.3`})
	vals, _ := mustCompile(t, `.[0] + .[1]`, WithFloatNumbers()).RunAll(t.Context(), mustDecode(t, `[0.1, 0.2]`))
	var buf bytes.Buffer
	enc := NewEncoder(&buf, EncodeOptions{FloatNumbers: true})
	_ = enc.Encode(vals[0])
	_ = enc.Flush()
	wantEqual(t, buf.String(), "0.30000000000000004\n")
}

func TestRegexBudgetReachesTheRun(t *testing.T) {
	b := regex.Budget{Steps: 10, MaxPattern: 20}
	q := mustCompile(t, `.`, WithRegexBudget(b))
	wantEqual(t, q.newCtx(t.Context()).RegexBudget, b)
	wantEqual(t, mustCompile(t, `.`).newCtx(t.Context()).RegexBudget, regex.Budget{})
}

func TestRegexBudgetBoundsTheRegexBuiltins(t *testing.T) {
	in := value.Str(strings.Repeat("a", 200))
	out, err := mustCompile(t, `test("a+b")`).RunAll(t.Context(), in)
	if err != nil {
		t.Fatalf("under the default budget: %v", err)
	}
	wantJSON(t, out[0], `false`)
	_, err = mustCompile(t, `test("a+b")`, WithRegexBudget(regex.Budget{Steps: 100})).RunAll(t.Context(), in)
	wantErrContains(t, err, "regex step budget exhausted for a+b")
}

func TestModulePathsAndOriginAreAccepted(t *testing.T) {
	q := mustCompile(t, `.`, WithModulePaths([]string{"lib"}), WithProgramOrigin("prog"))
	wantEqual(t, q.cfg.modulePaths, []string{"lib"})
	wantEqual(t, q.cfg.origin, "prog")
}

func TestModulesRunThroughOptions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.jq"), []byte(`def inc: . + 1;`), 0o600); err != nil {
		t.Fatal(err)
	}
	q := mustCompile(t, `import "m" as m; m::inc, get_search_list, get_prog_origin`, WithModulePaths([]string{dir}), WithProgramOrigin(dir))
	out, err := q.RunAll(t.Context(), value.Num(1))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, len(out), 3)
	wantJSON(t, out[0], `2`)
	wantJSON(t, out[1], `["`+dir+`"]`)
	wantJSON(t, out[2], `"`+dir+`"`)
	_, err = Compile(`import "missing" as m; 1`, WithModulePaths([]string{dir}))
	wantErrContains(t, err, "module not found: missing")
	meta := mustCompile(t, `"m" | modulemeta | .defs`, WithModulePaths([]string{dir}))
	out, err = meta.RunAll(t.Context(), value.Null())
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, out[0], `["inc/0"]`)
}

func TestModulesNeedModulePaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.jq"), []byte(`def inc: . + 1;`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Compile(`import "m" as m; m::inc`, WithProgramOrigin(dir))
	wantErrContains(t, err, "module not found: m")
	_, err = Compile(`include "m"; inc`)
	wantErrContains(t, err, "module not found: m")
	q := mustCompile(t, `get_search_list`)
	out, err := q.RunAll(t.Context(), value.Null())
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, out[0], `[]`)
}

func TestBuiltinsListsNameAndArity(t *testing.T) {
	names := mustCompile(t, `.`).Builtins()
	for _, want := range []string{"map/1", "path/1", "input/0", "halt_error/1"} {
		if !slices.Contains(names, want) {
			t.Errorf("Builtins lacks %s", want)
		}
	}
	for _, n := range names {
		if strings.HasPrefix(n, "_") || strings.HasPrefix(n, "@") {
			t.Errorf("Builtins lists the private name %s", n)
		}
	}
	names[0] = "changed"
	if mustCompile(t, `.`).Builtins()[0] == "changed" {
		t.Error("Builtins shares its slice between callers")
	}
}

func TestExitCodeTable(t *testing.T) {
	wantEqual(t, ExitCode(value.Bool(false), true, nil, true), 1)
	wantEqual(t, ExitCode(value.Null(), true, nil, true), 1)
	wantEqual(t, ExitCode(value.Bool(true), true, nil, true), 0)
	wantEqual(t, ExitCode(value.Bool(false), true, nil, false), 0)
	wantEqual(t, ExitCode(nil, false, nil, true), 4)
	wantEqual(t, ExitCode(nil, false, nil, false), 0)
	wantEqual(t, ExitCode(nil, false, &query.CompileError{}, false), 3)
	wantEqual(t, ExitCode(nil, false, &query.RuntimeError{}, false), 5)
	wantEqual(t, ExitCode(nil, false, &query.HaltError{Code: 7}, false), 7)
	wantEqual(t, ExitCode(value.Bool(true), true, &query.HaltError{Code: 0}, true), 0)
	wantEqual(t, ExitCode(nil, false, &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}, false), 2)
	wantEqual(t, ExitCode(value.Bool(true), true, errors.New("invalid input"), true), 5)
	wantEqual(t, ExitCode(nil, false, context.Canceled, false), 5)
	wantEqual(t, ExitCode(nil, false, fmt.Errorf("reading: %w", &fs.PathError{Op: "read", Path: "d", Err: errors.New("is a directory")}), true), 2)
	// A halt's code exits as it is under -e, and as 0 when negative
	// without it, while halt_error(-4) under -e reads as no output.
	wantEqual(t, ExitCode(nil, false, &query.HaltError{Code: -1}, false), 0)
	wantEqual(t, ExitCode(nil, false, &query.HaltError{Code: -1}, true), 1)
	wantEqual(t, ExitCode(nil, false, &query.HaltError{Code: -4}, false), 0)
	wantEqual(t, ExitCode(nil, false, &query.HaltError{Code: -4}, true), 4)
	wantEqual(t, ExitCode(value.Bool(true), true, &query.HaltError{Code: -4}, true), 0)
	wantEqual(t, ExitCode(value.Null(), true, &query.HaltError{Code: -4}, true), 1)
}

func TestRunTestFile(t *testing.T) {
	file := "# a comment\n.a\n{\"a\":1}\n1\n\n.b\n{\"b\":2}\n3\n\n%%FAIL\n.[\njq: error: unexpected end of input\n"
	var buf bytes.Buffer
	pass, fail, err := RunTestFile(strings.NewReader(file), &buf)
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, []int{pass, fail}, []int{2, 1})
	if !strings.HasSuffix(buf.String(), "2 of 3 tests passed (0 malformed, 0 skipped)\n") {
		t.Errorf("unexpected summary in %q", buf.String())
	}
}

// encode writes each JSON text in ins through an Encoder with opts.
func encode(t *testing.T, opts EncodeOptions, ins ...string) string {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf, opts)
	for _, in := range ins {
		if err := enc.Encode(mustDecode(t, in)); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestEncoderIndent(t *testing.T) {
	in := `{"a":[1]}`
	wantEqual(t, encode(t, EncodeOptions{}, in), "{\n  \"a\": [\n    1\n  ]\n}\n")
	wantEqual(t, encode(t, EncodeOptions{Indent: 1}, in), "{\n \"a\": [\n  1\n ]\n}\n")
	wantEqual(t, encode(t, EncodeOptions{Indent: -1}, in), "{\n\"a\": [\n1\n]\n}\n") // jq --indent 0
	wantEqual(t, encode(t, EncodeOptions{Tab: true}, in), "{\n\t\"a\": [\n\t\t1\n\t]\n}\n")
	wantEqual(t, encode(t, EncodeOptions{Compact: true}, in), "{\"a\":[1]}\n")
}

func TestEncoderRawForms(t *testing.T) {
	wantEqual(t, encode(t, EncodeOptions{Raw: true}, `"a"`, `[1]`), "a\n[\n  1\n]\n")
	wantEqual(t, encode(t, EncodeOptions{Join: true}, `"a"`, `"b"`), "ab")
	wantEqual(t, encode(t, EncodeOptions{RawNUL: true}, `"a"`), "a\x00")
	wantEqual(t, encode(t, EncodeOptions{ASCII: true, Raw: true}, `"é"`), "\"\\u00e9\"\n")
	wantEqual(t, encode(t, EncodeOptions{Seq: true, Compact: true}, `1`), "\x1e1\n")
	var buf bytes.Buffer
	err := NewEncoder(&buf, EncodeOptions{RawNUL: true}).Encode(value.Str("a\x00b"))
	wantErrContains(t, err, "NUL")
}

func TestEncoderSortsKeys(t *testing.T) {
	wantEqual(t, encode(t, EncodeOptions{Compact: true, SortKeys: "lexical"}, `{"b":1,"a":{"d":1,"c":2}}`), "{\"a\":{\"c\":2,\"d\":1},\"b\":1}\n")
	wantEqual(t, encode(t, EncodeOptions{Compact: true, SortKeys: "none"}, `{"b":1,"a":2}`), "{\"b\":1,\"a\":2}\n")
}

func TestEncoderColors(t *testing.T) {
	wantEqual(t, encode(t, EncodeOptions{Color: true}, `null`), "\x1b[0;90mnull\x1b[0m\n")
	wantEqual(t, encode(t, EncodeOptions{Color: true, Colors: "0;31"}, `null`), "\x1b[0;31mnull\x1b[0m\n")
	wantEqual(t, encode(t, EncodeOptions{Color: true, Colors: "0;31:zz"}, `null`), "\x1b[0;90mnull\x1b[0m\n") // jq falls back to its defaults
	wantErrContains(t, CheckColors("0;31:zz"), "zz")
	wantEqual(t, CheckColors("0;31:1;32"), nil)
}

func TestEncoderYAML(t *testing.T) {
	wantEqual(t, encode(t, EncodeOptions{Format: FormatYAML}, `{"a":[1,"yes"]}`, `"s"`), "a:\n  - 1\n  - \"yes\"\n---\n\"s\"\n")
	wantEqual(t, encode(t, EncodeOptions{Format: FormatYAML, Raw: true}, `"a: b"`), "a: b\n")
	wantEqual(t, encode(t, EncodeOptions{Format: FormatYAML, Indent: 4, SortKeys: "lexical"}, `{"b":{"y":1},"a":1}`), "a: 1\nb:\n    y: 1\n")
}

// flushCounter counts the writes that reach it.
type flushCounter struct{ writes int }

func (f *flushCounter) Write(p []byte) (int, error) {
	f.writes++
	return len(p), nil
}

func TestEncoderUnbuffered(t *testing.T) {
	for _, format := range []Format{FormatJSON, FormatYAML} {
		var w flushCounter
		enc := NewEncoder(&w, EncodeOptions{Format: format, Unbuffered: true})
		_ = enc.Encode(value.Num(1))
		_ = enc.Encode(value.Num(2))
		wantEqual(t, w.writes, 2)
		w = flushCounter{}
		enc = NewEncoder(&w, EncodeOptions{Format: format})
		_ = enc.Encode(value.Num(1))
		wantEqual(t, w.writes, 0)
	}
}
