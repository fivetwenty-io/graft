package query_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/jq"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// benchBytes is the size of the generated input, which GRAFT_BENCH_BYTES
// overrides for a quick run.
const benchBytes = 50 << 20

var (
	inputOnce sync.Once
	inputText string
)

// largeInput returns a JSON array of small objects about benchBytes long.
// tests/query-bench/gen-input.sh writes the same document for jq.
func largeInput(b *testing.B) string {
	b.Helper()
	inputOnce.Do(func() {
		size := benchBytes
		if v := os.Getenv("GRAFT_BENCH_BYTES"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				b.Fatalf("GRAFT_BENCH_BYTES must be a positive number, got %q", v)
			}
			size = n
		}
		var buf bytes.Buffer
		buf.Grow(size + 256)
		buf.WriteByte('[')
		for i := 0; buf.Len() < size; i++ {
			if i > 0 {
				buf.WriteByte(',')
			}
			fmt.Fprintf(&buf, `{"n":%d,"k":"k%d","s":"item number %d","f":%d.5}`, i, i%100, i, i%1000)
		}
		buf.WriteByte(']')
		inputText = buf.String()
	})
	return inputText
}

// benchProgram measures a whole run, which decodes the input, runs the
// program, and encodes every output, as one graft jq invocation does.
func benchProgram(b *testing.B, prog, input string) { benchRun(b, prog, input, false) }

// benchProgramFloat is benchProgram with doubles, the in-repo baseline for
// the exact-decimal benchmarks.
func benchProgramFloat(b *testing.B, prog, input string) { benchRun(b, prog, input, true) }

func benchRun(b *testing.B, prog, input string, float bool) {
	b.Helper()
	var opts []jq.Option
	if float {
		opts = append(opts, jq.WithFloatNumbers())
	}
	q, err := jq.Compile(prog, opts...)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(input)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		v, err := jq.NewDecoder(bytes.NewReader([]byte(input)), jq.DecodeOptions{Format: jq.FormatJSON}).Next()
		if err != nil {
			b.Fatal(err)
		}
		enc := jq.NewEncoder(io.Discard, jq.EncodeOptions{Compact: true, FloatNumbers: float})
		for out, err := range q.Run(b.Context(), v) {
			if err != nil {
				b.Fatal(err)
			}
			if err := enc.Encode(out); err != nil {
				b.Fatal(err)
			}
		}
		if err := enc.Flush(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIdentity50MB(b *testing.B) { benchProgram(b, ".", largeInput(b)) }

func BenchmarkSelectGroupBy(b *testing.B) {
	benchProgram(b, `[.[] | select(.n % 2 == 0)] | group_by(.k) | length`, largeInput(b))
}

func BenchmarkReduceSum(b *testing.B) {
	benchProgram(b, `reduce .[] as $x (0; . + $x.n)`, largeInput(b))
}

func BenchmarkToStream(b *testing.B) { benchProgram(b, `[tostream] | length`, largeInput(b)) }

// BenchmarkUpdateEveryElement keeps the builders honest, because a copy per
// element instead of per container shows up here as quadratic time.
func BenchmarkUpdateEveryElement(b *testing.B) {
	benchProgram(b, `.[].n |= . + 1 | length`, largeInput(b))
}

func BenchmarkSetpathChain(b *testing.B) {
	benchProgram(b, `reduce range(10000) as $i ({}; setpath(["k\($i)"]; $i)) | length`, `null`)
}

// BenchmarkExactArithmetic runs exact decimals on the program that
// BenchmarkFloatArithmetic runs on doubles, as jq does.
func BenchmarkExactArithmetic(b *testing.B) {
	benchProgram(b, `reduce range(1000000) as $i (0; . + $i / 7)`, `null`)
}

func BenchmarkFloatArithmetic(b *testing.B) {
	benchProgramFloat(b, `reduce range(1000000) as $i (0; . + $i / 7)`, `null`)
}

func BenchmarkExactCompare(b *testing.B) {
	benchProgram(b, `[range(200000) | . * 1.5] | sort | length`, `null`)
}

// BenchmarkRegexLeadingGreedyLoop is the baseline for regexes that open
// with a greedy loop, which try every start position and give back one
// character at a time when no x follows. A run that exhausts the regex
// step budget ends early and counts as budget-exhausted/op, so the numbers
// stay comparable once the budget stops tripping.
func BenchmarkRegexLeadingGreedyLoop(b *testing.B) {
	for _, n := range []int{1000, 5000, 20000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			q, err := jq.Compile(`test(".*x")`)
			if err != nil {
				b.Fatal(err)
			}
			in := value.Str(string(bytes.Repeat([]byte("a"), n)))
			exhausted := 0
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_, err := q.RunAll(b.Context(), in)
				if err != nil && !strings.Contains(err.Error(), "budget") {
					b.Fatal(err)
				}
				if err != nil {
					exhausted++
				}
			}
			b.ReportMetric(float64(exhausted)/float64(b.N), "budget-exhausted/op")
		})
	}
}
