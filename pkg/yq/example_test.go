package yq_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/regex"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
	"github.com/fivetwenty-io/graft/pkg/yq"
)

// decode reads the first document of text.
func decode(text string) *value.Value {
	doc, err := yq.NewDecoder(strings.NewReader(text), yq.DecodeOptions{}).Next()
	if err != nil {
		log.Fatal(err)
	}
	return doc
}

// show runs ev on doc and prints each output as YAML.
func show(ev *yq.Evaluator, doc *value.Value) {
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{UnwrapScalar: true})
	for v, err := range ev.Eval(context.Background(), doc) {
		if err != nil {
			log.Fatal(err)
		}
		_ = enc.Encode(v)
	}
	_ = enc.Flush()
}

func ExampleCompile() {
	doc, _ := yq.NewDecoder(strings.NewReader("a: 1 # keep\nb: [x]\n"), yq.DecodeOptions{}).Next()
	ev, _ := yq.Compile(`.a = 2 | .b += ["y"]`)
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{})
	for v, err := range ev.Eval(context.Background(), doc) {
		if err != nil {
			log.Fatal(err)
		}
		_ = enc.Encode(v)
	}
	_ = enc.Flush()
	// Output:
	// a: 2 # keep
	// b: [x, y]
}

func ExampleParse() {
	_, err := yq.Parse(`.a | | .b`)
	var ce *query.CompileError
	fmt.Println(errors.As(err, &ce), ce.Line)
	// Output: true 1
}

func ExampleExpression() {
	x, err := yq.Parse(`.name`)
	if err != nil {
		log.Fatal(err)
	}
	ev, _ := x.Compile()
	show(ev, decode("name: graft\n"))
	// Output: graft
}

func ExampleExpression_Compile() {
	x, _ := yq.Parse(`strenv(WHO)`)
	for _, who := range []string{"ada", "grace"} {
		ev, _ := x.Compile(yq.WithEnv([]string{"WHO=" + who}))
		show(ev, value.Null())
	}
	// Output:
	// ada
	// grace
}

func ExampleEvaluator() {
	ev, _ := yq.Compile(`.n * 2`)
	for _, text := range []string{"n: 2\n", "n: 5\n"} {
		show(ev, decode(text))
	}
	// Output:
	// 4
	// 10
}

func ExampleEvaluator_Eval() {
	ev, _ := yq.Compile(`.items[]`)
	for v, err := range ev.Eval(context.Background(), decode("items: [a, b, c]\n")) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(v.Str())
		break // stopping the range stops the run
	}
	// Output: a
}

func ExampleEvaluator_EvalWith() {
	ev, _ := yq.Compile(`.n + $index`)
	vars := map[string]*value.Value{"index": value.Num(3)}
	for v, err := range ev.EvalWith(context.Background(), decode("n: 4\n"), vars) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(v.Float())
	}
	// Output: 7
}

func ExampleEvaluator_EvalAll() {
	dec := yq.NewDecoder(strings.NewReader("a: 1\n---\na: 2\n"), yq.DecodeOptions{})
	var docs []*value.Value
	for {
		doc, err := dec.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		docs = append(docs, doc)
	}
	ev, _ := yq.Compile(`[.a]`)
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{})
	for v, err := range ev.EvalAll(context.Background(), docs) {
		if err != nil {
			log.Fatal(err)
		}
		_ = enc.Encode(v)
	}
	_ = enc.Flush()
	// Output:
	// - 1
	// - 2
}

func ExampleConfig() {
	// An Option receives the Config and changes one setting, so a caller
	// writes its own the same way the package's do.
	var calls int
	_, _ = yq.Compile(`.`,
		func(*yq.Config) error { calls++; return nil },
		func(*yq.Config) error { calls += 10; return nil })
	fmt.Println(calls)
	// Output: 11
}

func ExampleOption() {
	_, err := yq.Compile(`.`, func(*yq.Config) error { return errors.New("no thanks") })
	fmt.Println(err)
	// Output: no thanks
}

