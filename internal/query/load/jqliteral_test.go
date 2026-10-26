package load

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
)

func TestJQNumberSpellings(t *testing.T) {
	for in, want := range map[string]string{
		"01":     "1",
		"-01":    "-1",
		"00":     "0",
		"-00":    "-0",
		"+1":     "1",
		".5":     "0.5",
		"-.5":    "-0.5",
		"1.":     "1",
		"1.e2":   "1e2",
		"+1.5E3": "1.5E3",
		"1.0":    "1.0",
		"1e5":    "1e5",
	} {
		v, ok := jqNumber(in)
		if !ok {
			t.Errorf("%q: refused", in)
			continue
		}
		w, _ := jqNumber(want)
		wantEqual(t, runtime.JSON(v), runtime.JSON(w))
	}
	for _, in := range []string{"1e", "1e+", "0x10", "1_0", "0.5.5", "1e5e5", "-", "--1", "++1", "1-1",
		".", "+", "nan1", "nanx", "na", "infi", "infinityx", "I", "x", "1x"} {
		if _, ok := jqNumber(in); ok {
			t.Errorf("%q: accepted", in)
		}
	}
}

func TestJQNonFinite(t *testing.T) {
	for _, in := range []string{"nan", "NaN", "NAN", "-nan", "+nan"} {
		v := mustReadJSON(t, in)
		wantEqual(t, runtime.JSON(v), "null")
	}
	for in, want := range map[string]string{
		"infinity": "1.7976931348623157e+308", "-Infinity": "-1.7976931348623157e+308",
		"inf": "1.7976931348623157e+308", "iNf": "1.7976931348623157e+308",
	} {
		wantEqual(t, runtime.JSON(mustReadJSON(t, in)), want)
	}
}

func TestStreamAcceptsTheSameSpellings(t *testing.T) {
	opts := LoadOptions{Format: FormatJSON, Stream: true}
	wantEqual(t, events(t, "[01, nan, +1, .5, 1.]", opts),
		[]string{`[[0],1]`, `[[1],null]`, `[[2],1]`, `[[3],0.5]`, `[[4],1]`, `[[4]]`})
	got := events(t, "nu", opts)
	if !strings.Contains(got[0], "Invalid literal at") {
		t.Errorf("got %v", got)
	}
	got = events(t, "na", opts)
	if !strings.Contains(got[0], "Invalid numeric literal at") {
		t.Errorf("got %v", got)
	}
}
