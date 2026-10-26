package jq_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/jq"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/regex"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

func ExampleCompile() {
	q, _ := jq.Compile(`.items[] | select(.n > $min) | .name`, jq.WithArg("min", value.Num(1)))
	in, _ := jq.NewDecoder(strings.NewReader(`{"items":[{"n":1,"name":"a"},{"n":2,"name":"b"}]}`), jq.DecodeOptions{}).Next()
	for v, err := range q.Run(context.Background(), in) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(v.Str())
	}
	// Output: b
}

func ExampleNewEncoder() {
	enc := jq.NewEncoder(os.Stdout, jq.EncodeOptions{Compact: true, SortKeys: sortkeys.Lexical})
	_ = enc.Encode(value.Obj(value.Pair{Key: "b", Value: value.Num(1)}, value.Pair{Key: "a", Value: value.Num(2)}))
	_ = enc.Flush()
	// Output: {"a":2,"b":1}
}

func ExampleParse() {
	_, err := jq.Parse(`.a | `)
	var ce *query.CompileError
	fmt.Println(errors.As(err, &ce), ce.Line)
	// Output: true 1
}

func ExampleProgram() {
	p, err := jq.Parse(`. * $n`)
	if err != nil {
		log.Fatal(err)
	}
	for _, n := range []float64{2, 3} {
		q, err := p.Compile(jq.WithArg("n", value.Num(n)))
		if err != nil {
			log.Fatal(err)
		}
		out, _ := q.RunAll(context.Background(), value.Num(5))
		fmt.Println(out[0].Dec())
	}
	// Output:
	// 10
	// 15
}

func ExampleProgram_Compile() {
	p, _ := jq.Parse(`$x`)
	_, err := p.Compile()
	fmt.Println(err)
	q, _ := p.Compile(jq.WithArg("x", value.Str("bound")))
	out, _ := q.RunAll(context.Background(), value.Null())
	fmt.Println(out[0].Str())
	// Output:
	// $x is not defined at line 1, column 1
	// bound
}

func ExampleQuery() {
	q, _ := jq.Compile(`.n + 1`)
	for _, in := range []string{`{"n":1}`, `{"n":41}`} {
		v, _ := jq.NewDecoder(strings.NewReader(in), jq.DecodeOptions{}).Next()
		out, _ := q.RunAll(context.Background(), v)
		fmt.Println(out[0].Dec())
	}
	// Output:
	// 2
	// 42
}

func ExampleQuery_Run() {
	q, _ := jq.Compile(`range(10)`)
	for v, err := range q.Run(context.Background(), value.Null()) {
		if err != nil {
			log.Fatal(err)
		}
		if v.Float() > 2 {
			break // stopping here stops the program too
		}
		fmt.Println(v.Dec())
	}
	// Output:
	// 0
	// 1
	// 2
}

func ExampleQuery_RunAll() {
	q, _ := jq.Compile(`1, 2, error("stop"), 3`)
	out, err := q.RunAll(context.Background(), value.Null())
	fmt.Println(len(out), err)
	// Output: 2 stop
}

func ExampleQuery_Builtins() {
	q, _ := jq.Compile(`.`)
	names := q.Builtins()
	for _, n := range names {
		if n == "map/1" || n == "length/0" {
			fmt.Println(n)
		}
	}
	// Output:
	// length/0
	// map/1
}

func ExampleOption() {
	opts := []jq.Option{
		jq.WithArg("greeting", value.Str("hello")),
		jq.WithEnv([]string{"USER=ada"}),
	}
	q, _ := jq.Compile(`"\($greeting), \($ENV.USER)"`, opts...)
	out, _ := q.RunAll(context.Background(), value.Null())
	fmt.Println(out[0].Str())
	// Output: hello, ada
}

func ExampleWithArg() {
	q, _ := jq.Compile(`[$a, $ARGS.named.a]`, jq.WithArg("a", value.Str("first")), jq.WithArg("a", value.Str("second")))
	out, _ := q.RunAll(context.Background(), value.Null())
	enc := jq.NewEncoder(os.Stdout, jq.EncodeOptions{Compact: true})
	_ = enc.Encode(out[0])
	_ = enc.Flush()
	// Output: ["first","first"]
}