func ExampleWithEnv() {
	ev, _ := yq.Compile(`strenv(GREETING)`, yq.WithEnv([]string{"GREETING=hello"}))
	show(ev, value.Null())
	// Output: hello
}

func ExampleWithEnvOps() {
	ev, _ := yq.Compile(`strenv(HOME)`, yq.WithEnvOps(false))
	for _, err := range ev.Eval(context.Background(), value.Null()) {
		fmt.Println(err)
	}
	// Output: env operations have been disabled
}

func ExampleWithFileOps() {
	ev, _ := yq.Compile(`load("other.yaml")`, yq.WithFileOps(false))
	for _, err := range ev.Eval(context.Background(), value.Null()) {
		fmt.Println(err)
	}
	// Output: file operations have been disabled
}

func ExampleWithSystemOps() {
	// The setting is recorded and nothing reads it, because this engine
	// has no system operator.
	ev, err := yq.Compile(`.`, yq.WithSystemOps(true))
	fmt.Println(ev != nil, err)
	// Output: true <nil>
}

func ExampleWithFloatNumbers() {
	doc := decode("a: 0.1\nb: 0.2\n")
	exact, _ := yq.Compile(`.a + .b`)
	float, _ := yq.Compile(`.a + .b`, yq.WithFloatNumbers())
	show(exact, doc)
	show(float, doc)
	// Output:
	// 0.3
	// 0.30000000000000004
}

func ExampleWithStringInterpolation() {
	ev, err := yq.Compile(`.`, yq.WithStringInterpolation(false))
	fmt.Println(ev != nil, err)
	// Output: true <nil>
}

func ExampleWithHeaderPreprocess() {
	// The setting is kept for the release that completes graft yq.
	ev, err := yq.Compile(`.`, yq.WithHeaderPreprocess(false))
	fmt.Println(ev != nil, err)
	// Output: true <nil>
}

func ExampleWithResolvedAliases() {
	ev, _ := yq.Compile(`.b`, yq.WithResolvedAliases())
	doc := decode("a: &x {k: 1}\nb: *x\n")
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{Format: yq.FormatJSON, Indent: -1})
	for v, err := range ev.Eval(context.Background(), doc) {
		if err != nil {
			log.Fatal(err)
		}
		_ = enc.Encode(v)
	}
	_ = enc.Flush()
	// Output: {"k":1}
}

func ExampleWithMergeAnchorFix() {
	ev, err := yq.Compile(`.`, yq.WithMergeAnchorFix(true))
	fmt.Println(ev != nil, err)
	// Output: true <nil>
}

func ExampleWithStderr() {
	var buf bytes.Buffer
	ev, err := yq.Compile(`.`, yq.WithStderr(&buf))
	fmt.Println(ev != nil, err, buf.Len())
	// Output: true <nil> 0
}

func ExampleWithRegexBudget() {
	// A pattern longer than the budget allows is refused.
	ev, _ := yq.Compile(`test("abcdef")`, yq.WithRegexBudget(regex.Budget{MaxPattern: 3}))
	for _, err := range ev.Eval(context.Background(), value.Str("abcdef")) {
		fmt.Println(err != nil)
	}
	// Output: true
}

func ExampleFormat() {
	doc, _ := yq.NewDecoder(strings.NewReader(`{"a": [1, 2]}`), yq.DecodeOptions{Format: yq.FormatJSON}).Next()
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{Format: yq.FormatYAML})
	_ = enc.Encode(doc)
	_ = enc.Flush()
	// Output:
	// a:
	//   - 1
	//   - 2
}

func ExampleDocument() {
	dec := yq.NewDecoder(strings.NewReader("a: 1\n---\na: 2\n"), yq.DecodeOptions{Filename: "in.yaml", FileIndex: 4})
	_, _ = dec.Next()
	second, _ := dec.Next()
	m := second.Meta()
	d := yq.Document{Value: second, Index: m.DocIndex, FileIndex: m.FileIndex, Filename: m.Filename}
	fmt.Println(d.Index, d.FileIndex, d.Filename)
	// Output: 1 4 in.yaml
}

