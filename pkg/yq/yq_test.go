package yq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/regex"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

func TestFileOpsDisabledIsTypedError(t *testing.T) {
	ev := mustCompile(t, `load("x.yaml")`, WithFileOps(false))
	_, err := collect(ev.Eval(context.Background(), value.Null()))
	var re *query.RuntimeError
	wantErrAs(t, err, &re)
	wantContains(t, re.Msg, "file operations have been disabled")
}

func TestEnvOpsDisabledIsTypedError(t *testing.T) {
	ev := mustCompile(t, `env(HOME)`, WithEnvOps(false))
	_, err := collect(ev.Eval(context.Background(), value.Null()))
	var re *query.RuntimeError
	wantErrAs(t, err, &re)
	wantContains(t, re.Msg, "env operations have been disabled")
}

func TestEnvOpsAreOnByDefaultAndReadWithEnv(t *testing.T) {
	out, err := collect(mustCompile(t, `env(GREETING)`, WithEnv([]string{"GREETING=hi", "junk"})).Eval(context.Background(), value.Null()))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, out[0].Str(), "hi")
}

func TestParseReportsCompileErrorWithPosition(t *testing.T) {
	_, err := Parse("a:\n  .b | |")
	var ce *query.CompileError
	wantErrAs(t, err, &ce)
	wantEqual(t, ce.Line > 0, true)
	_, err = Compile(`.a | | .b`)
	wantErrAs(t, err, &ce)
}

func TestExpressionCompileMatchesCompile(t *testing.T) {
	ex, err := Parse(`.a + 1`)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := ex.Compile()
	if err != nil {
		t.Fatal(err)
	}
	out, err := collect(ev.Eval(context.Background(), decodeOne(t, "a: 1\n")))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, out[0].Float(), 2.0)
}

func TestEvalAllSeesEveryDocument(t *testing.T) {
	docs := decodeAll(t, "a: 1\n---\na: 2\n")
	out, _ := collect(mustCompile(t, `[.a] | .[0] + .[1]`).EvalAll(context.Background(), docs))
	wantEqual(t, out[0].Float(), 3.0) // every document is a candidate, so .a yields both
	out, _ = collect(mustCompile(t, `[.[] | .a]`).EvalAll(context.Background(), docs))
	wantEqual(t, out[0].Len(), 0) // .[] on each map yields its scalars, and .a on a scalar is empty
	out, _ = collect(mustCompile(t, `[.a]`).Eval(context.Background(), docs[0]))
	wantEqual(t, out[0].Len(), 1)
}

func TestEvaluatorRunsRepeatedlyAndConcurrently(t *testing.T) {
	ev := mustCompile(t, `.a = .a + 1`)
	for range 3 {
		got := render(t, `.a = .a + 1`, "a: 1\n", false)
		wantEqual(t, got, "a: 2\n")
	}
	// The goroutines only run and report. Every assertion happens here,
	// because a goroutine must not call t.Fatal.
	type result struct {
		name string
		err  error
	}
	docs := make([]*value.Value, 4)
	for i := range docs {
		docs[i] = decodeOne(t, "a: 5\n")
	}
	done := make(chan result, len(docs))
	for _, doc := range docs {
		go func() {
			out, err := collect(ev.Eval(context.Background(), doc))
			if err != nil {
				done <- result{err: fmt.Errorf("eval after %d outputs: %w", len(out), err)}
				return
			}
			if len(out) != 1 {
				done <- result{err: fmt.Errorf("got %d outputs", len(out))}
				return
			}
			done <- result{name: out[0].Meta().Filename}
		}()
	}
	for range docs {
		r := <-done
		if r.err != nil {
			t.Error(r.err)
		}
		wantEqual(t, r.name, "in.yaml")
	}
}

func TestStoppingTheRangeStopsTheRun(t *testing.T) {
	n := 0
	for _, err := range mustCompile(t, `.[]`).Eval(context.Background(), decodeOne(t, "[1, 2, 3]\n")) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		break
	}
	wantEqual(t, n, 1)
}

func TestCancelledContextEndsTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	big := value.NewArrBuilder(200000)
	for range 200000 {
		big.Append(value.Num(1))
	}
	_, err := collect(mustCompile(t, `[.[] | . + 1] | length`).Eval(ctx, big.Seal()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestFloatNumbersRestoresDoubles(t *testing.T) {
	wantEqual(t, render(t, `.a + .b`, "a: 0.1\nb: 0.2\n", false), "0.3\n")
	wantEqual(t, render(t, `.a + .b`, "a: 0.1\nb: 0.2\n", false, WithFloatNumbers()), "0.30000000000000004\n")
}

func TestRegexBudgetBoundsMatching(t *testing.T) {
	ev := mustCompile(t, `test("abcdef")`, WithRegexBudget(regex.Budget{MaxPattern: 3}))
	_, err := collect(ev.Eval(context.Background(), value.Str("abcdef")))
	if err == nil {
		t.Fatal("a pattern longer than the budget was accepted")
	}
	out, err := collect(mustCompile(t, `test("abcdef")`).Eval(context.Background(), value.Str("abcdef")))
	if err != nil || out[0].Kind() != value.KindTrue {
		t.Fatalf("without a budget got %v, %v", out, err)
	}
}

func TestMergeAnchorAndInterpolationOptionsCompile(t *testing.T) {
	mustCompile(t, `.`, WithMergeAnchorFix(true), WithStringInterpolation(false), WithHeaderPreprocess(false), WithSystemOps(true))
}

func TestOptionErrorStopsCompile(t *testing.T) {
	_, err := Compile(`.`, func(*Config) error { return context.Canceled })
	wantEqual(t, err, context.Canceled)
}

func TestSplitDocOutputsCarryTheirDocumentIndex(t *testing.T) {
	wantEqual(t, render(t, `.[] | split_doc`, "- a: 1\n- b: 2\n", false), "a: 1\n---\nb: 2\n")
}

func TestEvalKeepsCommentsAndStyles(t *testing.T) {
	got := render(t, `.a = 2 | .b += ["y"]`, "# top\na: 1 # keep\nb: [x]\n", false)
	wantEqual(t, got, "# top\na: 2 # keep\nb: [x, y]\n")
}

func TestMultiDocumentRoundTripKeepsSeparators(t *testing.T) {
	in := "a: 1\n---\na: 2\n"
	wantEqual(t, render(t, `.`, in, false), in)
	wantEqual(t, render(t, `.`, in, true), in)
}

func TestAMissingKeyInALaterDocumentPrintsNull(t *testing.T) {
	in := "a: 1\n---\nb: 2\n---\nc: 3\n"
	wantEqual(t, render(t, `.a`, in, false), "1\n---\nnull\n---\nnull\n")
}

func TestAnEmptyLaterDocumentPrintsABlankLine(t *testing.T) {
	wantEqual(t, render(t, `.`, "a: 1\n---\n", false), "a: 1\n---\n\n")
	wantEqual(t, render(t, `.`, "a: 1\n--- # x\n", false), "a: 1\n---\n\n")
	wantEqual(t, render(t, `.`, "a: 1\n---\n# c\n---\n", false), "a: 1\n---\n\n---\n\n")
	wantEqual(t, render(t, `.`, "---\n", false), "---\n")
}

// compact runs src over the one document in and returns its outputs as
// JSON on one line each.
func compact(t *testing.T, src, in string) string {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf, EncodeOptions{Format: FormatJSON, Indent: -1})
	out, err := collect(mustCompile(t, src).Eval(t.Context(), decodeOne(t, in)))
	if err != nil {
		t.Fatalf("running %s: %v", src, err)
	}
	for _, v := range out {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestIsKeyMarksOnlyMapKeys(t *testing.T) {
	wantEqual(t, compact(t, `[... | is_key]`, "a: b\n"), "[false,true,false]\n")
	wantEqual(t, compact(t, `[.[] | is_key]`, "- x\n"), "[false]\n")
	wantEqual(t, compact(t, `[.a | key | is_key]`, "a: b\n"), "[true]\n")
}

func TestTildeIsANullLiteral(t *testing.T) {
	wantEqual(t, compact(t, `[~, .b == ~, ~ == null, ~ | type]`, "a: 1\n"), "[null,true,true,\"!!null\"]\n")
	wantEqual(t, render(t, `.x = ~`, "a: 1\n", false), "a: 1\nx: ~\n")
}

func TestAddingNullToASequenceAddsNothing(t *testing.T) {
	in := "a: [1, 2]\n"
	wantEqual(t, render(t, `.a + null`, in, false), "[1, 2]\n")
	wantEqual(t, render(t, `.a + .missing`, in, false), "[1, 2]\n")
	wantEqual(t, render(t, `.a + 3`, in, false), "[1, 2, 3]\n")
}

func TestSubtractingSequencesKeepsTheLeftStyle(t *testing.T) {
	wantEqual(t, render(t, `.a - .b`, "a: [1, 2, 3, 2]\nb: [2]\n", false), "[1, 3]\n")
	wantEqual(t, render(t, `.a - .b`, "a: [[1], [2]]\nb: [[1]]\n", false), "[[2]]\n")
	wantEqual(t, render(t, `.a - .b`, "a:\n  - 1\n  - 2\nb: [2]\n", false), "- 1\n")
}

func TestBase64URLEncodesWithTheURLAlphabet(t *testing.T) {
	in := "s: \"??>>\"\nt: \"?\"\nu: Pz8-Pg\nv: Pz8-Pg==\n"
	wantEqual(t, render(t, `(.s, .t) | @base64url`, in, false), "Pz8-Pg==\nPw==\n")
	wantEqual(t, render(t, `(.u, .v) | @base64urld`, in, false), "??>>\n??>>\n")
}

func TestEqualityComparesScalarTextWithWildcards(t *testing.T) {
	in := "- 3\n- 3.0\n- \"3\"\n- cat\n- camel\n- dog\n"
	wantEqual(t, compact(t, `[.[] | . == 3]`, in), "[true,false,true,false,false,false]\n")
	wantEqual(t, compact(t, `[.[] | select(. == "ca*")]`, in), "[\"cat\",\"camel\"]\n")
	wantEqual(t, compact(t, `[.[] | select(. == "*m?l")]`, in), "[\"camel\"]\n")
	wantEqual(t, compact(t, `[.[] | . != "*a*"]`, in), "[true,true,true,false,false,true]\n")
	wantEqual(t, compact(t, `[null == null, null == 1, [1] == [1], {} == {}]`, "a: 1\n"), "[true,false,false,false]\n")
}

func TestOrderingFollowsYQ(t *testing.T) {
	wantEqual(t, compact(t, `[null > null, null >= null, null < 1, 1 < null, 2 > 1.5, "b" >= "a", 3 <= 3]`, "a: 1\n"),
		"[false,true,false,false,true,true,true]\n")
	for src, msg := range map[string]string{
		`[1] < [2]`:    "arrays not yet supported for comparison",
		`{} < {}`:      "maps not yet supported for comparison",
		`true < false`: "!!bool not yet supported for comparison",
		`"a" < 1`:      "!!str not yet supported for comparison",
	} {
		_, err := collect(mustCompile(t, src).Eval(t.Context(), decodeOne(t, "a: 1\n")))
		if err == nil {
			t.Errorf("%s compared without an error", src)
			continue
		}
		wantContains(t, err.Error(), msg)
	}
}

func TestSortKeysOnEncode(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf, EncodeOptions{SortKeys: sortkeys.Lexical})
	_ = enc.Encode(decodeOne(t, "b: 1\na: 2\n"))
	_ = enc.Flush()
	wantEqual(t, buf.String(), "a: 2\nb: 1\n")
}

func TestStringInterpolationOffKeepsTheEscape(t *testing.T) {
	in := decodeOne(t, "a: 1\n")
	for _, ev := range []*Evaluator{
		mustCompile(t, `"x\(.a)"`, WithStringInterpolation(false)),
		mustParse(t, `"x\(.a)"`).mustCompile(t, WithStringInterpolation(false)),
	} {
		out, err := collect(ev.Eval(context.Background(), in))
		if err != nil {
			t.Fatal(err)
		}
		wantEqual(t, out[0].Str(), `x\(.a)`)
	}
	out, err := collect(mustCompile(t, `"x\(.a)"`).Eval(context.Background(), in))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, out[0].Str(), "x1")
}

func mustParse(t *testing.T, src string) *parsed {
	t.Helper()
	x, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return &parsed{x}
}

type parsed struct{ *Expression }

func (p *parsed) mustCompile(t *testing.T, opts ...Option) *Evaluator {
	t.Helper()
	ev, err := p.Compile(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}
