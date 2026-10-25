package yamlout

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func loadYAML(t *testing.T, text string) []*value.Value {
	t.Helper()
	r := load.Open(load.Source{Name: "roundtrip", Reader: strings.NewReader(text)}, 0, load.LoadOptions{Format: load.FormatYAML})
	var out []*value.Value
	for {
		d, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("loading %q: %v", text, err)
		}
		out = append(out, d.Value)
	}
}

func TestYAMLSpecialNumbersRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		v    *value.Value
		text string
		ok   func(decimal.Decimal) bool
	}{
		{"nan", value.NumDec(decimal.NaN()), ".nan\n", func(d decimal.Decimal) bool { return d.IsNaN() }},
		{"+inf", value.NumDec(decimal.Inf(1)), ".inf\n", func(d decimal.Decimal) bool { return d.Equal(decimal.Inf(1)) }},
		{"-inf", value.NumDec(decimal.Inf(-1)), "-.inf\n", func(d decimal.Decimal) bool { return d.Equal(decimal.Inf(-1)) }},
		{"-0", dec("-0"), "-0\n", func(d decimal.Decimal) bool { return d.IsZero() && d.IsNegative() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := emitOne(t, c.v, Options{})
			wantEqual(t, text, c.text)
			got := loadYAML(t, text)
			if len(got) != 1 {
				t.Fatalf("loaded %d documents from %q, want 1", len(got), text)
			}
			if got[0].Kind() != value.KindNumber || !c.ok(got[0].Dec()) {
				t.Errorf("%q loaded as %v, want the original special value", text, got[0].Dec())
			}
		})
	}
}

func roundTripSample() *value.Value {
	return obj(
		pair("objs", arr(
			obj(pair("a", lit("1")), pair("b", arr(lit("2"), obj(pair("c", value.Null()))))),
			arr(obj(pair("d", value.Str("x"))), arr(lit("3"), lit("4"))),
			obj(),
			arr(),
		)),
		pair("nest", obj(
			pair("list", arr(value.Str("p"), value.Bool(true), value.Bool(false))),
			pair("deeper", obj(pair("k", arr(obj(pair("z", lit("1.5"))))))),
		)),
		pair("literals", arr(
			value.Str("a\nb"),
			value.Str("a\nb\n"),
			value.Str("a\nb\n\n"),
			value.Str("  x\n"),
			value.Str("top\n  indented\nback"),
		)),
		pair("quoted", arr(value.Str("yes"), value.Str("1"), value.Str("null"), value.Str("<<"), value.Str(""), value.Str("a: b"), value.Str("true"))),
		pair("<<", value.Str("merge key")),
		pair("emptyArray", arr()),
		pair("emptyObject", obj()),
	)
}

func TestYAMLParseBackRoundTripIndents(t *testing.T) {
	v := roundTripSample()
	for indent := 2; indent <= 7; indent++ {
		t.Run(fmt.Sprintf("indent%d", indent), func(t *testing.T) {
			text := emitOne(t, v, Options{Indent: indent})
			got := loadYAML(t, text)
			if len(got) != 1 {
				t.Fatalf("loaded %d documents from:\n%s", len(got), text)
			}
			if !value.Equal(got[0], v) {
				t.Errorf("round trip changed the value\nemitted:\n%s", text)
			}
		})
	}
}

func TestYAMLMultiDocumentRoundTrip(t *testing.T) {
	vs := []*value.Value{roundTripSample(), arr(value.Str("a\nb"), lit("2")), value.Str("yes")}
	text := emitAll(t, vs, Options{Indent: 2})
	if !strings.Contains(text, "\n---\n") {
		t.Fatalf("no document separator in:\n%s", text)
	}
	got := loadYAML(t, text)
	if len(got) != len(vs) {
		t.Fatalf("loaded %d documents, want %d, from:\n%s", len(got), len(vs), text)
	}
	for i := range vs {
		if !value.Equal(got[i], vs[i]) {
			t.Errorf("document %d changed\nemitted:\n%s", i, text)
		}
	}
}

// With NoDoc the separators are gone, so two mappings with distinct keys
// read back as one merged-looking mapping; the pair is chosen to show that
// the text is still one valid document holding both values' keys.
func TestYAMLNoDocRoundTrip(t *testing.T) {
	vs := []*value.Value{
		obj(pair("a", lit("1")), pair("l", arr(value.Str("x\ny")))),
		obj(pair("b", value.Str("null")), pair("e", obj())),
	}
	text := emitAll(t, vs, Options{Indent: 3, NoDoc: true})
	if strings.Contains(text, "---") {
		t.Fatalf("NoDoc output has a separator:\n%s", text)
	}
	got := loadYAML(t, text)
	want := obj(append(append([]value.Pair{}, pairsOf(vs[0])...), pairsOf(vs[1])...)...)
	if len(got) != 1 || !value.Equal(got[0], want) {
		t.Errorf("NoDoc text did not load as the joined mapping\nemitted:\n%s", text)
	}
	single := emitOne(t, vs[0], Options{Indent: 3, NoDoc: true})
	if g := loadYAML(t, single); len(g) != 1 || !value.Equal(g[0], vs[0]) {
		t.Errorf("single NoDoc value changed\nemitted:\n%s", single)
	}
}

func pairsOf(v *value.Value) []value.Pair {
	var ps []value.Pair
	for _, k := range v.Keys() {
		x, _ := v.Get(k)
		ps = append(ps, pair(k, x))
	}
	return ps
}
