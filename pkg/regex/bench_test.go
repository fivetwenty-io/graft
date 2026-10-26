package regex

import (
	"context"
	"strings"
	"testing"
)

// The benchmarks search with the shortcuts and without them under the
// default budget, and report the steps each search took. A plain search
// that runs out of budget reports the steps and time it took to do so.

var (
	benchAs        = strings.Repeat("a", 2_000_000)
	benchAsThenX   = benchAs + "x"
	benchLines     = strings.Repeat(strings.Repeat("a", 99)+"\n", 2000)
	benchLinesX    = benchLines + "x"
	benchShortAs   = strings.Repeat("a", 100_000)
	benchOniguruma = WithFlavor(Oniguruma)
)

func BenchmarkDotStarNoMatch(b *testing.B)    { benchSearch(b, `.*x`, benchAs) }
func BenchmarkDotStarMatch(b *testing.B)      { benchSearch(b, `.*x`, benchAsThenX) }
func BenchmarkDotStarLines(b *testing.B)      { benchSearch(b, `.*x`, benchLines) }
func BenchmarkDotStarLinesMatch(b *testing.B) { benchSearch(b, `.*x`, benchLinesX) }
func BenchmarkDotStarClass(b *testing.B)      { benchSearch(b, `.*[xy]`, benchAs) }
func BenchmarkAlternationLoop(b *testing.B)   { benchSearch(b, `^(a|b)*$`, benchShortAs) }

// benchSearch times one search of subject for pattern under the Oniguruma
// flavor, as jq's test runs it, with the shortcuts and without them.
func benchSearch(b *testing.B, pattern, subject string) {
	for _, mode := range []struct {
		name string
		opts []Option
	}{
		{"shortcuts", []Option{benchOniguruma}},
		{"plain", []Option{benchOniguruma, withoutShortcuts()}},
	} {
		b.Run(mode.name, func(b *testing.B) {
			p := MustCompile(pattern, mode.opts...)
			steps := 0
			for b.Loop() {
				m := newMachine(context.Background(), p, subject, false)
				_, _ = m.search(0)
				steps = m.steps
			}
			b.ReportMetric(float64(steps), "steps/op")
		})
	}
}
