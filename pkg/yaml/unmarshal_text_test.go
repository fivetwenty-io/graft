package yaml_test

import (
	"errors"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// The yaml.v2 and yaml.v3 values cited below were recorded the same way
// as those in unmarshal_struct_test.go.

// TestUnmarshalStringKeepsSourceText checks that a string field holds a
// plain scalar as written, even where YAML reads it as a number or a
// boolean. yaml.v2 and yaml.v3 both give every value in the table.
func TestUnmarshalStringKeepsSourceText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.10", "1.10"},
		{"0x1F", "0x1F"},
		{"1e3", "1e3"},
		{"yes", "yes"},
		{"on", "on"},
		{"012", "012"},
		{"2001-12-14", "2001-12-14"},
		{"0o17", "0o17"},
		{"0b101", "0b101"},
		{"1_000", "1_000"},
		{".inf", ".inf"},
		{"-.5", "-.5"},
		{"+12", "+12"},
		{"-0", "-0"},
		{"1.0", "1.0"},
		{"True", "True"},
		{"FALSE", "FALSE"},
		{"!!str 1.10", "1.10"},
		{"!!str 0x1F", "0x1F"},
		{"~", ""},
	}
	for _, m := range modes {
		for _, tc := range cases {
			t.Run(m.name+"/"+tc.in, func(t *testing.T) {
				var got struct{ V string }
				if err := yaml.UnmarshalWith([]byte("v: "+tc.in+"\n"), &got, m.opts); err != nil {
					t.Fatalf("UnmarshalWith: %v", err)
				}
				if got.V != tc.want {
					t.Errorf("V = %q, want %q", got.V, tc.want)
				}
			})
		}
	}
}

type namedString string

// TextInner is exported because goccy refuses to set an unexported
// embedded struct, ,inline or not.
type TextInner struct {
	V string
}

type textOuter struct {
	TextInner `yaml:",inline"`
	N         int
}

// TextEmbedded is exported because goccy sets an embedded struct that
// has no ,inline option as an ordinary field.
type TextEmbedded struct {
	V string
}

type textEmbedded struct {
	TextEmbedded
	N int
}

type portLabel struct {
	D     string
	Label string
	Port  int
}

// textShapes puts numeric text into every shape that holds a string.
// Unless a row says otherwise, yaml.v2 and yaml.v3 both give its want.
var textShapes = []struct {
	name string
	doc  string
	mk   func() any
	want any
}{
	{"pointer", "v: 1.10\n", func() any { return &struct{ V *string }{} }, &struct{ V *string }{V: strPtr("1.10")}},
	{"flow slice", "v: [1.10, 0x1F, True]\n", func() any { return &struct{ V []string }{} },
		&struct{ V []string }{V: []string{"1.10", "0x1F", "True"}}},
	{"block slice", "v:\n- 1.10\n- 012\n", func() any { return &struct{ V []string }{} },
		&struct{ V []string }{V: []string{"1.10", "012"}}},
	{"array", "v: [1.10]\n", func() any { return &struct{ V [1]string }{} }, &struct{ V [1]string }{V: [1]string{"1.10"}}},
	{"map values", "v: {a: 1.10, b: 012}\n", func() any { return &struct{ V map[string]string }{} },
		&struct{ V map[string]string }{V: map[string]string{"a": "1.10", "b": "012"}}},
	// yaml.v2 also gives the null key as "", and yaml.v3 drops it.
	{"map keys", "v: {1.10: a, 0x1F: b, true: c, ~: d}\n", func() any { return &struct{ V map[string]string }{} },
		&struct{ V map[string]string }{V: map[string]string{"1.10": "a", "0x1F": "b", "true": "c", "": "d"}}},
	{"named string", "v: 1.10\n", func() any { return &struct{ V namedString }{} }, &struct{ V namedString }{V: "1.10"}},
	{"inline struct", "v: 1.10\nn: 2\n", func() any { return &textOuter{} }, &textOuter{TextInner: TextInner{V: "1.10"}, N: 2}},
	// An embedded struct with no ,inline option reads the key named for
	// its type in lower case, as in yaml.v2 and yaml.v3.
	{"embedded struct", "textembedded: {v: 1.10}\nn: 2\n", func() any { return &textEmbedded{} },
		&textEmbedded{TextEmbedded: TextEmbedded{V: "1.10"}, N: 2}},
	{"top-level map", "a: 1.10\n", func() any { return &map[string]string{} }, &map[string]string{"a": "1.10"}},
	{"top-level slice", "- 1.10\n", func() any { return &[]string{} }, &[]string{"1.10"}},
	{"one anchor, two types", "d: &d 0x1F\nport: *d\nlabel: *d\n", func() any { return &portLabel{} },
		&portLabel{D: "0x1F", Label: "0x1F", Port: 31}},
	{"one anchor, two types, string first", "d: &d 0x1F\nlabel: *d\nport: *d\n", func() any { return &portLabel{} },
		&portLabel{D: "0x1F", Label: "0x1F", Port: 31}},
	{"merge key", "d: &d {v: 1.10}\ne:\n  <<: *d\n", func() any { return &struct{ E TextInner }{} },
		&struct{ E TextInner }{E: TextInner{V: "1.10"}}},
	{"relocated literal", "a: &a |\n  one\n  two\nb:\n  c: *a\n", func() any {
		return &struct {
			A string
			B struct{ C string }
		}{}
	}, &struct {
		A string
		B struct{ C string }
	}{A: "one\ntwo\n", B: struct{ C string }{C: "one\ntwo\n"}}},
	{"relocated folded", "a: &a >-\n  one\n  two\nb:\n  - *a\n", func() any {
		return &struct {
			A string
			B []string
		}{}
	}, &struct {
		A string
		B []string
	}{A: "one two", B: []string{"one two"}}},
	{"float key collision", "1.10: a\n1.1: b\n", func() any { return &map[string]string{} },
		&map[string]string{"1.10": "a", "1.1": "b"}},
	{"integer key collision", "0x10: a\n16: b\n", func() any { return &map[string]string{} },
		&map[string]string{"0x10": "a", "16": "b"}},
	// A merge list into a struct follows the generic route, where the
	// earlier map wins, as in spruce, yaml.v2, and yaml.v3, so N is 1.
	{"merge list", "a: &a {v: 1.10, n: 1}\nb: &b {n: 2}\nc:\n  <<: [*a, *b]\n", func() any { return &struct{ C textOuter }{} },
		&struct{ C textOuter }{C: textOuter{TextInner: TextInner{V: "1.10"}, N: 1}}},
	{"beside a generic value", "v: 1.10\na: \"yes\"\n", func() any {
		return &struct {
			V string
			A any
		}{}
	}, &struct {
		V string
		A any
	}{V: "1.10", A: "yes"}},
}

