package yaml_test

import (
	"math"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/sortkeys"
	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// The expected text of the v2 comparisons below is what yaml.v2 v2.4.0
// writes for the same Go value, recorded with a throwaway program, so
// the test needs no yaml.v2 import.

type marshalInner struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type marshalBase struct {
	Name string `yaml:"name"`
	Tier string `yaml:"tier,omitempty"`
}

type marshalPlan struct {
	marshalBase `yaml:",inline"`
	ID          string            `yaml:"id"`
	Zones       []string          `yaml:"azs,flow"`
	Limits      map[string]int    `yaml:"limits,flow"`
	Props       marshalInner      `yaml:"props"`
	Notes       string            `yaml:"notes,omitempty"`
	Skip        []string          `yaml:"skip,omitempty"`
	Tags        map[string]string `yaml:"tags,omitempty"`
	Enabled     bool              `yaml:"enabled"`
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	out, err := yaml.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(out)
}

func TestMarshalStructShapesMatchV2(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   marshalPlan
		want string
	}{
		{
			name: "flow and inline",
			in: marshalPlan{
				marshalBase: marshalBase{Name: "small"}, ID: "p1",
				Zones:  []string{"z1", "z2"},
				Limits: map[string]int{"cpu": 2, "mem": 4},
				Props:  marshalInner{Host: "db", Port: 5432}, Enabled: true,
			},
			want: "name: small\nid: p1\nazs: [z1, z2]\nlimits: {cpu: 2, mem: 4}\nprops:\n  host: db\n  port: 5432\nenabled: true\n",
		},
		{
			name: "omitempty kept and empty collections",
			in: marshalPlan{
				marshalBase: marshalBase{Name: "big", Tier: "gold"}, ID: "p2", Notes: "x",
				Skip: []string{"a"}, Tags: map[string]string{"k": "v"},
			},
			want: "name: big\ntier: gold\nid: p2\nazs: []\nlimits: {}\nprops:\n  host: \"\"\n  port: 0\nnotes: x\nskip:\n- a\ntags:\n  k: v\nenabled: false\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mustMarshal(t, tt.in); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestMarshalMapShapesMatchV2(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "quoting",
			in: map[string]any{
				"yes": "yes", "num": "123", "empty": "", "colon": "a: b", "hash": "a #b",
				"tilde": "~", "true": "true", "null": "null", "lead": " lead",
				"plain": "hello world", "dash": "- x", "star": "*x",
			},
			want: "colon: 'a: b'\ndash: '- x'\nempty: \"\"\nhash: 'a #b'\nlead: ' lead'\n\"null\": \"null\"\nnum: \"123\"\nplain: hello world\nstar: '*x'\ntilde: \"~\"\n\"true\": \"true\"\n\"yes\": \"yes\"\n",
		},
		{
			name: "block scalars",
			in:   map[string]any{"cert": "line1\nline2\n", "two": "a\nb"},
			want: "cert: |\n  line1\n  line2\ntwo: |-\n  a\n  b\n",
		},
		{
			name: "nested",
			in: map[string]any{
				"list": []any{"a", map[string]any{"b": 1}, []any{1, 2}},
				"e":    []any{}, "m": map[string]any{},
			},
			want: "e: []\nlist:\n- a\n- b: 1\n- - 1\n  - 2\nm: {}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mustMarshal(t, tt.in); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// The key orders below are the ones the key-order documentation of
// sortkeys gives: spruce puts numbers by value and keeps a leading
// underscore with the letters, lexical order compares code points.
func TestMarshalKeyOrder(t *testing.T) {
	t.Parallel()
	in := map[string]any{"a10": 1, "a2": 2, "9": 3, "10": 4, "_x": 5}
	tests := []struct {
		name  string
		order sortkeys.Mode
		want  string
	}{
		{"zero value is spruce", "", "\"9\": 3\n\"10\": 4\n_x: 5\na2: 2\na10: 1\n"},
		{"spruce", sortkeys.Spruce, "\"9\": 3\n\"10\": 4\n_x: 5\na2: 2\na10: 1\n"},
		{"lexical", sortkeys.Lexical, "\"10\": 4\n\"9\": 3\n_x: 5\na10: 1\na2: 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := yaml.MarshalWith(in, yaml.EncodeOptions{KeyOrder: tt.order})
			if err != nil {
				t.Fatalf("MarshalWith: %v", err)
			}
			if string(out) != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", out, tt.want)
			}
		})
	}
}

func TestMarshalDefaultIsSpruceOrder(t *testing.T) {
	t.Parallel()
	in := map[string]any{"a10": 1, "a2": 2}
	if got, want := mustMarshal(t, in), "a2: 2\na10: 1\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarshalWithRefusesUnknownKeyOrders(t *testing.T) {
	t.Parallel()
	for _, order := range []sortkeys.Mode{sortkeys.None, "bogus", "Spruce"} {
		t.Run(string(order), func(t *testing.T) {
			t.Parallel()
			out, err := yaml.MarshalWith(map[string]any{"b": 1, "a": 2}, yaml.EncodeOptions{KeyOrder: order})
			if err == nil {
				t.Fatalf("MarshalWith(%q) = %q, want an error", order, out)
			}
			if out != nil {
				t.Errorf("MarshalWith(%q) returned %q with its error", order, out)
			}
		})
	}
}

// Floats and timestamps follow yaml.v3, whose output the broker's
// provision parameters were written against.
func TestMarshalFloats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"whole", map[string]any{"v": 3.0}, "v: 3\n"},
		{"negative whole", map[string]any{"v": -2.0}, "v: -2\n"},
		{"fraction", map[string]any{"v": 3.5}, "v: 3.5\n"},
		{"nan", map[string]any{"v": math.NaN()}, "v: .nan\n"},
		{"positive infinity", map[string]any{"v": math.Inf(1)}, "v: .inf\n"},
		{"negative infinity", map[string]any{"v": math.Inf(-1)}, "v: -.inf\n"},
		{"large whole", map[string]any{"v": 1e20}, "v: 100000000000000000000\n"},
		{"million", map[string]any{"v": 1e6}, "v: 1000000\n"},
		{"seconds since epoch", map[string]any{"v": 1700000000.0}, "v: 1700000000\n"},
		{"negative zero", map[string]any{"v": math.Copysign(0, -1)}, "v: 0\n"},
		{"small fraction", map[string]any{"v": 1e-7}, "v: 1.0e-07\n"},
		{"small fraction with digits", map[string]any{"v": 1.5e-7}, "v: 1.5e-07\n"},
		{"float32 whole", map[string]any{"v": float32(2)}, "v: 2\n"},
		{"float32 fraction", map[string]any{"v": float32(0.1)}, "v: 0.1\n"},
		{"nested in maps and lists", map[string]any{"a": []any{1.0, map[string]any{"b": 2.5, "c": 4e9}}}, "a:\n- 1\n- b: 2.5\n  c: 4000000000\n"},
		{"typed slice", []float64{1, 2.5}, "- 1\n- 2.5\n"},
		{"typed map", map[string]float32{"x": 3}, "x: 3\n"},
		{"in a list", []any{3.0, 3.5}, "- 3\n- 3.5\n"},
		{"in a struct", struct {
			F float64 `yaml:"f"`
		}{3.0}, "f: 3\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mustMarshal(t, tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMarshalTimestampStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"date", map[string]any{"v": "2026-01-02"}, "v: \"2026-01-02\"\n"},
		{"datetime", map[string]any{"v": "2026-01-02T03:04:05Z"}, "v: \"2026-01-02T03:04:05Z\"\n"},
		{"spaced datetime", map[string]any{"v": "2026-01-02 03:04:05.5 -5"}, "v: \"2026-01-02 03:04:05.5 -5\"\n"},
		{"plain string", map[string]any{"v": "2026 plan"}, "v: 2026 plan\n"},
		{"near miss", map[string]any{"v": "2026-1-2"}, "v: 2026-1-2\n"},
		{"in a list", []any{"2026-01-02"}, "- \"2026-01-02\"\n"},
		{"as a key", map[string]any{"2026-01-02": "x"}, "\"2026-01-02\": x\n"},
		{"in a struct", struct {
			D string `yaml:"d"`
		}{"2026-01-02"}, "d: \"2026-01-02\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mustMarshal(t, tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

type marshalHolder struct {
	N yaml.Node `yaml:"n"`
}

type marshalNested struct {
	List []any
}

type marshalLoop struct {
	Any  any
	Next *marshalLoop
}

func TestMarshalRefusesNodes(t *testing.T) {
	t.Parallel()
	n := yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "secret-value"}
	loop := &marshalLoop{}
	loop.Next = loop
	loop.Any = []any{&n, loop}
	tests := []struct {
		name string
		in   any
	}{
		{"value", n},
		{"pointer", &n},
		{"struct field", marshalHolder{N: n}},
		{"slice", []yaml.Node{n}},
		{"array", [1]*yaml.Node{&n}},
		{"map value", map[string]*yaml.Node{"k": &n}},
		{"interface", map[string]any{"k": []any{map[string]any{"x": &n}}}},
		{"struct holding interface", marshalNested{List: []any{n}}},
		{"cycle", loop},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := yaml.Marshal(tt.in)
			if err == nil {
				t.Fatalf("Marshal = %q, want an error", out)
			}
			if out != nil {
				t.Errorf("Marshal returned %q with its error", out)
			}
			if !strings.Contains(err.Error(), "yaml.Node") {
				t.Errorf("error %q does not name yaml.Node", err)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Errorf("error %q quotes a value", err)
			}
		})
	}
}

