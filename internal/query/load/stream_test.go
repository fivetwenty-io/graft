package load

import (
	"bufio"
	"errors"
	"io"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// events reads every document of text under opts and prints each as
// compact JSON.
func events(t *testing.T, text string, opts LoadOptions) []string {
	t.Helper()
	r := Open(Source{"f", strings.NewReader(text)}, 0, opts)
	var out []string
	for {
		d, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			out = append(out, "error: "+err.Error())
			return out
		}
		out = append(out, runtime.JSON(d.Value))
	}
}

func TestStreamEvents(t *testing.T) {
	opts := LoadOptions{Format: FormatJSON, Stream: true}
	for in, want := range map[string][]string{
		`{"a":[1,2]}`:   {`[["a",0],1]`, `[["a",1],2]`, `[["a",1]]`, `[["a"]]`},
		`[[],{},[1]]`:   {`[[0],[]]`, `[[1],{}]`, `[[2,0],1]`, `[[2,0]]`, `[[2]]`},
		`3 "s" null`:    {`[[],3]`, `[[],"s"]`, `[[],null]`},
		`[] {}`:         {`[[],[]]`, `[[],{}]`},
		`{"a":{"b":1}}`: {`[["a","b"],1]`, `[["a","b"]]`, `[["a"]]`},
	} {
		wantEqual(t, events(t, in, opts), want)
	}
}

func TestStreamHoldsBackAnUnconfirmedLeaf(t *testing.T) {
	opts := LoadOptions{Format: FormatJSON, Stream: true}
	got := events(t, `{"a":[1,2`, opts)
	wantEqual(t, got[0], `[["a",0],1]`)
	wantEqual(t, len(got), 2)
	if !strings.Contains(got[1], "Unfinished JSON term at EOF") {
		t.Errorf("got %q", got[1])
	}
}

func TestStreamErrorsBecomeEventsAndTheParserReadsOnNextLine(t *testing.T) {
	opts := LoadOptions{Format: FormatJSON, Stream: true, StreamErrors: true}
	wantEqual(t, events(t, "[1,]\n[2]\n", opts), []string{
		`[[0],1]`,
		`["Expected another array element at line 1, column 4",[1]]`,
		`[[0],2]`, `[[0]]`,
	})
	wantEqual(t, events(t, "x\n3", opts), []string{
		`["Invalid numeric literal at line 2, column 0",[]]`,
		`[[],3]`,
	})
}

func TestStreamEventsOfALoadedYAMLDocument(t *testing.T) {
	got := events(t, "a: [1, 2]\n---\nb: {}\n", LoadOptions{Stream: true})
	wantEqual(t, got, []string{`[["a",0],1]`, `[["a",1],2]`, `[["a",1]]`, `[["a"]]`, `[["b"],{}]`, `[["b"]]`})
}

// holdingReader gives its first piece and then fails the test if the
// reader asks for more before the caller releases it.
type holdingReader struct {
	t     *testing.T
	first string
	done  bool
}

func (h *holdingReader) Read(p []byte) (int, error) {
	if h.done {
		h.t.Error("the reader asked for more input before it had to")
		return 0, io.EOF
	}
	h.done = true
	return copy(p, h.first), nil
}

func TestStreamDoesNotReadAheadOfWhatItReturns(t *testing.T) {
	// A large document would stall here if the loader read it whole, so
	// the first events must come from the first piece alone.
	h := &holdingReader{t: t, first: `{"a":[1,2,3],"b":`}
	r := Open(Source{"f", h}, 0, LoadOptions{Format: FormatJSON, Stream: true})
	var got []string
	for range 3 {
		d, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, runtime.JSON(d.Value))
	}
	wantEqual(t, got, []string{`[["a",0],1]`, `[["a",1],2]`, `[["a",2],3]`})
}

