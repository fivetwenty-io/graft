package yaml_test

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// prng is a small deterministic generator, so a failing case repeats
// from its seed.
type prng struct{ state uint64 }

func newPRNG(seed uint64) *prng { return &prng{state: seed*0x9E3779B97F4A7C15 + 1} }

func (p *prng) Intn(n int) int {
	p.state ^= p.state << 13
	p.state ^= p.state >> 7
	p.state ^= p.state << 17
	return int(p.state>>1) % n
}

// roundTripPieces are the building blocks of the generated strings.
var roundTripPieces = []string{
	"", " ", "a", "b c", "\n", "\t", "\r", "\r\n", "\u0085", ": ", ":", "#", " #", "- ", "-", "? ", "'", "\"",
	"\\", "[", "]", "{", "}", ",", "&", "*", "!", "|", ">", "%", "@", "`", "<<", "~", "null", "Null", "yes", "No",
	"on", "OFF", "true", "False", "y", "n", "0", "007", "0x1F", "0o17", "1_000", "1.10", "1e3", "-5", "+5", ".5",
	".inf", "-.inf", ".NaN", "2026-01-02", "2026-01-02T03:04:05Z", "2026-01-02 03:04:05.5 -5", "12:30:45", "é", "日本",
	"line1\nline2", "trail\n", "\nlead", "a\n\nb", "  indented", "end ", "x\ty",
}

func genString(r *prng) string {
	var b strings.Builder
	for range 1 + r.Intn(3) {
		b.WriteString(roundTripPieces[r.Intn(len(roundTripPieces))])
	}
	return b.String()
}

func genValue(r *prng, depth int) any {
	n := 9
	if depth <= 0 {
		n = 6
	}
	switch r.Intn(n) {
	case 0:
		return genString(r)
	case 1:
		return genString(r)
	case 2:
		return r.Intn(2000) - 1000
	case 3:
		return r.Intn(2) == 0
	case 4:
		return []float64{0.5, -2.25, 1e-7, 123456.789, 3.0, 1e20, 4e9}[r.Intn(7)]
	case 5:
		return nil
	case 6, 7:
		m := map[string]any{}
		for range r.Intn(4) {
			m[genString(r)] = genValue(r, depth-1)
		}
		return m
	default:
		l := make([]any, r.Intn(4))
		for i := range l {
			l[i] = genValue(r, depth-1)
		}
		return l
	}
}

// want is the value Unmarshal gives for v: a whole float comes back as
// an int, and every other number comes back as it went in. A whole float
// beyond 64 bits has no Go integer, so Unmarshal into any keeps its
// digits as a string, while a float64 target reads it as the float.
func want(v any) any {
	switch x := v.(type) {
	case float64:
		switch {
		case x == math.Trunc(x) && math.Abs(x) < 1<<62:
			return int(x)
		case x == math.Trunc(x):
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return x
	case map[string]any:
		m := map[string]any{}
		for k, e := range x {
			m[k] = want(e)
		}
		return m
	case []any:
		l := make([]any, len(x))
		for i, e := range x {
			l[i] = want(e)
		}
		return l
	default:
		return v
	}
}

func TestMarshalRoundTripsGenericValues(t *testing.T) {
	t.Parallel()
	r := newPRNG(1)
	for i := range 4000 {
		v := genValue(r, 3)
		for _, order := range []yaml.EncodeOptions{{}, {KeyOrder: "lexical"}} {
			out, err := yaml.MarshalWith(v, order)
			if err != nil {
				continue // an error is allowed, corruption is not
			}
			var got any
			if err := yaml.Unmarshal(out, &got); err != nil {
				t.Fatalf("case %d: Unmarshal(%q): %v", i, out, err)
			}
			if !reflect.DeepEqual(got, want(v)) {
				t.Fatalf("case %d: %#v\nwrote %q\nread  %#v", i, v, out, got)
			}
		}
	}
}

type roundTripStruct struct {
	A string            `yaml:"a"`
	B []string          `yaml:"b"`
	C map[string]string `yaml:"c"`
	D []map[string]int  `yaml:"d"`
}

func TestMarshalRoundTripsTypedTargets(t *testing.T) {
	t.Parallel()
	r := newPRNG(2)
	for i := range 4000 {
		in := roundTripStruct{A: genString(r), C: map[string]string{}}
		for range r.Intn(4) {
			in.B = append(in.B, genString(r))
			in.C[genString(r)] = genString(r)
		}
		for range r.Intn(3) {
			in.D = append(in.D, map[string]int{genString(r): r.Intn(100)})
		}
		out, err := yaml.Marshal(in)
		if err != nil {
			continue
		}
		var got roundTripStruct
		if err := yaml.Unmarshal(out, &got); err != nil {
			t.Fatalf("case %d: Unmarshal(%q): %v", i, out, err)
		}
		if len(in.B) == 0 {
			in.B = nil
			got.B = nil
		}
		if len(in.D) == 0 {
			in.D, got.D = nil, nil
		}
		if !reflect.DeepEqual(got, in) {
			t.Fatalf("case %d: %#v\nwrote %q\nread  %#v", i, in, out, got)
		}
	}
}

func TestMarshalRoundTripsTypedMapsAndSlices(t *testing.T) {
	t.Parallel()
	r := newPRNG(3)
	for i := range 4000 {
		m := map[string]string{}
		var l []string
		for range r.Intn(5) {
			m[genString(r)] = genString(r)
			l = append(l, genString(r))
		}
		for _, in := range []any{m, l} {
			out, err := yaml.Marshal(in)
			if err != nil {
				continue
			}
			got := reflect.New(reflect.TypeOf(in))
			if err := yaml.Unmarshal(out, got.Interface()); err != nil {
				t.Fatalf("case %d: Unmarshal(%q): %v", i, out, err)
			}
			if got.Elem().Len() != 0 && !reflect.DeepEqual(got.Elem().Interface(), in) {
				t.Fatalf("case %d: %#v\nwrote %q\nread  %#v", i, in, out, got.Elem().Interface())
			}
		}
	}
}

func TestMarshalFloatsRoundTrip(t *testing.T) {
	t.Parallel()
	for _, f := range []float64{0.1, -0.1, 1e-7, 1.5e-7, 1e-300, 123.456, 2.5e-5, 1e6, 1e20, 4294967296, -3, 0.30000000000000004} {
		out := mustMarshal(t, map[string]any{"v": f})
		var got map[string]any
		if err := yaml.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%v: %v", f, err)
		}
		if !reflect.DeepEqual(got["v"], want(f)) {
			t.Errorf("%v wrote %q read %#v", f, out, got["v"])
		}
		var typed struct {
			V float64 `yaml:"v"`
		}
		if err := yaml.Unmarshal([]byte(out), &typed); err != nil || typed.V != f {
			t.Errorf("%v wrote %q read %v, %v", f, out, typed.V, err)
		}
	}
}