func strPtr(s string) *string { return &s }

func TestUnmarshalStringShapesKeepSourceText(t *testing.T) {
	for _, m := range modes {
		for _, tc := range textShapes {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				got := tc.mk()
				if err := yaml.UnmarshalWith([]byte(tc.doc), got, m.opts); err != nil {
					t.Fatalf("UnmarshalWith: %v", err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("got  %+v\nwant %+v", reflect.ValueOf(got).Elem(), reflect.ValueOf(tc.want).Elem())
				}
			})
		}
	}
}

// TestUnmarshalInlineMapKeepsItsValues checks that a struct whose ,inline
// map reads every key leaves a value alone when the map and a field want
// different types, and rewrites it when they both want a string. goccy
// hands an ,inline map every key, including those that name a field,
// where yaml.v2 and yaml.v3 leave those keys out, so name is in Rest.
func TestUnmarshalInlineMapKeepsItsValues(t *testing.T) {
	type ints struct {
		Name string         `yaml:"name"`
		Rest map[string]int `yaml:",inline"`
	}
	type strs struct {
		Name string            `yaml:"name"`
		Rest map[string]string `yaml:",inline"`
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			var gotInts ints
			if err := yaml.UnmarshalWith([]byte("port: 0x1F\n"), &gotInts, m.opts); err != nil {
				t.Fatalf("UnmarshalWith ints: %v", err)
			}
			wantDeepEqual(t, gotInts.Rest, map[string]int{"port": 31})

			var gotStrs strs
			if err := yaml.UnmarshalWith([]byte("name: 1.10\nv: 0x1F\n"), &gotStrs, m.opts); err != nil {
				t.Fatalf("UnmarshalWith strs: %v", err)
			}
			want := strs{Name: "1.10", Rest: map[string]string{"name": "1.10", "v": "0x1F"}}
			if !reflect.DeepEqual(gotStrs, want) {
				t.Errorf("got %+v, want %+v", gotStrs, want)
			}
		})
	}
}