func TestMarshalRefusesCycles(t *testing.T) {
	t.Parallel()
	loop := &marshalLoop{}
	loop.Next = loop
	m := map[string]any{}
	m["self"] = m
	shared := &marshalInner{Host: "h", Port: 1}
	tests := []struct {
		name string
		in   any
	}{
		{"pointer cycle", loop},
		{"map cycle", m},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := yaml.Marshal(tt.in)
			if err == nil {
				t.Fatalf("Marshal = %q, want an error", out)
			}
			if out != nil {
				t.Errorf("Marshal returned %q with its error", out)
			}
		})
	}
	// A value reached twice is shared, not cyclic, and it encodes.
	got := mustMarshal(t, []*marshalInner{shared, shared})
	if want := "- host: h\n  port: 1\n- host: h\n  port: 1\n"; got != want {
		t.Errorf("shared value: got %q, want %q", got, want)
	}
}

type marshalPanics struct{}

func (marshalPanics) MarshalYAML() (any, error) { panic("user method failed") }

func TestMarshalNeverPanicsOnUnencodableValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   any
	}{
		{"channel", map[string]any{"c": make(chan int)}},
		{"func", map[string]any{"f": func() {}}},
		{"top-level channel", make(chan int)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := yaml.Marshal(tt.in)
			if err == nil {
				t.Fatalf("Marshal = %q, want an error", out)
			}
			if out != nil {
				t.Errorf("Marshal returned %q with its error", out)
			}
			if strings.Contains(err.Error(), "s3cret") {
				t.Errorf("error %q quotes a value", err)
			}
		})
	}
}

func TestMarshalReraisesUserPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != "user method failed" {
			t.Errorf("recovered %v, want the method's own panic", r)
		}
	}()
	_, _ = yaml.Marshal(marshalPanics{})
	t.Error("Marshal returned without panicking")
}

func TestMarshalFloatStringsAreQuoted(t *testing.T) {
	t.Parallel()
	in := []string{".inf", "-.inf", "+.inf", ".NaN", ".nan", "<<"}
	want := "- \".inf\"\n- \"-.inf\"\n- \"+.inf\"\n- \".NaN\"\n- \".nan\"\n- \"<<\"\n"
	if got := mustMarshal(t, in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarshalNonStringKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    any
		order sortkeys.Mode
		want  string
	}{
		{"int keys in spruce order", map[int]string{10: "a", 9: "b", 2: "e", -1: "d", 100: "f"}, sortkeys.Spruce, "-1: d\n2: e\n9: b\n10: a\n100: f\n"},
		{"int keys in lexical order", map[int]string{10: "a", 9: "b", 2: "e", -1: "d", 100: "f"}, sortkeys.Lexical, "-1: d\n10: a\n100: f\n2: e\n9: b\n"},
		{"uint8 keys", map[uint8]int{10: 1, 9: 2}, sortkeys.Spruce, "9: 2\n10: 1\n"},
		{"float keys", map[float64]int{1.5: 1, 2: 2}, sortkeys.Spruce, "1.5: 1\n2: 2\n"},
		{"bool keys", map[bool]int{true: 1, false: 2}, sortkeys.Spruce, "false: 2\ntrue: 1\n"},
		{"nested int keys", map[string]map[int]int{"m": {10: 1, 9: 2}}, sortkeys.Spruce, "m:\n  9: 2\n  10: 1\n"},
		{"any keys keep their type", map[any]any{"b": 1, 2: 2, true: 6, 1.5: 5}, sortkeys.Spruce, "1.5: 5\n2: 2\nb: 1\ntrue: 6\n"},
		{"string key that looks like a number is quoted", map[string]int{"9": 1, "10": 2}, sortkeys.Spruce, "\"9\": 1\n\"10\": 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := yaml.MarshalWith(tt.in, yaml.EncodeOptions{KeyOrder: tt.order})
			if err != nil {
				t.Fatalf("MarshalWith: %v", err)
			}
			if string(out) != tt.want {
				t.Errorf("got %q, want %q", out, tt.want)
			}
		})
	}
}

