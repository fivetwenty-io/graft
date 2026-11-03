package yaml_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// explicitTagDocs are documents whose scalars carry an explicit core
// tag, with the value real spruce v1.35.17 printed for key a. Every
// pkg/yaml route reads them alike, in both modes.
var explicitTagDocs = []struct {
	doc  string
	want any
}{
	{"a: !!str yes\n", "yes"},
	{"a: !!str No\n", "No"},
	{"a: !!str ~\n", "~"},
	{"a: !!str null\n", "null"},
	{"a: !!str 1.50\n", "1.50"},
	{"a: !!str 012\n", "012"},
	{"a: !!str |-\n  yes\n", "yes"},
	{"a: !<tag:yaml.org,2002:str> yes\n", "yes"},
	{"a: !!bool ON\n", true},
	{"a: !!bool n\n", false},
	{"a: !!bool \"yes\"\n", true},
	{"a: !!bool 'off'\n", false},
	{"a: !!int 0x1f\n", 31},
	{"a: !!int \"0x1f\"\n", 31},
	{"a: !!int 0B101\n", 5},
	{"a: !!int -0b101\n", -5},
	{"a: !!int 0o17\n", 15},
	{"a: !!int 012\n", 10},
	{"a: !!int 1_000\n", 1000},
	{"a: !!int 18446744073709551615\n", uint64(math.MaxUint64)},
	{"a: !!float 1e3\n", 1000.0},
	// graft reads integer text under !!float as a float. spruce
	// refuses it, so the cases down to the quoted ones are deliberate
	// divergences.
	{"a: !!float 3\n", 3.0},
	{"a: !!float -7\n", -7.0},
	{"a: !!float +7\n", 7.0},
	{"a: !!float 0x1F\n", 31.0},
	{"a: !!float 0o17\n", 15.0},
	{"a: !!float 0b101\n", 5.0},
	{"a: !!float 1_000\n", 1000.0},
	{"a: !!float \"3\"\n", 3.0},
	{"a: !!float '-7'\n", -7.0},
	{"a: !!float 09\n", 9.0},
	{"a: !!float 9223372036854775807\n", float64(math.MaxInt64)},
	{"a: !!float -9223372036854775808\n", float64(math.MinInt64)},
	{"a: !!float 18446744073709551615\n", float64(math.MaxUint64)},
	{"a: !!float 0x1p3\n", 8.0},
	{"a: !!float -.inf\n", math.Inf(-1)},
	{"a: !!null \"~\"\n", nil},
	{"a: !!null \"\"\n", nil},
	{"a: !!binary aGk=\n", "hi"},
	{"a: !!binary |\n  aGVsbG8g\n  d29ybGQ=\n", "hello world"},
	{"a: !!timestamp word\n", "word"},
	{"a: !!timestamp 1\n", "1"},
	{"a: !!timestamp 2001-12-14\n", "2001-12-14"},
	{"a: !foo yes\n", "yes"},
	{"a: ! on\n", "on"},
	{"a: !foo >-\n  Off\n", "Off"},
	{"a: !foo 12\n", "12"},
}

// TestExplicitTagsMatchSpruce checks Unmarshal, in both modes, against
// spruce's value. graft merge reads the same documents through the same
// pass, which pkg/graft's tests and tests/yamlparity check.
func TestExplicitTagsMatchSpruce(t *testing.T) {
	t.Parallel()
	for _, tc := range explicitTagDocs {
		for _, m := range modes {
			t.Run(m.name+"/"+tc.doc, func(t *testing.T) {
				t.Parallel()
				var got map[string]any
				if err := yaml.UnmarshalWith([]byte(tc.doc), &got, m.opts); err != nil {
					t.Fatalf("UnmarshalWith: %v", err)
				}
				if !reflect.DeepEqual(got["a"], tc.want) {
					t.Errorf("a = %#v, want %#v", got["a"], tc.want)
				}
			})
		}
	}
}