// TestUnmarshalStructNonStringKeys checks that a key YAML reads as a
// number, a boolean, or a null leaves the other fields of the struct to
// decode. yaml.v2 and yaml.v3 both give Name "x" and Port 1 for each.
func TestUnmarshalStructNonStringKeys(t *testing.T) {
	type named struct {
		Name string
		Port int
	}
	for _, m := range modes {
		for _, key := range []string{"80", "true", "~", "1.5", ".inf", "? 80\n:"} {
			t.Run(m.name+"/"+key, func(t *testing.T) {
				var got named
				doc := "name: x\n" + key + " http\nport: 1\n"
				if key[0] != '?' {
					doc = "name: x\n" + key + ": http\nport: 1\n"
				}
				if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
					t.Fatalf("UnmarshalWith: %v", err)
				}
				if want := (named{Name: "x", Port: 1}); got != want {
					t.Errorf("got %+v, want %+v", got, want)
				}
			})
		}
		t.Run(m.name+"/key names a field", func(t *testing.T) {
			var got struct {
				Port string `yaml:"80"`
			}
			if err := yaml.UnmarshalWith([]byte("80: http\n"), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			if got.Port != "http" {
				t.Errorf("Port = %q, want %q", got.Port, "http")
			}
		})
	}
}

// TestUnmarshalGenericMapKeysMatchTopLevel checks that the keys of a
// map[string]any field are those a top-level decode gives.
func TestUnmarshalGenericMapKeysMatchTopLevel(t *testing.T) {
	const keys = "~: a\n1.10: b\n0x1F: c\ntrue: d\n"
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			var top map[string]any
			if err := yaml.UnmarshalWith([]byte(keys), &top, m.opts); err != nil {
				t.Fatalf("top-level: %v", err)
			}
			var got struct {
				M map[string]any
				L []map[string]any
			}
			doc := "m:\n" + indentLines(keys, "  ") + "l:\n- " + indentLines(keys, "  ")[2:]
			if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
				t.Fatalf("nested: %v", err)
			}
			wantDeepEqual(t, got.M, top)
			wantDeepEqual(t, got.L, []map[string]any{top})
		})
	}
}

func indentLines(s, prefix string) string {
	out := ""
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			out += prefix + s[start:i+1]
			start = i + 1
		}
	}
	return out
}

// TestUnmarshalErrorPathConvertsGenericValues checks that a decode that
// fails part way still converts the generic values it filled and strips
// the quoted-boolean marker from its strings.
func TestUnmarshalErrorPathConvertsGenericValues(t *testing.T) {
	type partial struct {
		S string
		G map[string]any
		P *string
		N int
	}
	const doc = "s: \"on\"\ng: {a: \"yes\", b: 1, c: on}\np: 'off'\nn: abc\n"
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			var got partial
			err := yaml.UnmarshalWith([]byte(doc), &got, m.opts)
			wantDecodeError(t, err, yaml.ErrorKindType, 4)
			wantNoMarker(t, reflect.ValueOf(got), "got")
			want := map[string]any{"a": "yes", "b": 1, "c": "on"}
			if m.yaml11 {
				want["c"] = true
			}
			wantDeepEqual(t, got.G, want)
			if got.S != "on" || got.P == nil || *got.P != "off" {
				t.Errorf("S = %q, P = %v, want on and off", got.S, got.P)
			}
		})
	}
}