func ExampleWithPositional() {
	q, _ := jq.Compile(`$ARGS.positional | length`, jq.WithPositional([]*value.Value{value.Str("a"), value.Num(1)}))
	out, _ := q.RunAll(context.Background(), value.Null())
	fmt.Println(out[0].Dec())
	// Output: 2
}

func ExampleWithEnv() {
	q, _ := jq.Compile(`$ENV | keys`, jq.WithEnv([]string{"HOME=/home/ada", "PATH=/bin"}))
	out, _ := q.RunAll(context.Background(), value.Null())
	enc := jq.NewEncoder(os.Stdout, jq.EncodeOptions{Compact: true})
	_ = enc.Encode(out[0])
	_ = enc.Flush()
	// Output: ["HOME","PATH"]
}

func ExampleWithModulePaths() {
	dir, err := os.MkdirTemp("", "jqlib")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "double.jq"), []byte(`def twice: . * 2;`), 0o600); err != nil {
		fmt.Println(err)
		return
	}
	q, err := jq.Compile(`import "double" as d; d::twice`, jq.WithModulePaths([]string{dir}))
	if err != nil {
		fmt.Println(err)
		return
	}
	out, _ := q.RunAll(context.Background(), value.Num(21))
	fmt.Println(out[0].Dec())
	// Output: 42
}

func ExampleWithUnconfinedModules() {
	dir, err := os.MkdirTemp("", "jqlib")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "double.jq"), []byte(`def twice: . * 2;`), 0o600); err != nil {
		fmt.Println(err)
		return
	}
	// The listed directory is empty, and the program names the directory
	// of the module itself. A confined search ignores that.
	prog := `import "double" as d {search: ` + strconv.Quote(dir) + `}; d::twice`
	_, err = jq.Compile(prog, jq.WithModulePaths([]string{os.TempDir() + "/nowhere"}))
	fmt.Println(err)
	q, err := jq.Compile(prog, jq.WithModulePaths([]string{os.TempDir() + "/nowhere"}), jq.WithUnconfinedModules())
	if err != nil {
		fmt.Println(err)
		return
	}
	out, _ := q.RunAll(context.Background(), value.Num(21))
	fmt.Println(out[0].Dec())
	// Output:
	// module not found: double
	// 42
}

func ExampleWithModulePaths_default() {
	_, err := jq.Compile(`import "double" as d; d::twice`)
	fmt.Println(err)
	// Output: module not found: double
}

func ExampleWithAutoInclude() {
	dir, err := os.MkdirTemp("", "jqprelude")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "prelude.jq")
	if err := os.WriteFile(path, []byte(`def hello: "hi";`), 0o600); err != nil {
		fmt.Println(err)
		return
	}
	q, err := jq.Compile(`hello`, jq.WithAutoInclude(path))
	if err != nil {
		fmt.Println(err)
		return
	}
	out, _ := q.RunAll(context.Background(), value.Null())
	fmt.Println(out[0].Str())
	// Output: hi
}

func ExampleWithProgramOrigin() {
	_, err := jq.Compile(`.`, jq.WithModulePaths([]string{"./lib"}), jq.WithProgramOrigin("/srv/filters"))
	fmt.Println(err)
	// Output: <nil>
}

func ExampleWithInputs() {
	dec := jq.NewDecoder(strings.NewReader(`1 2 3`), jq.DecodeOptions{})
	q, _ := jq.Compile(`reduce inputs as $n (0; . + $n)`, jq.WithInputs(dec.Next))
	out, _ := q.RunAll(context.Background(), value.Null())
	fmt.Println(out[0].Dec())
	// Output: 6
}

func ExampleDecodeOptions_stream() {
	dec := jq.NewDecoder(strings.NewReader(`{"a":[1,2]}`), jq.DecodeOptions{Stream: true})
	enc := jq.NewEncoder(os.Stdout, jq.EncodeOptions{Compact: true})
	for {
		v, err := dec.Next()
		if err != nil {
			break
		}
		_ = enc.Encode(v)
	}
	_ = enc.Flush()
	// Output:
	// [["a",0],1]
	// [["a",1],2]
	// [["a",1]]
	// [["a"]]
}