func TestJSONLineIsTheLinesTakenSoFar(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []int
	}{
		{"1\n2\n", []int{1, 2}},
		{"1 2\n3\n", []int{1, 1, 2}},
		{"1\n\n\n2", []int{1, 3}},
		{"{\n\"a\":\n1}\n", []int{3}},
		{"{\n\"a\":\n1}", []int{2}},
	} {
		r := Open(Source{"f", strings.NewReader(c.in)}, 0, LoadOptions{Format: FormatJSON})
		var got []int
		for {
			if _, err := r.Next(); err != nil {
				break
			}
			got = append(got, r.Line())
		}
		wantEqual(t, got, c.want)
	}
}

func TestSeqSkipsADamagedTextAndWarns(t *testing.T) {
	var warned []string
	opts := LoadOptions{Format: FormatJSON, Seq: true, Warn: func(m string) { warned = append(warned, m) }}
	got := events(t, "\x1e{\"a\":1\x1e{\"b\":2}\n\x1e[3,\x1e4\n", opts)
	wantEqual(t, got, []string{`{"b":2}`, `4`})
	wantEqual(t, warned, []string{
		"Truncated value at line 1, column 8",
		"Truncated value at line 2, column 5",
	})
}

func TestSeqAssemblesRepeatedKeysAndNesting(t *testing.T) {
	opts := LoadOptions{Format: FormatJSON, Seq: true}
	got := events(t, "\x1e{\"a\":{\"x\":1},\"a\":{\"y\":2},\"b\":[[1],[2,3]],\"c\":[]}\n", opts)
	wantEqual(t, got, []string{`{"a":{"y":2},"b":[[1],[2,3]],"c":[]}`})
}

func TestSeqReadsEventsAndRecoversAtTheNextByte(t *testing.T) {
	opts := LoadOptions{Format: FormatJSON, Seq: true, Stream: true, StreamErrors: true}
	wantEqual(t, events(t, "\x1e[1,]\n[2]\n", opts), []string{
		`[[0],1]`, `["Expected another array element at line 1, column 5 (need RS to resync)",[1]]`,
		`[[0],2]`, `[[0]]`,
	})
}