// TestUnmarshalSelfDecodingTypes checks the types goccy decodes with
// their own rules. Each refusal is a type error on its true line, as in
// yaml.v2 and yaml.v3, and quotes nothing from the input.
func TestUnmarshalSelfDecodingTypes(t *testing.T) {
	type self struct {
		X  int
		D  time.Duration
		T  time.Time
		IP netip.Addr
		Ts []time.Time
	}
	refusals := []struct {
		name string
		doc  string
		line int
	}{
		// yaml.v2 and yaml.v3 both refuse on line 2.
		{"duration", "x: 1\nd: S3KRITzq9\n", 2},
		// yaml.v3 refuses an integer, and yaml.v2 reads it as nanoseconds.
		{"integer duration", "x: 1\nd: 10\n", 2},
		// yaml.v2 and yaml.v3 both refuse.
		{"address", "x: 1\nip: S3KRITzq9\n", 2},
		{"time", "x: 1\nt: S3KRITzq9\n", 2},
		{"empty time", "x: 1\nt: \"\"\n", 2},
		{"time in a slice", "x: 1\nts:\n- 2001-12-14\n- S3KRITzq9\n", 4},
	}
	for _, m := range modes {
		for _, tc := range refusals {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				var got self
				err := yaml.UnmarshalWith([]byte(tc.doc), &got, m.opts)
				wantDecodeError(t, err, yaml.ErrorKindType, tc.line)
				wantNoLeak(t, err, "S3KRIT")
			})
		}
		t.Run(m.name+"/accepted", func(t *testing.T) {
			var got self
			// yaml.v2 gives the same, and yaml.v3 drops the null time.
			doc := "d: 1h30m\nt: 2001-12-14\nip: 10.0.0.1\nts: [2001-12-14 21:59:43.10, ~]\n"
			if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			want := self{
				D:  90 * time.Minute,
				T:  time.Date(2001, time.December, 14, 0, 0, 0, 0, time.UTC),
				IP: netip.MustParseAddr("10.0.0.1"),
				Ts: []time.Time{time.Date(2001, time.December, 14, 21, 59, 43, 100000000, time.UTC), {}},
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// TestUnmarshalCyclicHeldValue checks that a generic value the target
// already held, and that holds itself, does not stop the decode.
func TestUnmarshalCyclicHeldValue(t *testing.T) {
	type held struct {
		X any
		Y int
	}
	loop := map[string]any{"w": "yes"}
	loop["self"] = loop
	list := []any{"on", nil}
	list[1] = list
	for _, x := range []any{loop, list} {
		got := held{X: x}
		if err := yaml.Unmarshal([]byte("y: 1\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got.Y != 1 {
			t.Errorf("Y = %d, want 1", got.Y)
		}
	}
}

// TestUnmarshalTaggedScalarsKeepSourceText checks explicitly tagged
// scalars in string places and in keys. yaml.v2 and yaml.v3 both give
// every value below.
func TestUnmarshalTaggedScalarsKeepSourceText(t *testing.T) {
	values := []struct{ in, want string }{
		{"!!int 0x1F", "0x1F"},
		{"!!int 012", "012"},
		{"!!float 1.10", "1.10"},
		{"!!float 1e3", "1e3"},
		{"!!bool True", "True"},
		{"!!null ~", ""},
		{"!!str 0x1F", "0x1F"},
	}
	type named struct{ Name string }
	for _, m := range modes {
		for _, tc := range values {
			t.Run(m.name+"/value "+tc.in, func(t *testing.T) {
				var got struct{ V []string }
				doc := "v: [" + tc.in + "]\n"
				if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
					t.Fatalf("UnmarshalWith: %v", err)
				}
				wantDeepEqual(t, got.V, []string{tc.want})
			})
		}
		for _, key := range []string{"!!int 80", "!!float 1.5", "!!bool true", "!!null ~", "!!str 80"} {
			t.Run(m.name+"/struct key "+key, func(t *testing.T) {
				var got named
				if err := yaml.UnmarshalWith([]byte(key+": x\nname: y\n"), &got, m.opts); err != nil {
					t.Fatalf("UnmarshalWith: %v", err)
				}
				if got.Name != "y" {
					t.Errorf("Name = %q, want %q", got.Name, "y")
				}
			})
		}
		t.Run(m.name+"/map keys", func(t *testing.T) {
			var got map[string]string
			doc := "{!!str 1.10: a, 1.1: b, !!int 0x10: c, 16: d}\n"
			if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			wantDeepEqual(t, got, map[string]string{"1.10": "a", "1.1": "b", "0x10": "c", "16": "d"})
		})
	}
}

// echoText is a TextUnmarshaler whose refusal repeats its input without
// quotes.
type echoText struct{}

func (*echoText) UnmarshalText(b []byte) error { return errors.New("bad value " + string(b)) }

// TestUnmarshalOwnRefusalsQuoteNothing checks that the refusal of a type
// that decodes itself names only the type, located or not, even when
// the type's own error repeats the input.
func TestUnmarshalOwnRefusalsQuoteNothing(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		v    func() any
		line int
	}{
		{"net.IP", "x: 1\nip: ipS3KRITa\n", func() any {
			return &struct {
				X  int
				IP net.IP
			}{}
		}, 2},
		{"netip.Addr", "x: 1\nip: addrS3KRITb\n", func() any {
			return &struct {
				X  int
				IP netip.Addr
			}{}
		}, 2},
		{"time.Duration", "x: 1\nd: durS3KRITc\n", func() any {
			return &struct {
				X int
				D time.Duration
			}{}
		}, 2},
		{"time.Time", "x: 1\nt: timeS3KRITd\n", func() any {
			return &struct {
				X int
				T time.Time
			}{}
		}, 2},
		{"echoing UnmarshalText", "x: 1\ne: echoS3KRITe\n", func() any {
			return &struct {
				X int
				E echoText
			}{}
		}, 2},
		// The same refusal on two lines cannot be placed.
		{"two lines", "a: twoS3KRITf\nb: twoS3KRITf\n", func() any { return &struct{ A, B echoText }{} }, 0},
		// A sequence never reaches UnmarshalText as a scalar, so no
		// candidate matches.
		{"echo of a sequence", "e: [seqS3KRITg]\n", func() any { return &struct{ E echoText }{} }, 0},
	}
	for _, m := range modes {
		for _, tc := range cases {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				err := yaml.UnmarshalWith([]byte(tc.doc), tc.v(), m.opts)
				wantDecodeError(t, err, yaml.ErrorKindType, tc.line)
				wantNoLeak(t, err, "S3KRIT")
			})
		}
	}
}