// TestExplicitTagsTypedFields checks the typed route, where a string
// field takes the text as written, as yaml.v2 gives it, and the other
// fields take the tag's value.
func TestExplicitTagsTypedFields(t *testing.T) {
	t.Parallel()
	type target struct {
		S   string   `yaml:"s"`
		Raw string   `yaml:"raw"`
		Bin string   `yaml:"bin"`
		B   bool     `yaml:"b"`
		I   int      `yaml:"i"`
		F   float64  `yaml:"f"`
		P   *int     `yaml:"p"`
		L   []string `yaml:"l"`
	}
	const doc = "s: !!str yes\nraw: !!int 0x1F\nbin: !!binary aGk=\nb: !!bool Off\ni: !!int 0b101\nf: !!float 1_000.5\np: !!null ~\nl: [!!str on, !!str 1.50]\n"
	want := target{S: "yes", Raw: "0x1F", Bin: "hi", B: false, I: 5, F: 1000.5, L: []string{"on", "1.50"}}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			got := target{P: new(int)}
			if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// TestTimestampTagTypedFields checks that a !!timestamp scalar still
// fills a time.Time field, while a string field takes its text.
func TestTimestampTagTypedFields(t *testing.T) {
	t.Parallel()
	type target struct {
		T time.Time `yaml:"t"`
		S string    `yaml:"s"`
	}
	const doc = "t: !!timestamp 2001-12-14\ns: !!timestamp 2001-12-14t21:59:43.10-05:00\n"
	want := target{T: time.Date(2001, 12, 14, 0, 0, 0, 0, time.UTC), S: "2001-12-14t21:59:43.10-05:00"}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			var got target
			if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
				t.Fatal(err)
			}
			if !got.T.Equal(want.T) || got.S != want.S {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// TestExplicitTagsNodeDecode checks that Node.Decode, on a tree that
// Unmarshal read and on a hand-built tree, gives what Unmarshal gives.
func TestExplicitTagsNodeDecode(t *testing.T) {
	t.Parallel()
	for _, tc := range explicitTagDocs {
		for _, m := range modes {
			t.Run(m.name+"/"+tc.doc, func(t *testing.T) {
				t.Parallel()
				var n yaml.Node
				if err := yaml.UnmarshalWith([]byte(tc.doc), &n, m.opts); err != nil {
					t.Fatal(err)
				}
				var got map[string]any
				if err := n.Decode(&got); err != nil {
					t.Fatalf("Decode: %v", err)
				}
				if !reflect.DeepEqual(got["a"], tc.want) {
					t.Errorf("a = %#v, want %#v", got["a"], tc.want)
				}
			})
		}
	}

	scalar := func(tag, value string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value} }
	for _, c := range []struct {
		n    *yaml.Node
		want any
	}{
		{scalar("!!str", "off"), "off"},
		{scalar("!!str", "~"), "~"},
		{scalar("!!bool", "Y"), true},
		{scalar("!!int", "0o17"), 15},
		{scalar("!!float", "1.50"), 1.5},
		{scalar("!!null", "NULL"), nil},
	} {
		// The node sits in a sequence, since a document that is null
		// leaves its target unchanged.
		var v any
		seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{c.n}}
		if err := seq.Decode(&v); err != nil {
			t.Errorf("%s %s: %v", c.n.Tag, c.n.Value, err)
			continue
		}
		if !reflect.DeepEqual(v, []any{c.want}) {
			t.Errorf("%s %s decoded to %#v, want %#v", c.n.Tag, c.n.Value, v, c.want)
		}
	}
}

// TestExplicitTagErrors checks that a tag on text it cannot hold fails
// on every route, as spruce fails, with an error that names the tags and
// the tag's position but never the text.
func TestExplicitTagErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		doc, msg string
		column   int
	}{
		{"a: !!int S3KRIT\n", "cannot decode a !!str scalar as !!int", 4},
		{"a: !!int 1.5\n", "cannot decode a !!float scalar as !!int", 4},
		{"a: !!int yes\n", "cannot decode a !!bool scalar as !!int", 4},
		{"a: !!int ~\n", "cannot decode a !!null scalar as !!int", 4},
		{"a: !!bool tRue\n", "cannot decode a !!str scalar as !!bool", 4},
		{"a: !!bool 1\n", "cannot decode a !!int scalar as !!bool", 4},
		{"a: !!null S3KRIT\n", "cannot decode a !!str scalar as !!null", 4},
		{"a: !!binary S3KRIT\n", "!!binary value contains invalid base64 data", 4},
		{"a: [1, !!int S3KRIT]\n", "cannot decode a !!str scalar as !!int", 8},
		{"!!int S3KRIT: a\n", "cannot decode a !!str scalar as !!int", 1},
	}
	type typed struct {
		A any `yaml:"a"`
	}
	for _, tc := range cases {
		for _, m := range modes {
			t.Run(m.name+"/"+tc.doc, func(t *testing.T) {
				t.Parallel()
				var generic any
				var st typed
				for _, err := range []error{
					yaml.UnmarshalWith([]byte(tc.doc), &generic, m.opts),
					yaml.UnmarshalWith([]byte(tc.doc), &st, m.opts),
				} {
					de := wantDecodeError(t, err, yaml.ErrorKindSyntax, 1)
					wantNoLeak(t, err, "S3KRIT", "tRue", "1.5")
					if de.Msg != tc.msg || de.Column != tc.column {
						t.Errorf("Msg %q at column %d, want %q at %d", de.Msg, de.Column, tc.msg, tc.column)
					}
				}
			})
		}
	}
}

// TestExplicitTagSyntaxErrors checks the two tag placements spruce's
// parser refuses, a second tag on one node and a tag on an alias.
func TestExplicitTagSyntaxErrors(t *testing.T) {
	t.Parallel()
	for _, doc := range []string{
		"a: !!str !!int S3KRIT\n",
		"a: !!int &x !!int S3KRIT\n",
		"x: &y S3KRIT\na: !!int *y\n",
	} {
		t.Run(doc, func(t *testing.T) {
			t.Parallel()
			var v any
			err := yaml.Unmarshal([]byte(doc), &v)
			wantDecodeError(t, err, yaml.ErrorKindSyntax, lineOf(doc))
			wantNoLeak(t, err, "S3KRIT")
		})
	}
}

// TestBlockScalarBoolWordsStayStrings checks that a block scalar holding
// a YAML 1.1 boolean word decodes as that word in both modes, as spruce
// reads it, while a plain yes is still true in the default mode.
func TestBlockScalarBoolWordsStayStrings(t *testing.T) {
	t.Parallel()
	const doc = "a: |-\n  yes\nb: >-\n  Off\nc:\n- |-\n  NO\nd: &x |-\n  on\ne: *x\n"
	want := map[string]any{"a": "yes", "b": "Off", "c": []any{"NO"}, "d": "on", "e": "on"}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			var got map[string]any
			if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %#v, want %#v", got, want)
			}
			var n yaml.Node
			if err := yaml.UnmarshalWith([]byte(doc), &n, m.opts); err != nil {
				t.Fatal(err)
			}
			var viaNode map[string]any
			if err := n.Decode(&viaNode); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(viaNode, want) {
				t.Errorf("Node.Decode gave %#v, want %#v", viaNode, want)
			}
		})
	}
}