func ExampleDecodeOptions() {
	dec := yq.NewDecoder(strings.NewReader("a: 1\n"), yq.DecodeOptions{Filename: "x.yaml", FileIndex: 2})
	doc, _ := dec.Next()
	fmt.Println(doc.Meta().Filename, doc.Meta().FileIndex)
	// Output: x.yaml 2
}

func ExampleNewDecoder() {
	dec := yq.NewDecoder(strings.NewReader("a: 1 # one\n---\na: 2\n"), yq.DecodeOptions{})
	for {
		doc, err := dec.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		a, _ := doc.Get("a")
		fmt.Printf("document %d: a=%s comment=%q\n", doc.Meta().DocIndex, a.Dec(), a.Meta().LineComment)
	}
	// Output:
	// document 0: a=1 comment="# one"
	// document 1: a=2 comment=""
}

func ExampleDecoder() {
	dec := yq.NewDecoder(strings.NewReader("- &x 1\n- *x\n"), yq.DecodeOptions{})
	doc, _ := dec.Next()
	first, _ := doc.Index(0)
	fmt.Println(first.Meta().Anchor)
	// Output: x
}

func ExampleDecoder_Next() {
	dec := yq.NewDecoder(strings.NewReader("a: [1\n"), yq.DecodeOptions{})
	_, err := dec.Next()
	_, again := dec.Next()
	fmt.Println(err != nil, errors.Is(again, io.EOF), errors.Is(again, err))
	// Output: true false true
}

func ExampleEncodeOptions() {
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{Format: yq.FormatJSON, Indent: -1, SortKeys: sortkeys.Lexical})
	_ = enc.Encode(decode("b: 1\na: [x]\n"))
	_ = enc.Flush()
	// Output: {"a":["x"],"b":1}
}

func ExampleNewEncoder() {
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{Indent: 4, NoDocSeparators: true})
	_ = enc.Encode(decode("a:\n  b: 1\n"))
	_ = enc.Encode(decode("c: 2\n"))
	_ = enc.Flush()
	// Output:
	// a:
	//     b: 1
	// c: 2
}

func ExampleEncoder() {
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{PrettyPrint: true})
	_ = enc.Encode(decode("{a: 'x', b: [1, 2]}\n"))
	_ = enc.Flush()
	// Output:
	// a: x
	// b:
	//   - 1
	//   - 2
}

func ExampleEncoder_Encode() {
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{})
	for _, doc := range []string{"a: 1\n", "b: 2\n"} {
		ev, _ := yq.Compile(`.`)
		for v, err := range ev.Eval(context.Background(), decode(doc)) {
			if err != nil {
				log.Fatal(err)
			}
			_ = enc.Encode(v)
		}
	}
	_ = enc.Flush()
	// Output:
	// a: 1
	// b: 2
}

func ExampleEncoder_Flush() {
	var buf bytes.Buffer
	enc := yq.NewEncoder(&buf, yq.EncodeOptions{})
	_ = enc.Encode(decode("a: 1\n"))
	fmt.Println(buf.Len())
	_ = enc.Flush()
	fmt.Println(buf.Len())
	// Output:
	// 0
	// 5
}

func ExampleExitCode() {
	// -e over a run that printed only false, over one that printed a truthy
	// value among others, over one that printed nothing, and over one that
	// failed without -e.
	fmt.Println(
		yq.ExitCode(true, false, nil, true),
		yq.ExitCode(true, true, nil, true),
		yq.ExitCode(false, false, nil, true),
		yq.ExitCode(false, false, errors.New("boom"), false),
	)
	// Output: 1 0 1 1
}

func ExampleSplitFrontMatter() {
	page := "---\ntitle: Hello\n---\n# The page\n"
	head, rest, found, err := yq.SplitFrontMatter(strings.NewReader(page))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%q\n%q\n%v\n", head, rest, found)
	// Output:
	// "---\ntitle: Hello\n"
	// "---\n# The page\n"
	// true
}
