package yq

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func encodeString(t *testing.T, opts EncodeOptions, vs ...*value.Value) string {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf, opts)
	for _, v := range vs {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestEncoderJSONMatchesCLI(t *testing.T) {
	got := encodeString(t, EncodeOptions{Format: FormatJSON, Indent: 2}, decodeOne(t, "a: {b: 1}\n"))
	wantEqual(t, got, "{\n  \"a\": {\n    \"b\": 1\n  }\n}\n")
}

func TestEncoderJSONCompactAndIndent(t *testing.T) {
	v := decodeOne(t, "a: {b: 1}\n")
	wantEqual(t, encodeString(t, EncodeOptions{Format: FormatJSON, Indent: -1}, v), "{\"a\":{\"b\":1}}\n")
	wantEqual(t, encodeString(t, EncodeOptions{Format: FormatJSON, Indent: 4}, v), "{\n    \"a\": {\n        \"b\": 1\n    }\n}\n")
}

func TestEncoderJSONUnwrapScalar(t *testing.T) {
	wantEqual(t, encodeString(t, EncodeOptions{Format: FormatJSON, UnwrapScalar: true}, value.Str("x")), "x\n")
	wantEqual(t, encodeString(t, EncodeOptions{Format: FormatJSON}, value.Str("x")), "\"x\"\n")
}

func TestEncoderJSONColors(t *testing.T) {
	got := encodeString(t, EncodeOptions{Format: FormatJSON, Colors: true}, value.Null())
	wantEqual(t, strings.Contains(got, "\x1b["), true)
}

func TestEncoderYAMLIndentAndNoDocSeparators(t *testing.T) {
	docs := decodeAll(t, "a:\n  b: 1\n---\nc: 2\n")
	wantEqual(t, encodeString(t, EncodeOptions{Indent: 4}, docs...), "a:\n    b: 1\n---\nc: 2\n")
	wantEqual(t, encodeString(t, EncodeOptions{NoDocSeparators: true}, docs...), "a:\n  b: 1\nc: 2\n")
}

func TestEncoderYAMLCompactSeqIndent(t *testing.T) {
	v := decodeOne(t, "a:\n  - 1\n  - b:\n      - c\n")
	wantEqual(t, encodeString(t, EncodeOptions{}, v), "a:\n  - 1\n  - b:\n      - c\n")
	wantEqual(t, encodeString(t, EncodeOptions{CompactSeqIndent: true}, v), "a:\n- 1\n- b:\n  - c\n")
}

func TestEncoderYAMLUnwrapScalar(t *testing.T) {
	wantEqual(t, encodeString(t, EncodeOptions{UnwrapScalar: true}, decodeOne(t, "\"hi\"\n")), "hi\n")
	wantEqual(t, encodeString(t, EncodeOptions{}, decodeOne(t, "\"hi\"\n")), "\"hi\"\n")
}

func TestEncoderPrettyPrintDropsStyles(t *testing.T) {
	in := "a: !foo b\nc: \"1\"\nd: {x: 1, y: [1, 2]}\n"
	want := "a: !foo b\nc: \"1\"\nd:\n  x: 1\n  y:\n    - 1\n    - 2\n"
	wantEqual(t, encodeString(t, EncodeOptions{PrettyPrint: true}, decodeOne(t, in)), want)
	wantEqual(t, encodeString(t, EncodeOptions{}, decodeOne(t, in)), in)
}

func TestEncoderPrettyPrintKeepsBoolWordStyles(t *testing.T) {
	in := "'yes': 'On'\nb: \"NO\"\nc: 'y'\nd: 'yesno'\n"
	want := "'yes': 'On'\nb: \"NO\"\nc: 'y'\nd: yesno\n"
	wantEqual(t, encodeString(t, EncodeOptions{PrettyPrint: true}, decodeOne(t, in)), want)
}

func TestDecoderValuesCarryMeta(t *testing.T) {
	docs := decodeAll(t, "a: &x 1 # line\nb: *x\n---\nc: 2\n")
	wantEqual(t, len(docs), 2)
	a, _ := docs[0].Get("a")
	m := a.Meta()
	wantEqual(t, m.Anchor, "x")
	wantEqual(t, m.LineComment, "# line")
	wantEqual(t, m.Line, 1)
	wantEqual(t, docs[1].Meta().DocIndex, 1)
	wantEqual(t, docs[1].Meta().Filename, "in.yaml")
}

func TestDecoderFileIndex(t *testing.T) {
	v, err := NewDecoder(strings.NewReader("a: 1\n"), DecodeOptions{FileIndex: 3}).Next()
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, v.Meta().FileIndex, 3)
}

func TestDecoderJSON(t *testing.T) {
	dec := NewDecoder(strings.NewReader(`{"a": [1, 2]} {"b": 3}`), DecodeOptions{Format: FormatJSON})
	v, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, v.Len(), 1)
	if _, err = dec.Next(); err != nil {
		t.Fatal(err)
	}
	_, err = dec.Next()
	wantEqual(t, errors.Is(err, io.EOF), true)
	_, err = NewDecoder(strings.NewReader("a: 1"), DecodeOptions{Format: FormatJSON}).Next()
	if err == nil {
		t.Fatal("YAML read as JSON did not fail")
	}
}

func TestDecoderErrorSticksAndEOF(t *testing.T) {
	dec := NewDecoder(strings.NewReader("a: [1\n"), DecodeOptions{})
	_, err := dec.Next()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want a parse error", err)
	}
	_, err = NewDecoder(strings.NewReader(""), DecodeOptions{}).Next()
	wantEqual(t, errors.Is(err, io.EOF), true)
}

func TestDecoderFrontMatterIsNotYetSupported(t *testing.T) {
	_, err := NewDecoder(strings.NewReader("a: 1\n"), DecodeOptions{FrontMatter: "extract"}).Next()
	wantContains(t, err.Error(), "front matter")
}

func TestExitCodeTable(t *testing.T) {
	wantEqual(t, ExitCode(true, false, nil, true), 1)  // produced only null or false
	wantEqual(t, ExitCode(false, false, nil, true), 1) // produced nothing
	wantEqual(t, ExitCode(false, false, &query.CompileError{}, false), 1)
	wantEqual(t, ExitCode(true, true, nil, true), 0)   // one truthy output is enough
	wantEqual(t, ExitCode(true, false, nil, false), 0) // without -e, null is fine
	wantEqual(t, ExitCode(false, false, nil, false), 0)
	wantEqual(t, ExitCode(true, true, &query.RuntimeError{}, true), 1)
	wantEqual(t, ExitCode(false, false, &fs.PathError{}, false), 1)
}
