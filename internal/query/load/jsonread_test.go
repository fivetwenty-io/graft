package load

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

func jsonReader(text string, opts LoadOptions) *JSONReader {
	return NewJSONReader(bufio.NewReader(strings.NewReader(text)), "f", opts)
}

func TestJSONKeepsLiteralAndOrder(t *testing.T) {
	v := mustReadJSON(t, `{"b":100000000000000000000,"a":1.000000000000000000001,"b":2}`)
	wantEqual(t, v.Keys(), []string{"b", "a"})
	wantEqual(t, mustGet(v, "b").Float(), 2.0) // last value wins, first position kept
	v = mustReadJSON(t, `[100000000000000000000, 3.0e2, 1e1000, -0]`)
	var lits []string
	for _, e := range v.Elems() {
		lits = append(lits, e.Literal())
	}
	wantEqual(t, lits, []string{"100000000000000000000", "3.0e2", "1e1000", "-0"})
}

func TestJSONOverBoundLiteralPassesThrough(t *testing.T) {
	v := mustReadJSON(t, "1e9999999999")
	wantEqual(t, v.Literal(), "1e9999999999")
	wantEqual(t, v.Bounded(), false)
	v = mustReadJSON(t, "1"+strings.Repeat("0", 100001))
	wantEqual(t, v.Bounded(), false)
}

func TestJSONSyntaxErrorShape(t *testing.T) {
	_, err := Open(Source{"f", strings.NewReader("{\"a\":}")}, 0, LoadOptions{Format: FormatJSON}).Next()
	wantErrContains(t, err, "line 1, column 6")
}

func TestJSONSyntaxErrors(t *testing.T) {
	for in, want := range map[string]string{
		`[1,]`:           "unexpected character ']'",
		`{"a":1,}`:       "object key must be a string",
		`{"a" 1}`:        "expected ':'",
		`[1 2]`:          "expected ',' or ']'",
		`"abc`:           "unexpected end of input",
		"\"a\nb\"":       "control characters",
		`"\q"`:           "invalid escape",
		`"\u12G4"`:       "invalid \\u escape",
		`tru`:            "invalid literal",
		`1e`:             "invalid literal",
		`0x10`:           "invalid literal",
		`nan1`:           "invalid literal",
		`[`:              "unexpected end of input",
		`{"a":1`:         "unexpected end of input",
		`@`:              "unexpected character '@'",
		"[1,\n  2,\n x]": "line 3, column 2",
	} {
		_, _, err := jsonReader(in, LoadOptions{}).Next()
		if err == nil {
			t.Errorf("%q: no error", in)
			continue
		}
		wantErrContains(t, err, want)
	}
}

func TestJSONStringEscapes(t *testing.T) {
	v := mustReadJSON(t, `"a\n\t\"\\\/é😀\ud800x\udc00"`)
	wantEqual(t, v.Str(), "a\n\t\"\\/é😀�x�")
	v = mustReadJSON(t, "\"a\xffb\xfe\xfdc\"")
	wantEqual(t, v.Str(), "a�b��c")
}

func TestJSONErrorSticks(t *testing.T) {
	r := jsonReader(`1 [ 2`, LoadOptions{})
	if _, _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	_, _, err := r.Next()
	wantErrContains(t, err, "unexpected end of input")
	_, _, err2 := r.Next()
	wantEqual(t, err2, err)
}

func TestJSONSeqSkipsRecordSeparators(t *testing.T) {
	r := jsonReader("\x1e1\n\x1e[2]\n", LoadOptions{Seq: true})
	var n int
	for {
		_, _, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	wantEqual(t, n, 2)
	_, _, err := jsonReader("\x1e1", LoadOptions{}).Next()
	wantErrContains(t, err, "unexpected character")
}

func TestJSONDepthLimitOption(t *testing.T) {
	_, _, err := jsonReader("[[[1]]]", LoadOptions{DepthLimit: 2}).Next()
	wantErrContains(t, err, "exceeds maximum depth of 2")
	_, _, err = jsonReader("[[1]]", LoadOptions{DepthLimit: 2}).Next()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = jsonReader(strings.Repeat("[", 10000)+strings.Repeat("]", 10000), LoadOptions{}).Next()
	if err != nil {
		t.Fatal(err)
	}
}
