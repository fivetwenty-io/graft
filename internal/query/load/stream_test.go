package load

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
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