// allocatedBy returns the bytes allocated while f runs.
func allocatedBy(f func()) uint64 {
	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	f()
	goruntime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// countDocs reads every document of text under opts and counts them.
func countDocs(t *testing.T, text string, opts LoadOptions) int {
	t.Helper()
	r := Open(Source{"f", strings.NewReader(text)}, 0, opts)
	n := 0
	for {
		_, err := r.Next()
		if errors.Is(err, io.EOF) {
			return n
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
}

// A path that every event copies in full costs the square of the depth, so
// the bytes allocated for deeply nested input tell the two apart without
// a clock.
func TestStreamWorkIsLinearInDepth(t *testing.T) {
	const depth = 20000
	text := strings.Repeat("[", depth) + strings.Repeat("]", depth)
	var n int
	got := allocatedBy(func() { n = countDocs(t, text, LoadOptions{Format: FormatJSON, Stream: true}) })
	wantEqual(t, n, depth)
	if limit := uint64(depth) * 4096; got > limit {
		t.Errorf("allocated %d bytes for %d levels, want at most %d", got, depth, limit)
	}
}

func TestSeqWorkIsLinearInDepth(t *testing.T) {
	const depth = 9990
	text := "\x1e" + strings.Repeat("[", depth) + strings.Repeat("]", depth)
	var n int
	got := allocatedBy(func() { n = countDocs(t, text, LoadOptions{Format: FormatJSON, Seq: true}) })
	wantEqual(t, n, 1)
	if limit := uint64(depth) * 4096; got > limit {
		t.Errorf("allocated %d bytes for %d levels, want at most %d", got, depth, limit)
	}
}

func TestStreamSharedPathsKeepTheirOwnValues(t *testing.T) {
	opts := LoadOptions{Format: FormatJSON, Stream: true}
	wantEqual(t, events(t, `[[1,[2]],[3]]`, opts), []string{
		`[[0,0],1]`, `[[0,1,0],2]`, `[[0,1,0]]`, `[[0,1]]`,
		`[[1,0],3]`, `[[1,0]]`, `[[1]]`,
	})
	wantEqual(t, events(t, `{"a":{"b":[]},"c":{"d":1}}`, opts), []string{
		`[["a","b"],[]]`, `[["a","b"]]`, `[["c","d"],1]`, `[["c","d"]]`, `[["c"]]`,
	})
}

// The assembler is given the events of a deeply nested value and must find
// the container of each in a few steps, not by walking from the root.
func TestAssemblerFindsContainersWithoutWalkingFromTheRoot(t *testing.T) {
	const depth = 9990
	text := strings.Repeat("[", depth) + "1" + strings.Repeat("]", depth) + "\n"
	r := NewJSONReader(bufio.NewReader(strings.NewReader(text)), "f", LoadOptions{Format: FormatJSON, Stream: true, DepthLimit: depth + 1})
	var a assembler
	var done *value.Value
	for {
		ev, err := r.NextEvent()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if v := a.add(ev); v != nil {
			done = v
		}
	}
	if done == nil {
		t.Fatal("the assembler completed no value")
	}
	if limit := 4 * depth; a.walked > limit {
		t.Errorf("walked %d steps for %d levels, want at most %d", a.walked, depth, limit)
	}
}

// jq refuses a high surrogate that no low surrogate escape follows, and
// reads a lone low surrogate as U+FFFD.
func TestStringEscapeFailuresAreJQs(t *testing.T) {
	for in, want := range map[string]string{
		`"\ud800"`:       "Invalid \\uXXXX\\uXXXX surrogate pair escape",
		`"\ud800x"`:      "Invalid \\uXXXX\\uXXXX surrogate pair escape",
		`"\ud800A"`:      "Invalid \\uXXXX\\uXXXX surrogate pair escape",
		`"\ud800\ud800"`: "Invalid \\uXXXX\\uXXXX surrogate pair escape",
		`"\udc00\ud800"`: "Invalid \\uXXXX\\uXXXX surrogate pair escape",
		`"\ud800\uzzzz"`: "Invalid \\uXXXX\\uXXXX surrogate pair escape",
		`"\uzzzz"`:       "Invalid characters in \\uXXXX escape",
		`"\u12zz"`:       "Invalid characters in \\uXXXX escape",
		`"\u12"`:         "Invalid \\uXXXX escape",
		`"\q"`:           "Invalid escape",
	} {
		stream := events(t, in, LoadOptions{Format: FormatJSON, Stream: true})
		plain := events(t, in, LoadOptions{Format: FormatJSON})
		for name, got := range map[string][]string{"stream": stream, "plain": plain} {
			if len(got) != 1 || !strings.Contains(got[0], want) {
				t.Errorf("%s %s: got %q, want an error containing %q", name, in, got, want)
			}
		}
	}
}

func TestStringEscapesThatDecode(t *testing.T) {
	for in, want := range map[string]string{
		`"😀"`:      `"😀"`,
		`"𐀀􏿿"`:     `"𐀀􏿿"`,
		`"\udc00"`: "\"�\"",
		`"aA\n\/"`: `"aA\n/"`,
	} {
		wantEqual(t, events(t, in, LoadOptions{Format: FormatJSON}), []string{want})
		wantEqual(t, events(t, in, LoadOptions{Format: FormatJSON, Stream: true}), []string{"[[]," + want + "]"})
	}
}

func TestStreamErrorsReportABadEscapeAtTheClosingQuote(t *testing.T) {
	opts := LoadOptions{Format: FormatJSON, Stream: true, StreamErrors: true}
	wantEqual(t, events(t, `"\ud800"`, opts), []string{`["Invalid \\uXXXX\\uXXXX surrogate pair escape at line 1, column 8",[]]`})
	wantEqual(t, events(t, `"\uzzzz"`, opts), []string{`["Invalid characters in \\uXXXX escape at line 1, column 8",[]]`})
}