func TestMarshalNestedMapsInBothOrders(t *testing.T) {
	t.Parallel()
	in := map[string]any{"b": map[string]any{"a10": 1, "a2": 2}, "a": []any{map[string]any{"z10": 1, "z9": 2}}}
	tests := []struct {
		order sortkeys.Mode
		want  string
	}{
		{sortkeys.Spruce, "a:\n- z9: 2\n  z10: 1\nb:\n  a2: 2\n  a10: 1\n"},
		{sortkeys.Lexical, "a:\n- z10: 1\n  z9: 2\nb:\n  a10: 1\n  a2: 2\n"},
	}
	for _, tt := range tests {
		out, err := yaml.MarshalWith(in, yaml.EncodeOptions{KeyOrder: tt.order})
		if err != nil || string(out) != tt.want {
			t.Errorf("%s: got %q, %v; want %q", tt.order, out, err, tt.want)
		}
	}
}

type marshalTagged struct {
	Renamed string            `yaml:"new_name"`
	Skipped string            `yaml:"-"`
	JSON    string            `json:"from_json"`
	Plain   int               `yaml:"plain,omitempty"`
	Extra   map[string]string `yaml:",inline"`
	Ptr     *marshalInner     `yaml:"ptr,omitempty"`
	Any     any               `yaml:"any"`
	Flowed  marshalInner      `yaml:"flowed,flow"`
	Seq     []map[string]int  `yaml:"seq,flow"`
	Text    marshalText       `yaml:"text"`
	Own     marshalOwn        `yaml:"own"`
	unexp   string
}

type marshalText struct{ s string }

func (m marshalText) MarshalText() ([]byte, error) { return []byte("text:" + m.s), nil }

type marshalOwn struct{}

func (marshalOwn) MarshalYAML() (any, error) { return map[string]any{"b10": 1, "b9": 2}, nil }

func TestMarshalStructFeatures(t *testing.T) {
	t.Parallel()
	in := marshalTagged{
		Renamed: "r", Skipped: "s", JSON: "j", Extra: map[string]string{"x10": "1", "x9": "2"},
		Any: []any{1, "two"}, Flowed: marshalInner{Host: "h, x", Port: 3},
		Seq:  []map[string]int{{"b": 1, "a": 2}},
		Text: marshalText{"t"}, unexp: "u",
	}
	want := "new_name: r\nfrom_json: j\nx9: \"2\"\nx10: \"1\"\nany:\n- 1\n- two\nflowed: {host: \"h, x\", port: 3}\nseq: [{a: 2, b: 1}]\ntext: text:t\nown:\n  b9: 2\n  b10: 1\n"
	if got := mustMarshal(t, in); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalRefusesNodeMapKeys(t *testing.T) {
	t.Parallel()
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: "SECRET"}
	for name, in := range map[string]any{
		"pointer key": map[*yaml.Node]string{n: "v"},
		"any key":     map[any]any{n: 1},
	} {
		out, err := yaml.Marshal(in)
		if err == nil {
			t.Errorf("%s: Marshal = %q, want an error", name, out)
			continue
		}
		if strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "yaml.Node") {
			t.Errorf("%s: error %q", name, err)
		}
	}
}

func TestMarshalRefusesCollectionKeys(t *testing.T) {
	t.Parallel()
	if out, err := yaml.Marshal(map[[2]int]int{{1, 2}: 1}); err == nil {
		t.Errorf("Marshal = %q, want an error", out)
	}
}
