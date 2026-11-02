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

// bigFloats mixes fractions with whole floats on both sides of the int64
// range. float64(math.MaxInt64) is 2^63, which does not fit.
var bigFloats = []float64{
	0.5, -2.25, 1e-7, 123456.789, 3.0, 4e9, 1e20, -1e20, 1e300,
	9223372036854774784, 9223372036854775808, -9223372036854775808, -9223372036854777856,
	float64(math.MaxInt64), 4611686018427387904, 1e15, 1e19,
	math.NaN(), math.Inf(1), math.Inf(-1),
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
		return bigFloats[r.Intn(len(bigFloats))]
	case 5:
		return nil
	case 6, 7:
		switch r.Intn(6) {
		case 0:
			m := map[int]any{}
			for range r.Intn(4) {
				m[r.Intn(40)-10] = genValue(r, depth-1)
			}
			return m
		case 1:
			m := map[bool]any{}
			for range r.Intn(3) {
				m[r.Intn(2) == 0] = genValue(r, depth-1)
			}
			return m
		}
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
// an int when it fits in an int64, and every other number comes back as
// the float it went in.
func want(v any) any {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && x >= -(1<<63) && x < 1<<63 {
			return int(x)
		}
		return x
	case map[string]any:
		m := map[string]any{}
		for k, e := range x {
			m[k] = want(e)
		}
		return m
	case map[int]any:
		m := map[string]any{}
		for k, e := range x {
			m[strconv.Itoa(k)] = want(e)
		}
		return m
	case map[bool]any:
		m := map[string]any{}
		for k, e := range x {
			m[strconv.FormatBool(k)] = want(e)
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

// sameValue is reflect.DeepEqual except that NaN equals NaN.
func sameValue(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || math.IsNaN(x) && math.IsNaN(y))
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, e := range x {
			f, ok := y[k]
			if !ok || !sameValue(e, f) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameValue(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
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
				t.Fatalf("case %d: MarshalWith(%#v, %+v): %v", i, v, order, err)
			}
			var got any
			if err := yaml.Unmarshal(out, &got); err != nil {
				t.Fatalf("case %d: Unmarshal(%q): %v", i, out, err)
			}
			if !sameValue(got, want(v)) {
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
			t.Fatalf("case %d: Marshal(%#v): %v", i, in, err)
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
				t.Fatalf("case %d: Marshal(%#v): %v", i, in, err)
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
	for _, f := range append([]float64{0.1, -0.1, 1e-7, 1.5e-7, 1e-300, 123.456, 2.5e-5, 1e6, 4294967296, -3, 0.30000000000000004}, bigFloats...) {
		out := mustMarshal(t, map[string]any{"v": f})
		var got map[string]any
		if err := yaml.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%v: %v", f, err)
		}
		if !sameValue(got["v"], want(f)) {
			t.Errorf("%v wrote %q read %#v", f, out, got["v"])
		}
		var typed struct {
			V float64 `yaml:"v"`
		}
		if err := yaml.Unmarshal([]byte(out), &typed); err != nil || !sameValue(typed.V, f) {
			t.Errorf("%v wrote %q read %v, %v", f, out, typed.V, err)
		}
	}
}

func TestMarshalWholeFloatsAtTheInt64Edge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"largest float below 2^63", 9223372036854774784, "9223372036854774784"},
		{"float64 of MaxInt64 is 2^63", float64(math.MaxInt64), "9.223372036854776e+18"},
		{"2^63", 9223372036854775808, "9.223372036854776e+18"},
		{"-2^63", -9223372036854775808, "-9223372036854775808"},
		{"below -2^63", -9223372036854777856, "-9.223372036854778e+18"},
		{"1e20", 1e20, "1.0e+20"},
		{"-1e20", -1e20, "-1.0e+20"},
		{"1e300", 1e300, "1.0e+300"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := mustMarshal(t, map[string]any{"v": tt.in})
			if out != "v: "+tt.want+"\n" {
				t.Errorf("wrote %q, want v: %s", out, tt.want)
			}
			var anyGot map[string]any
			if err := yaml.Unmarshal([]byte(out), &anyGot); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(anyGot["v"], want(tt.in)) {
				t.Errorf("into any: got %#v, want %#v", anyGot["v"], want(tt.in))
			}
		})
	}
}
