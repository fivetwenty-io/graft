package suite

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/eval"
	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/jqout"
	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Result is how one case went. Got holds the outputs the program produced
// as compact JSON, and Err says what was wrong when the case did not pass.
// Malformed marks a case that could not be judged as a test, such as a
// program that does not compile or a case with too few results, and it
// also counts as a failure. Skipped marks a case Run did not run.
type Result struct {
	Case      Case
	Pass      bool
	Got       []string
	Err       string
	Malformed bool
	Skipped   bool
}

// Run runs every case under dialect d, writes jq's line for each case and
// each failure to w, then writes the summary. A malformed case counts as a
// failure, and the summary says how many there were. A case with Skip set
// is not run and counts as neither a pass nor a failure. A %%FAIL case
// passes only when the program fails to compile, as it does in jq, so a
// program that compiles and then raises an error does not satisfy it.
func Run(cases []Case, d runtime.Dialect, w io.Writer) (pass, fail int, results []Result) {
	malformed, skipped := 0, 0
	for i, c := range cases {
		if c.Skip != "" {
			skipped++
			_, _ = fmt.Fprintf(w, "Test #%d: '%s' at line number %d skipped: %s\n", i+1, c.Program, c.Line, c.Skip)
			results = append(results, Result{Case: c, Skipped: true})
			continue
		}
		_, _ = fmt.Fprintf(w, "Test #%d: '%s' at line number %d\n", i+1, c.Program, c.Line)
		res := runCase(c, d)
		if res.Pass {
			pass++
		} else {
			fail++
			_, _ = fmt.Fprintln(w, res.Err)
		}
		if res.Malformed {
			malformed++
		}
		results = append(results, res)
	}
	_, _ = fmt.Fprintf(w, "%d of %d tests passed (%d malformed, %d skipped)\n", pass, pass+fail, malformed, skipped)
	return pass, fail, results
}

func runCase(c Case, d runtime.Dialect) Result {
	res := Result{Case: c}
	prog, err := jq.Compile(c.Program)
	if err != nil {
		if !c.WantFail {
			res.Malformed = true
			res.Err = fmt.Sprintf("*** Test program failed to compile at line %d: %s", c.Line, c.Program)
			return res
		}
		return judgeFailure(res, d, err)
	}
	if c.WantFail {
		res.Malformed = true
		res.Err = fmt.Sprintf("*** Test program compiled successfully, but should fail at line number %d: %s", c.Line, c.Program)
		return res
	}
	input, err := parseJSON(c.Input)
	if c.Input == "" || err != nil {
		res.Malformed = true
		res.Err = fmt.Sprintf("*** Input is invalid at line number %d: %s", c.Line, c.Program)
		return res
	}
	runErr := execute(prog, d, input, func(v *value.Value) { res.Got = append(res.Got, compact(v)) })
	return judgeOutputs(res, d, runErr)
}

func execute(prog *eval.Program, d runtime.Dialect, input *value.Value, got func(*value.Value)) error {
	ctx := runtime.NewCtx(d, runtime.NumbersExact)
	ctx.Stderr = io.Discard
	return prog.Run(ctx, prog.Env, []*value.Value{input}, func(v *value.Value, _ value.Path) error {
		got(v)
		return nil
	})
}

// judgeOutputs compares what the program produced with what the case
// expects, as values, the way jq does.
func judgeOutputs(res Result, d runtime.Dialect, runErr error) Result {
	c := res.Case
	eq := runtime.NewCtx(d, runtime.NumbersExact).Equal
	for i, got := range res.Got {
		if i >= len(c.Want) {
			res.Err = fmt.Sprintf("*** Superfluous result: %s for test at line number %d, %s", got, c.Line, c.Program)
			return res
		}
		want, err := parseJSON(c.Want[i])
		if err != nil {
			res.Malformed = true
			res.Err = fmt.Sprintf("*** Expected output is invalid at line number %d: %s", c.Line, c.Want[i])
			return res
		}
		gotV, err := parseJSON(got)
		if err != nil || !eq(gotV, want) {
			res.Err = fmt.Sprintf("*** Expected %s, but got %s for test at line number %d: %s", c.Want[i], got, c.Line, c.Program)
			return res
		}
	}
	if len(res.Got) < len(c.Want) {
		res.Malformed = true
		res.Err = fmt.Sprintf("*** Insufficient results for test at line number %d: %s", c.Line, c.Program)
		return res
	}
	if runErr != nil {
		res.Err = fmt.Sprintf("*** Test program failed with '%s' at line number %d: %s", message(d, runErr), c.Line, c.Program)
		return res
	}
	res.Pass = true
	return res
}

// judgeFailure settles a %%FAIL case that did fail, by checking the error
// against the message the file expects.
func judgeFailure(res Result, d runtime.Dialect, err error) Result {
	c := res.Case
	got := message(d, err)
	if c.IgnoreMsg || normalize(got) == normalize(c.WantMsg) {
		res.Pass = true
		return res
	}
	res.Err = fmt.Sprintf("*** Erroneous program failed with 'jq: error: %s', but expected '%s' at line number %d: %s", got, c.WantMsg, c.Line, c.Program)
	return res
}

// message is the bare text of an error, without jq's prefix or position.
func message(d runtime.Dialect, err error) string {
	var ce *query.CompileError
	if errors.As(err, &ce) {
		return ce.Msg
	}
	return d.FormatError(err)
}

// normalize reduces the message a test file expects, or one of ours, to its
// bare text. jq's test files spell a message with a leading "jq: error: "
// or "jq: error (at ...): ", and a compile error ends with its position.
func normalize(msg string) string {
	if rest, ok := strings.CutPrefix(msg, "jq: error"); ok {
		if r, ok := strings.CutPrefix(rest, " (at"); ok {
			_, r, _ = strings.Cut(r, "):")
			rest = ":" + r
		}
		msg = strings.TrimPrefix(rest, ":")
	}
	if i := strings.Index(msg, " at <top-level>"); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimSpace(msg)
}

// parseJSON reads the one value on a line.
func parseJSON(s string) (*value.Value, error) {
	r := load.NewJSONReader(bufio.NewReader(strings.NewReader(s)), "test", load.LoadOptions{})
	v, _, err := r.Next()
	return v, err
}

func compact(v *value.Value) string {
	var b bytes.Buffer
	e := jqout.New(&b, jqout.Options{Compact: true})
	_ = e.Write(v)
	_ = e.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}