func ExampleWithInputFilename() {
	q, _ := jq.Compile(`input_filename`, jq.WithInputFilename(func() string { return "config.json" }))
	out, _ := q.RunAll(context.Background(), value.Null())
	fmt.Println(out[0].Str())
	// Output: config.json
}

func ExampleWithInputLine() {
	dec := jq.NewDecoder(strings.NewReader("1\n2\n"), jq.DecodeOptions{})
	q, _ := jq.Compile(`[input, input_line_number]`, jq.WithInputs(dec.Next), jq.WithInputLine(dec.Line))
	out, _ := q.RunAll(context.Background(), value.Null())
	fmt.Println(out[0].Elems()[1].Dec())
	// Output: 1
}

func ExampleWithStderr() {
	var stderr bytes.Buffer
	q, _ := jq.Compile(`debug("checking") | . + 1`, jq.WithStderr(&stderr))
	out, _ := q.RunAll(context.Background(), value.Num(1))
	fmt.Print(out[0].Dec(), " ", stderr.String())
	// Output: 2 ["DEBUG:","checking"]
}

func ExampleWithRegexBudget() {
	// The budget bounds the regular expressions a run compiles and
	// matches. This program uses none, so it runs as it would without one.
	q, err := jq.Compile(`. + 1`, jq.WithRegexBudget(regex.Budget{Steps: 1000, MaxPattern: 256}))
	if err != nil {
		log.Fatal(err)
	}
	out, _ := q.RunAll(context.Background(), value.Num(1))
	fmt.Println(out[0].Dec())
	// Output: 2
}

func ExampleWithDepthLimit() {
	q, _ := jq.Compile(`def nest: [nest]; nest`, jq.WithDepthLimit(50))
	_, err := q.RunAll(context.Background(), value.Null())
	fmt.Println(err)
	// Output: exceeds depth limit of 50
}

func ExampleWithFloatNumbers() {
	for _, opts := range [][]jq.Option{nil, {jq.WithFloatNumbers()}} {
		q, _ := jq.Compile(`0.1 + 0.2`, opts...)
		out, _ := q.RunAll(context.Background(), value.Null())
		enc := jq.NewEncoder(os.Stdout, jq.EncodeOptions{FloatNumbers: len(opts) > 0})
		_ = enc.Encode(out[0])
		_ = enc.Flush()
	}
	// Output:
	// 0.3
	// 0.30000000000000004
}

func ExampleFormat() {
	for _, f := range []jq.Format{jq.FormatAuto, jq.FormatYAML} {
		v, _ := jq.NewDecoder(strings.NewReader("a: 1\n"), jq.DecodeOptions{Format: f}).Next()
		fmt.Println(v.Keys())
	}
	_, err := jq.NewDecoder(strings.NewReader("a: 1\n"), jq.DecodeOptions{Format: jq.FormatJSON}).Next()
	fmt.Println(err != nil)
	// Output:
	// [a]
	// [a]
	// true
}

func ExampleDecodeOptions() {
	dec := jq.NewDecoder(strings.NewReader("one\ntwo\n"), jq.DecodeOptions{Raw: true, Filename: "words.txt"})
	for {
		v, err := dec.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		fmt.Println(v.Str())
	}
	// Output:
	// one
	// two
}

func ExampleDecoder() {
	dec := jq.NewDecoder(strings.NewReader(`1 [2] {"three":3}`), jq.DecodeOptions{})
	for {
		v, err := dec.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(v.Kind())
	}
	// Output:
	// 3
	// 5
	// 6
}

func ExampleNewDecoder() {
	dec := jq.NewDecoder(strings.NewReader("---\nname: web\n---\nname: db\n"), jq.DecodeOptions{})
	for {
		v, err := dec.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		name, _ := v.Get("name")
		fmt.Println(name.Str())
	}
	// Output:
	// web
	// db
}

func ExampleDecoder_Next() {
	dec := jq.NewDecoder(strings.NewReader("1\n{"), jq.DecodeOptions{Filename: "in.json"})
	v, err := dec.Next()
	fmt.Println(v.Dec(), err)
	_, err = dec.Next()
	fmt.Println(err != nil)
	// Output:
	// 1 <nil>
	// true
}

