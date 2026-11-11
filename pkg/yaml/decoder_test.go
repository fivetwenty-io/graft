package yaml_test

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func decodeStream(t *testing.T, in string) ([]any, error) {
	t.Helper()
	d := yaml.NewDecoder(strings.NewReader(in))
	var out []any
	for range 10 {
		var v any
		err := d.Decode(&v)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
	t.Fatalf("%q: the stream did not end", in)
	return nil, nil
}

func TestDecoderWalksTheStream(t *testing.T) {
	t.Parallel()
	one := []any{map[string]any{"a": "x"}}
	two := []any{map[string]any{"a": "x"}, map[string]any{"b": "y"}}
	cases := []struct {
		in   string
		want []any
	}{
		{"a: x\n---\nb: y\n", two},
		{"---\na: x\n", one},
		{"a: x\n...\n---\nb: y\n", two},
		{"a: x\n...\n", one},
		{"a: x\n---\n", one},
		{"a: x\n---\n\n", one},
		{"a: x\n---\n...\n---\nb: y\n", two},
		{"\uFEFFa: x\n---\nb: y\n", two},
		{"---\nnull\n---\nb: y\n", []any{nil, map[string]any{"b": "y"}}},
		{"", nil},
		{"\n\n  \n", nil},
		{"---\n", nil},
		{"--- \n---\n", nil},
		{"# c\n", nil},
	}
	for _, c := range cases {
		got, err := decodeStream(t, c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %#v, %v, want %#v", c.in, got, err, c.want)
		}
	}
}

func TestDecoderRefusesContentAfterDocumentEnd(t *testing.T) {
	t.Parallel()
	got, err := decodeStream(t, "a: x\n...\nb: y\n")
	var de *yaml.DecodeError
	if len(got) != 1 || !errors.As(err, &de) || de.Line != 3 || de.Kind != yaml.ErrorKindSyntax || de.Msg != "did not find expected <document start>" {
		t.Errorf("got %#v, %+v", got, err)
	}
}

func TestDecoderCountsLinesFromTheStreamStart(t *testing.T) {
	t.Parallel()
	type port struct {
		Port int `yaml:"port"`
	}
	d := yaml.NewDecoder(strings.NewReader("port: 1\n---\nport: 2\n---\n\nport: x\n---\nport: 4\n"))
	for _, want := range []int{1, 2} {
		var p port
		if err := d.Decode(&p); err != nil || p.Port != want {
			t.Fatalf("got %d, %v, want %d", p.Port, err, want)
		}
	}
	var p port
	err := d.Decode(&p)
	var got, alone *yaml.DecodeError
	if !errors.As(err, &got) || !errors.As(yaml.Unmarshal([]byte("port: x\n"), &port{}), &alone) {
		t.Fatalf("got %v", err)
	}
	if got.Line != 6 || got.Column != alone.Column || got.Kind != alone.Kind || got.Msg != alone.Msg {
		t.Errorf("got %+v, want %+v at line 6", got, alone)
	}
	// A type error ends only its own document.
	if err := d.Decode(&p); err != nil || p.Port != 4 {
		t.Errorf("after a type error: got %d, %v", p.Port, err)
	}

	h := yaml.NewDecoder(strings.NewReader("host: a\n---\nhost: b\n---\nhost: c\nport: x\n"))
	for _, want := range []int{1, 3} {
		var e endpoint
		if err := h.Decode(&e); err != nil || e.Line != want {
			t.Fatalf("got line %d, %v, want %d", e.Line, err, want)
		}
	}
	var e endpoint
	if err := h.Decode(&e); !errors.As(err, &got) || got.Line != 6 {
		t.Errorf("a hook's error: got %v, want line 6", err)
	}

	var seen []yaml.DuplicateKey
	dk := yaml.NewDecoderWith(strings.NewReader("a: x\n---\nk: 1\nk: 2\n"),
		yaml.DecodeOptions{OnDuplicateKey: func(d yaml.DuplicateKey) { seen = append(seen, d) }})
	for range 2 {
		var v any
		if err := dk.Decode(&v); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 1 || seen[0].Line != 4 || seen[0].FirstLine != 3 {
		t.Errorf("duplicates: got %+v", seen)
	}
}

func TestDecoderStopsAtASyntaxOrReadError(t *testing.T) {
	t.Parallel()
	d := yaml.NewDecoder(strings.NewReader("a: [\n"))
	var v any
	first := d.Decode(&v)
	var de *yaml.DecodeError
	if !errors.As(first, &de) || de.Kind != yaml.ErrorKindSyntax {
		t.Fatalf("got %v", first)
	}
	if again := d.Decode(&v); again != first { //nolint:errorlint // A later Decode returns the very same error.
		t.Errorf("a second Decode: got %v, want the same error", again)
	}
	boom := errors.New("boom")
	r := yaml.NewDecoder(iotest.ErrReader(boom))
	if err := r.Decode(&v); !errors.Is(err, boom) {
		t.Errorf("got %v, want the reader's error", err)
	}
	if err := r.Decode(&v); !errors.Is(err, boom) {
		t.Errorf("a second Decode: got %v", err)
	}
	if err := yaml.NewDecoder(strings.NewReader("a: 1\n")).Decode(nil); err == nil || err.Error() != "yaml: Decode(nil)" {
		t.Errorf("Decode(nil): got %v", err)
	}
}

// thirdDocumentAt is how many lines of the stream thirdDocument builds
// come before doc, whose first line is the stream's line 6.
const thirdDocumentAt = 5

// thirdDocument returns a stream whose third document is doc, after a
// "---" on line 5.
func thirdDocument(doc string) string {
	return "a: x\n---\nb: y\n\n---\n" + doc
}

// decodeThird decodes the first two documents of in and then the third
// into v, returning the third's error.
func decodeThird(t *testing.T, in string, opts yaml.DecodeOptions, v any) error {
	t.Helper()
	d := yaml.NewDecoderWith(strings.NewReader(in), opts)
	for range 2 {
		var skip any
		if err := d.Decode(&skip); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
	}
	return d.Decode(v)
}

type hostedNote struct {
	Primary endpoint `yaml:"primary"`
	Note    string   `yaml:"note"`
}

func TestDecoderThirdDocumentErrorsCountFromTheStreamStart(t *testing.T) {
	t.Parallel()
	text := func() any { return new(any) }
	hook := func() any { return new(hostedNote) }
	cases := []struct {
		route  string
		target func() any
		doc    string
	}{
		{"the text route", text, "note: [\n"},
		{"the text route", text, "note: *nowhere\n"},
		{"the text route", text, "note: !!int x\n"},
		{"the hook route", hook, "note: [\n"},
		{"the hook route", hook, "note: *nowhere\n"},
		{"the hook route", hook, "note: [1]\n"},
		{"the hook route", hook, "primary: {host: a, port: x}\n"},
		{"the hook route", hook, "primary:\n  host: a\n  port: [1]\n"},
	}
	for _, c := range cases {
		var got, alone *yaml.DecodeError
		err := decodeThird(t, thirdDocument(c.doc), yaml.DecodeOptions{}, c.target())
		if !errors.As(yaml.Unmarshal([]byte(c.doc), c.target()), &alone) || alone.Line <= 0 {
			t.Fatalf("%s, %q: Unmarshal gives no error with a line", c.route, c.doc)
		}
		want := *alone
		want.Line += thirdDocumentAt
		if !errors.As(err, &got) || got.Line != want.Line || got.Column != want.Column || got.Kind != want.Kind || got.Msg != want.Msg {
			t.Errorf("%s, %q: got %v, want %+v", c.route, c.doc, err, &want)
		}
	}
}

func TestDecoderThirdDocumentDuplicatesCountFromTheStreamStart(t *testing.T) {
	t.Parallel()
	in := thirdDocument("primary: {host: a}\nprimary: {host: b}\n")
	want := yaml.DuplicateKey{Path: "primary", Line: 7, Column: 1, FirstLine: 6, FirstColumn: 1}
	targets := map[string]any{
		"the text route": new(any),
		"the hook route": new(hostedNote),
		"a Node":         new(yaml.Node),
	}
	for name, v := range targets {
		var seen []yaml.DuplicateKey
		opts := yaml.DecodeOptions{OnDuplicateKey: func(d yaml.DuplicateKey) { seen = append(seen, d) }}
		if err := decodeThird(t, in, opts, v); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(seen) != 1 || seen[0] != want {
			t.Errorf("%s: got %+v, want %+v", name, seen, want)
		}
	}
}

func TestDecoderThirdDocumentNodesCountFromTheStreamStart(t *testing.T) {
	t.Parallel()
	doc := "note: |\n  kept  \nprimary:\n  host: a\n  port: 7\n"
	var got hostedNote
	if err := decodeThird(t, thirdDocument(doc), yaml.DecodeOptions{}, &got); err != nil {
		t.Fatal(err)
	}
	var alone hostedNote
	if err := yaml.Unmarshal([]byte(doc), &alone); err != nil {
		t.Fatal(err)
	}
	alone.Primary.Line += thirdDocumentAt
	if got != alone || got.Primary.Line != 9 {
		t.Errorf("got %+v, want %+v with the hook at line 9", got, alone)
	}

	var n yaml.Node
	if err := decodeThird(t, thirdDocument(doc), yaml.DecodeOptions{}, &n); err != nil {
		t.Fatal(err)
	}
	root := n.Content[0]
	if n.Line != 5 || root.Line != 6 || root.Content[2].Line != 8 || root.Content[3].Content[1].Line != 9 {
		t.Errorf("node lines: document %d, root %d, primary %d, host %d", n.Line, root.Line, root.Content[2].Line, root.Content[3].Content[1].Line)
	}
	var fromNode, fromText any
	if err := n.Decode(&fromNode); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(doc), &fromText); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromNode, fromText) {
		t.Errorf("the node decodes to %#v, want %#v", fromNode, fromText)
	}
	var bad struct {
		Primary struct {
			Port bool `yaml:"port"`
		} `yaml:"primary"`
	}
	var de *yaml.DecodeError
	if err := n.Decode(&bad); !errors.As(err, &de) || de.Line != 10 {
		t.Errorf("the node's error: got %v, want line 10", err)
	}

	// The tree reads two explicit block keys that are both "" as one key
	// twice, and Decode asks how Unmarshal read them, by their lines.
	keys := "? |\n? |1\n"
	var fromKeys yaml.Node
	if err := decodeThird(t, thirdDocument(keys), yaml.DecodeOptions{}, &fromKeys); err != nil {
		t.Fatal(err)
	}
	var keysFromNode, keysFromText map[string]string
	nodeErr, textErr := fromKeys.Decode(&keysFromNode), yaml.Unmarshal([]byte(keys), &keysFromText)
	if nodeErr != nil || textErr != nil || !reflect.DeepEqual(keysFromNode, keysFromText) {
		t.Errorf("explicit keys: the node gives %#v, %v, want %#v, %v", keysFromNode, nodeErr, keysFromText, textErr)
	}
}

func TestDecoderThirdDocumentDuplicateErrorsCountFromTheStreamStart(t *testing.T) {
	t.Parallel()
	doc := "primary: {host: a}\nprimary: {host: b}\n"
	want := "mapping key already defined at [6:1]"
	targets := map[string]any{
		"the text route": new(any),
		"the hook route": new(hostedNote),
	}
	for name, v := range targets {
		var de *yaml.DecodeError
		err := decodeThird(t, thirdDocument(doc), yaml.DecodeOptions{}, v)
		if !errors.As(err, &de) || de.Line != 7 || de.Msg != want {
			t.Errorf("%s: got %v, want line 7: %s", name, err, want)
		}
	}
}