func ExampleDecoder_Line() {
	dec := jq.NewDecoder(strings.NewReader("a\nb\nc\n"), jq.DecodeOptions{Raw: true})
	_, _ = dec.Next()
	_, _ = dec.Next()
	fmt.Println(dec.Line())
	// Output: 2
}

func ExampleSlurp() {
	all := jq.Slurp(
		jq.NewDecoder(strings.NewReader(`1 2`), jq.DecodeOptions{Filename: "a.json"}),
		jq.NewDecoder(strings.NewReader(`3`), jq.DecodeOptions{Filename: "b.json"}),
	)
	v, _ := all.Next()
	_, err := all.Next()
	fmt.Println(v.Len(), errors.Is(err, io.EOF))
	// Output: 3 true
}

func ExampleEncodeOptions() {
	v := value.Obj(value.Pair{Key: "msg", Value: value.Str("héllo")})
	enc := jq.NewEncoder(os.Stdout, jq.EncodeOptions{Tab: true, ASCII: true})
	_ = enc.Encode(v)
	_ = enc.Flush()
	// Output:
	// {
	// 	"msg": "h\u00e9llo"
	// }
}

func ExampleEncoder() {
	enc := jq.NewEncoder(os.Stdout, jq.EncodeOptions{Format: jq.FormatYAML})
	_ = enc.Encode(value.Obj(value.Pair{Key: "ports", Value: value.Arr(value.Num(80), value.Num(443))}))
	_ = enc.Flush()
	// Output:
	// ports:
	//   - 80
	//   - 443
}

func ExampleEncoder_Encode() {
	enc := jq.NewEncoder(os.Stdout, jq.EncodeOptions{Raw: true})
	_ = enc.Encode(value.Str("plain text"))
	_ = enc.Encode(value.Arr(value.Str("quoted")))
	_ = enc.Flush()
	// Output:
	// plain text
	// [
	//   "quoted"
	// ]
}

func ExampleEncoder_Flush() {
	var buf bytes.Buffer
	enc := jq.NewEncoder(&buf, jq.EncodeOptions{Compact: true})
	_ = enc.Encode(value.Num(1))
	fmt.Printf("%q ", buf.String())
	_ = enc.Flush()
	fmt.Printf("%q\n", buf.String())
	// Output: "" "1\n"
}

func ExampleCheckColors() {
	fmt.Println(jq.CheckColors("0;31:0;32"))
	fmt.Println(jq.CheckColors("0;31:purple") != nil)
	// Output:
	// <nil>
	// true
}

func ExampleExitCode() {
	q, _ := jq.Compile(`.ok`)
	in, _ := jq.NewDecoder(strings.NewReader(`{"ok":false}`), jq.DecodeOptions{}).Next()
	out, err := q.RunAll(context.Background(), in)
	fmt.Println(jq.ExitCode(out[len(out)-1], len(out) > 0, err, true))
	_, err = jq.Compile(`.[`)
	fmt.Println(jq.ExitCode(nil, false, err, false))
	// Output:
	// 1
	// 3
}

// A run over several inputs hands ExitCode the last output of the last
// input that ended without an error, and the error, if any, that ended
// the last input. Here the first input fails and the second ends on
// true, so -e exits 0, as jq does.
func ExampleExitCode_inputs() {
	q, _ := jq.Compile(`.ok`)
	dec := jq.NewDecoder(strings.NewReader(`[] {"ok":true}`), jq.DecodeOptions{})
	var last *value.Value
	var err error
	for in, derr := dec.Next(); derr == nil; in, derr = dec.Next() {
		var out []*value.Value
		out, err = q.RunAll(context.Background(), in)
		if err == nil && len(out) > 0 {
			last = out[len(out)-1]
		}
	}
	fmt.Println(jq.ExitCode(last, last != nil, err, true))
	// Output:
	// 0
}

func ExampleRunTestFile() {
	tests := "# addition\n. + 1\n1\n2\n\n# a failing test\n.a\n{\"a\":1}\n2\n"
	var report bytes.Buffer
	passed, failed, err := jq.RunTestFile(strings.NewReader(tests), &report)
	fmt.Println(passed, failed, err)
	// Output: 1 1 <nil>
}
