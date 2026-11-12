package yaml_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// The yaml.v2 and yaml.v3 values these tests cite were recorded from
// gopkg.in/yaml.v2 v2.4.0 and gopkg.in/yaml.v3 v3.0.1 in a throwaway
// module, since graft never imports either library.

// commandSwitch, commandCredential, and commandsConfig have the shape of
// the broker's CommandsConfig, whose ,inline struct holds a *bool.
type commandSwitch struct {
	On *bool `yaml:"enabled"`
}

type commandCredential struct {
	Key string `yaml:"key"`
}

type commandsConfig struct {
	Operation  commandSwitch     `yaml:",inline"`
	Root       *bool             `yaml:"allow_root"`
	Credential commandCredential `yaml:"credential"`
}

func boolPtr(b bool) *bool { return &b }

func TestUnmarshalStructInlinePointerBool(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want commandsConfig
	}{
		// yaml.v2 and yaml.v3 both give On true and Root false.
		{"words", "enabled: yes\nallow_root: no\ncredential:\n  key: k\n", commandsConfig{
			Operation: commandSwitch{On: boolPtr(true)}, Root: boolPtr(false), Credential: commandCredential{Key: "k"},
		}},
		{"core booleans", "enabled: false\nallow_root: true\n", commandsConfig{
			Operation: commandSwitch{On: boolPtr(false)}, Root: boolPtr(true),
		}},
		// yaml.v2 and yaml.v3 both leave the pointers nil.
		{"unset", "credential:\n  key: k\n", commandsConfig{Credential: commandCredential{Key: "k"}}},
		{"null", "enabled: ~\nallow_root:\n", commandsConfig{}},
		{"comment and alias", "base: &t yes\nenabled: yes # on\nallow_root: *t\n", commandsConfig{
			Operation: commandSwitch{On: boolPtr(true)}, Root: boolPtr(true),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got commandsConfig
			if err := yaml.Unmarshal([]byte(tc.doc), &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %s, want %s", showCommands(got), showCommands(tc.want))
			}
		})
	}
}

func showCommands(c commandsConfig) string {
	p := func(b *bool) string {
		if b == nil {
			return "nil"
		}
		return fmt.Sprint(*b)
	}
	return fmt.Sprintf("{On:%s Root:%s Key:%q}", p(c.Operation.On), p(c.Root), c.Credential.Key)
}

// tagged covers the tag forms goccy reads. ServiceName has only a json
// tag, which goccy uses, while yaml.v2 and yaml.v3 ignore json tags and
// read the key servicename. Plain has no tag, so its key is its name in
// lower case, as in yaml.v2 and yaml.v3.
type tagged struct {
	Name        string   `yaml:"name"`
	Port        int      `yaml:"port,omitempty"`
	Tags        []string `yaml:"tags,flow"`
	ServiceName string   `json:"service_name"`
	Plain       string
	Skipped     string `json:"-"`
}

func TestUnmarshalStructTags(t *testing.T) {
	const doc = "name: n\nport: 8080\ntags: [a, b]\nservice_name: s\nservicename: v2-key\nplain: p\nskipped: x\n"
	var got tagged
	if err := yaml.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := tagged{Name: "n", Port: 8080, Tags: []string{"a", "b"}, ServiceName: "s", Plain: "p"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// An omitempty field the document leaves out keeps its value.
	keep := tagged{Port: 9}
	if err := yaml.Unmarshal([]byte("name: n\n"), &keep); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if keep.Port != 9 || keep.Name != "n" {
		t.Errorf("got %+v, want Port 9 and Name n", keep)
	}
}

// boolFields holds the bool shapes the bool hook serves. X sits on the
// line before them, so an error's line is not the first.
type boolFields struct {
	X  int             `yaml:"x"`
	B  bool            `yaml:"b"`
	PB *bool           `yaml:"pb"`
	M  map[string]bool `yaml:"m"`
	L  []bool          `yaml:"l"`
}

// TestUnmarshalBoolMatrix decodes each word plain, double-quoted, and
// single-quoted into bool, *bool, map[string]bool, and []bool, in both
// modes. In YAML 1.1 mode a plain yes, no, on, or off is a boolean, as
// in yaml.v2 and yaml.v3, and every quoted form is refused, as yaml.v2
// refuses it. yaml.v3 accepts the quoted 1.1 words and the quoted y and
// n, and refuses the quoted true and false. A plain y or n is refused in
// both modes, while yaml.v2 and yaml.v3 read it as a boolean, since
// graft's merge reads it as a string. In YAML 1.2 mode only true and
// false in their three cases are booleans.
func TestUnmarshalBoolMatrix(t *testing.T) {
	cases := []struct {
		word   string
		yaml11 any // the bool, or nil when refused
		yaml12 any
	}{
		{"yes", true, nil}, {"Yes", true, nil}, {"YES", true, nil},
		{"no", false, nil}, {"No", false, nil}, {"NO", false, nil},
		{"on", true, nil}, {"On", true, nil}, {"ON", true, nil},
		{"off", false, nil}, {"Off", false, nil}, {"OFF", false, nil},
		{"true", true, true}, {"True", true, true}, {"TRUE", true, true},
		{"false", false, false}, {"False", false, false}, {"FALSE", false, false},
		{"y", nil, nil}, {"n", nil, nil},
	}
	forms := []struct {
		name  string
		quote func(string) string
	}{
		{"plain", func(s string) string { return s }},
		{"double", func(s string) string { return `"` + s + `"` }},
		{"single", func(s string) string { return "'" + s + "'" }},
	}
	for _, tc := range cases {
		for _, form := range forms {
			for _, m := range modes {
				want := tc.yaml12
				if m.yaml11 {
					want = tc.yaml11
				}
				if form.name != "plain" {
					want = nil
				}
				scalar := form.quote(tc.word)
				t.Run(m.name+"/"+form.name+"/"+tc.word, func(t *testing.T) {
					checkBool(t, m.opts, "x: 1\nb: "+scalar+"\n", 2, want, func(g *boolFields) bool { return g.B })
					checkBool(t, m.opts, "x: 1\npb: "+scalar+"\n", 2, want, func(g *boolFields) bool { return *g.PB })
					checkBool(t, m.opts, "x: 1\nm:\n  ok: true\n  k: "+scalar+"\n", 4, want, func(g *boolFields) bool { return g.M["k"] })
					checkBool(t, m.opts, "x: 1\nl: [true,\n  "+scalar+"]\n", 3, want, func(g *boolFields) bool { return g.L[1] })
				})
			}
		}
	}
}

// checkBool decodes doc into boolFields and checks the bool read finds,
// or, when want is nil, a type error on line.
func checkBool(t *testing.T, opts yaml.DecodeOptions, doc string, line int, want any, read func(*boolFields) bool) {
	t.Helper()
	var got boolFields
	err := yaml.UnmarshalWith([]byte(doc), &got, opts)
	if want == nil {
		de := wantDecodeError(t, err, yaml.ErrorKindType, line)
		if !strings.Contains(de.Msg, "bool") {
			t.Errorf("Msg %q does not name the bool type", de.Msg)
		}
		return
	}
	if err != nil {
		t.Fatalf("UnmarshalWith(%q): %v", doc, err)
	}
	if b := read(&got); b != want {
		t.Errorf("UnmarshalWith(%q) = %v, want %v", doc, b, want)
	}
}

// TestUnmarshalStringFieldsKeepWords checks that a string field holds a
// YAML 1.1 word as written, quoted or plain, in both modes, as yaml.v2
// and yaml.v3 both do.
func TestUnmarshalStringFieldsKeepWords(t *testing.T) {
	type strs struct {
		Plain  string `yaml:"plain"`
		Double string `yaml:"double"`
		Single string `yaml:"single"`
		Upper  string `yaml:"upper"`
	}
	const doc = "plain: yes\ndouble: \"yes\"\nsingle: 'on'\nupper: OFF\n"
	want := strs{Plain: "yes", Double: "yes", Single: "on", Upper: "OFF"}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			var got strs
			if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// job and manifest hold generic values inside structs.
type job struct {
	Name       string         `yaml:"name"`
	Properties map[string]any `yaml:"properties"`
	Any        any            `yaml:"any"`
	List       []any          `yaml:"list"`
}

type manifest struct {
	Defaults map[string]any `yaml:"defaults"`
	Jobs     []job          `yaml:"jobs"`
}

// The documents below use an anchor defined at the top level from deeper
// places, in flow style, in block style, and through a << merge key.
const anchorHead = "defaults: &d\n  a: 1\n  b: yes\n  s: \"on\"\n  n: 18446744073709551615\njobs:\n- name: x\n"

var nestedDocs = []struct {
	name string
	doc  string
}{
	{"flow", anchorHead + "  properties: {inner: *d, c: 2}\n  any: {inner: *d}\n  list: [*d, yes, \"no\", 3]\n"},
	{"block", anchorHead + "  properties:\n    inner: *d\n    c: 2\n  any:\n    deeper:\n      inner: *d\n  list:\n  - *d\n  - yes\n  - \"no\"\n  - 3\n"},
	{"merge", anchorHead + "  properties:\n    <<: *d\n    c: 2\n  any:\n    <<: *d\n  list:\n  - <<: *d\n    c: 2\n"},
	{"alias root", anchorHead + "  properties: *d\n  any: *d\n  list:\n  - *d\n"},
}

// TestUnmarshalNestedGenericMatchesTopLevel checks that a map[string]any,
// any, or []any field holds exactly what a top-level decode of the same
// text gives, with int integers, YAML 1.1 booleans in 1.1 mode, and
// quoted words kept as strings.
func TestUnmarshalNestedGenericMatchesTopLevel(t *testing.T) {
	for _, d := range nestedDocs {
		for _, m := range modes {
			t.Run(m.name+"/"+d.name, func(t *testing.T) {
				var top map[string]any
				if err := yaml.UnmarshalWith([]byte(d.doc), &top, m.opts); err != nil {
					t.Fatalf("UnmarshalWith(top): %v", err)
				}
				topJob := top["jobs"].([]any)[0].(map[string]any)

				var got manifest
				if err := yaml.UnmarshalWith([]byte(d.doc), &got, m.opts); err != nil {
					t.Fatalf("UnmarshalWith(struct): %v", err)
				}
				wantDeepEqual(t, got.Defaults, top["defaults"])
				wantDeepEqual(t, got.Jobs[0].Properties, topJob["properties"])
				wantDeepEqual(t, got.Jobs[0].Any, topJob["any"])
				wantDeepEqual(t, any(got.Jobs[0].List), topJob["list"])
			})
		}
	}
}

// TestUnmarshalNestedGenericValues pins the values themselves, so the
// test above cannot pass by both routes going wrong together.
func TestUnmarshalNestedGenericValues(t *testing.T) {
	for _, m := range modes {
		b := any("yes")
		if m.yaml11 {
			b = true
		}
		d := map[string]any{"a": 1, "b": b, "s": "on", "n": uint64(18446744073709551615)}
		t.Run(m.name+"/block", func(t *testing.T) {
			var got manifest
			if err := yaml.UnmarshalWith([]byte(nestedDocs[1].doc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			wantDeepEqual(t, got.Jobs[0].Properties, map[string]any{"inner": d, "c": 2})
			wantDeepEqual(t, got.Jobs[0].Any, map[string]any{"deeper": map[string]any{"inner": d}})
			wantDeepEqual(t, any(got.Jobs[0].List), []any{d, b, "no", 3})
		})
		t.Run(m.name+"/merge", func(t *testing.T) {
			var got manifest
			if err := yaml.UnmarshalWith([]byte(nestedDocs[2].doc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			withC := map[string]any{"a": 1, "b": b, "s": "on", "n": uint64(18446744073709551615), "c": 2}
			wantDeepEqual(t, got.Jobs[0].Properties, withC)
			wantDeepEqual(t, got.Jobs[0].Any, d)
			wantDeepEqual(t, any(got.Jobs[0].List), []any{withC})
		})
	}
}

// TestUnmarshalMergeKeyIntoStruct checks that a << merge key fills a
// struct's fields, with the struct's own key winning, as in yaml.v2 and
// yaml.v3, which both give {A:1 B:true S:on Name:x}.
func TestUnmarshalMergeKeyIntoStruct(t *testing.T) {
	type item struct {
		A    int    `yaml:"a"`
		B    bool   `yaml:"b"`
		S    string `yaml:"s"`
		Name string `yaml:"name"`
	}
	const doc = "base: &d\n  a: 1\n  b: yes\n  s: \"on\"\n  name: base\nitem:\n  <<: *d\n  name: x\n"
	var got struct {
		Item item `yaml:"item"`
	}
	if err := yaml.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if want := (item{A: 1, B: true, S: "on", Name: "x"}); got.Item != want {
		t.Errorf("got %+v, want %+v", got.Item, want)
	}
}

// leakName is a named string type.
type leakName string

type leakInner struct {
	S string `yaml:"s"`
}

// LeakEmbedded is embedded in leaky without a tag.
type LeakEmbedded struct {
	E string `yaml:"e"`
}

// leaky holds a string at every place the typed route fills. Any makes
// the target hold a generic value, so the quoted words are tagged.
type leaky struct {
	LeakEmbedded `yaml:",inline"`
	S            string                       `yaml:"s"`
	P            *string                      `yaml:"p"`
	Named        leakName                     `yaml:"named"`
	Arr          [2]string                    `yaml:"arr"`
	Slice        []string                     `yaml:"slice"`
	Map          map[string]string            `yaml:"map"`
	MapPtr       map[string]*string           `yaml:"mapptr"`
	Inner        leakInner                    `yaml:"inner"`
	InnerPtr     *leakInner                   `yaml:"innerptr"`
	Inners       []leakInner                  `yaml:"inners"`
	InnerMap     map[string]leakInner         `yaml:"innermap"`
	Any          any                          `yaml:"any"`
	AnyMap       map[string]any               `yaml:"anymap"`
	AnyList      []any                        `yaml:"anylist"`
	Nested       map[string][]any             `yaml:"nested"`
	PtrAny       *any                         `yaml:"ptrany"`
	Deep         map[string]map[string]string `yaml:"deep"`
}

// leakyTyped is leaky without generic values, so nothing is tagged.
type leakyTyped struct {
	S     string            `yaml:"s"`
	Slice []string          `yaml:"slice"`
	Map   map[string]string `yaml:"map"`
	Inner leakInner         `yaml:"inner"`
}

const leakDoc = `e: "yes"
s: "yes"
p: 'On'
named: "OFF"
arr: ["yes", 'off']
slice: ["yes", plain]
map: {k: "On"}
mapptr: {k: "no"}
inner: {s: "yes"}
innerptr: {s: 'on'}
inners: [{s: "Yes"}]
innermap: {k: {s: "NO"}}
any: "yes"
anymap: {k: "on", l: ["off"]}
anylist: ["Off", {k: "yes"}]
nested: {k: ["ON"]}
ptrany: "no"
deep: {a: {b: "yes"}}
`

// TestUnmarshalMarkerNeverLeaks checks that the marker graft puts on a
// quoted YAML 1.1 word while it decodes never reaches a decoded string,
// wherever the string sits.
func TestUnmarshalMarkerNeverLeaks(t *testing.T) {
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			var got leaky
			if err := yaml.UnmarshalWith([]byte(leakDoc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			wantNoMarker(t, reflect.ValueOf(got), "leaky")
			on := "On"
			no := "no"
			want := leaky{
				LeakEmbedded: LeakEmbedded{E: "yes"},
				S:            "yes", P: &on, Named: "OFF",
				Arr: [2]string{"yes", "off"}, Slice: []string{"yes", "plain"},
				Map: map[string]string{"k": "On"}, MapPtr: map[string]*string{"k": &no},
				Inner: leakInner{S: "yes"}, InnerPtr: &leakInner{S: "on"},
				Inners: []leakInner{{S: "Yes"}}, InnerMap: map[string]leakInner{"k": {S: "NO"}},
				Any: "yes", AnyMap: map[string]any{"k": "on", "l": []any{"off"}},
				AnyList: []any{"Off", map[string]any{"k": "yes"}},
				Nested:  map[string][]any{"k": {"ON"}},
				Deep:    map[string]map[string]string{"a": {"b": "yes"}},
			}
			ptrAny := any("no")
			want.PtrAny = &ptrAny
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}

			var typed leakyTyped
			if err := yaml.UnmarshalWith([]byte(leakDoc), &typed, m.opts); err != nil {
				t.Fatalf("UnmarshalWith(typed): %v", err)
			}
			wantNoMarker(t, reflect.ValueOf(typed), "leakyTyped")
		})
	}
}

// wantNoMarker fails for any string in v, at any depth, that
// holds the private-use code point graft's marker uses.
func wantNoMarker(t *testing.T, v reflect.Value, path string) {
	t.Helper()
	const marker = ""
	switch v.Kind() {
	case reflect.String:
		if strings.Contains(v.String(), marker) {
			t.Errorf("%s = %q holds the marker", path, v.String())
		}
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			wantNoMarker(t, v.Elem(), path)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				wantNoMarker(t, v.Field(i), path+"."+v.Type().Field(i).Name)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			wantNoMarker(t, v.Index(i), fmt.Sprintf("%s[%d]", path, i))
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			wantNoMarker(t, iter.Value(), fmt.Sprintf("%s[%v]", path, iter.Key()))
		}
	default:
	}
}

// TestUnmarshalTypedMapsAndSlices covers typed maps and slices at the
// root and inside a struct. The values match yaml.v2 and yaml.v3 except
// where a comment says otherwise.
func TestUnmarshalTypedMapsAndSlices(t *testing.T) {
	t.Run("map of strings", func(t *testing.T) {
		var got map[string]string
		if err := yaml.Unmarshal([]byte("a: yes\nb: \"yes\"\nc: 3\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		wantDeepEqual(t, any(got), any(map[string]string{"a": "yes", "b": "yes", "c": "3"}))
	})
	t.Run("map of ints", func(t *testing.T) {
		var got map[string]int
		if err := yaml.Unmarshal([]byte("a: 1\nb: -2\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		wantDeepEqual(t, any(got), any(map[string]int{"a": 1, "b": -2}))
	})
	t.Run("map of bools", func(t *testing.T) {
		var got map[string]bool
		if err := yaml.Unmarshal([]byte("a: yes\nb: off\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		wantDeepEqual(t, any(got), any(map[string]bool{"a": true, "b": false}))
	})
	t.Run("slice of strings", func(t *testing.T) {
		var got []string
		if err := yaml.Unmarshal([]byte("- yes\n- \"on\"\n- 3\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		wantDeepEqual(t, any(got), any([]string{"yes", "on", "3"}))
	})
	t.Run("slice of ints", func(t *testing.T) {
		var got []int
		if err := yaml.Unmarshal([]byte("- 1\n- 0x10\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		wantDeepEqual(t, any(got), any([]int{1, 16}))
	})
	t.Run("map of slices in a struct", func(t *testing.T) {
		var got struct {
			Groups map[string][]string `yaml:"groups"`
			Ports  []int               `yaml:"ports"`
		}
		if err := yaml.Unmarshal([]byte("groups:\n  a: [x, y]\n  b: []\nports: [80, 443]\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		wantDeepEqual(t, any(got.Groups), any(map[string][]string{"a": {"x", "y"}, "b": {}}))
		wantDeepEqual(t, any(got.Ports), any([]int{80, 443}))
	})
	t.Run("named generic map", func(t *testing.T) {
		type params map[string]any
		var got params
		if err := yaml.Unmarshal([]byte("a: 1\nb: yes\nc: \"on\"\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		wantDeepEqual(t, map[string]any(got), map[string]any{"a": 1, "b": true, "c": "on"})
	})
}

// TestUnmarshalTypeMismatch checks that a value that does not fit its
// field is an ErrorKindType error on the value's line that quotes nothing
// from the input. yaml.v2 and yaml.v3 both report line 2 for the first
// case.
func TestUnmarshalTypeMismatch(t *testing.T) {
	type target struct {
		Name  string              `yaml:"name"`
		Port  int                 `yaml:"port"`
		Tags  []string            `yaml:"tags"`
		Flags map[string]bool     `yaml:"flags"`
		Jobs  []struct{ On bool } `yaml:"jobs"`
		Any   []any               `yaml:"any"`
	}
	cases := []struct {
		name string
		doc  string
		line int
	}{
		{"string into int", "name: x\nport: supersecret\n", 2},
		{"mapping into slice", "name: x\n\ntags:\n  supersecret: 1\n", 4},
		{"quoted word into bool in a map", "flags:\n  a: true\n  b: \"on\"\n", 3},
		{"quoted word into bool in a list", "jobs:\n- on: true\n- on: 'off'\n", 3},
		{"number into bool", "jobs:\n- on: 1\n", 2},
		{"mapping into generic slice", "name: x\nany: {supersecret: 1}\n", 2},
		{"relocated bool refusal", "base: &b\n  on: \"yes\"\njobs:\n- *b\n", 2},
		{"bool refusal beside a merge key", "base: &b\n  on: true\n  x: 1\njobs:\n- <<: *b\n  on: \"no\"\n", 6},
	}
	for _, tc := range cases {
		for _, m := range modes {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				var got target
				err := yaml.UnmarshalWith([]byte(tc.doc), &got, m.opts)
				wantDecodeError(t, err, yaml.ErrorKindType, tc.line)
				wantNoLeak(t, err, "supersecret", "")
			})
		}
	}
}

// flag is a named bool type, which goccy decodes with its own YAML 1.2
// rule rather than through the bool hook, as Unmarshal's doc comment
// says. yaml.v2 and yaml.v3 both read a plain yes into it as true.
type flag bool

func TestUnmarshalNamedBoolFollowsYAML12(t *testing.T) {
	var got struct {
		F flag `yaml:"f"`
	}
	if err := yaml.Unmarshal([]byte("f: true\n"), &got); err != nil || !bool(got.F) {
		t.Fatalf("Unmarshal(true) = %v, %v", got.F, err)
	}
	err := yaml.Unmarshal([]byte("f: yes\n"), &got)
	wantDecodeError(t, err, yaml.ErrorKindType, 1)
}

// anyMap is a named generic map type, which takes the typed route.
type anyMap map[string]any

// TestUnmarshalTypedDecodeErrors runs the generic route's error cases
// through the typed route, which reports the same kind and line.
func TestUnmarshalTypedDecodeErrors(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		kind yaml.ErrorKind
		line int
	}{
		{"unclosed flow sequence", "a: 1\nb: [supersecret\nc: 3\n", yaml.ErrorKindSyntax, 3},
		{"duplicate key", "a: 1\npassword: supersecret\npassword: other\n", yaml.ErrorKindSyntax, 3},
		{"self-containing anchor", "a: &x\n  b: *x\n  pw: supersecret\n", yaml.ErrorKindSyntax, 2},
		{"billion laughs", billionLaughs, yaml.ErrorKindLimit, 0},
		{"nesting depth", "pw: supersecret\nx: " + strings.Repeat("[", 10001) + "\n", yaml.ErrorKindLimit, 2},
		{"content after end marker", "pw: supersecret\n...\nb: 2\n", yaml.ErrorKindSyntax, 3},
	}
	for _, tc := range cases {
		for _, m := range modes {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				var got anyMap
				err := yaml.UnmarshalWith([]byte(tc.doc), &got, m.opts)
				wantDecodeError(t, err, tc.kind, tc.line)
				wantNoLeak(t, err, "supersecret", "lol")
			})
		}
	}
}

// TestUnmarshalTypedEmptyLeavesTargetUnchanged checks that an empty, a
// comment-only, or a null document leaves a struct as it was. yaml.v3
// does the same, while yaml.v2 zeroes the struct for a null document.
func TestUnmarshalTypedEmptyLeavesTargetUnchanged(t *testing.T) {
	type pre struct {
		A string `yaml:"a"`
		B int    `yaml:"b"`
	}
	for _, doc := range []string{"", "# only a comment\n", "~\n", "---\n"} {
		t.Run(fmt.Sprintf("%q", doc), func(t *testing.T) {
			got := pre{A: "keep", B: 7}
			if err := yaml.Unmarshal([]byte(doc), &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got != (pre{A: "keep", B: 7}) {
				t.Errorf("got %+v, want it unchanged", got)
			}
		})
	}
}

// hiddenWord keeps what its UnmarshalYAML method decodes in an
// unexported field, which Unmarshal cannot reach once goccy is done.
type hiddenWord struct {
	word string
}

func (h *hiddenWord) UnmarshalYAML(unmarshal func(any) error) error {
	return unmarshal(&h.word)
}

// TestUnmarshalOwnUnmarshalerWithoutGenericValues checks that a quoted
// YAML 1.1 word reaches a type's own UnmarshalYAML method as written
// when the target holds no value of interface type, since graft tags
// quoted words only for the generic values it converts.
func TestUnmarshalOwnUnmarshalerWithoutGenericValues(t *testing.T) {
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			var got struct {
				W hiddenWord `yaml:"w"`
			}
			if err := yaml.UnmarshalWith([]byte("w: \"yes\"\n"), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			if got.W.word != "yes" {
				t.Errorf("word = %q, want %q", got.W.word, "yes")
			}
		})
	}
}

// TestUnmarshalGenericValuesUnderNaNKeys checks that a map with float
// keys and values of type any keeps one entry for each NaN key, as goccy
// makes it, once those values are converted.
func TestUnmarshalGenericValuesUnderNaNKeys(t *testing.T) {
	type nanValues struct {
		M map[float64]any `yaml:"m"`
	}
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"m: {.nan: x}\n", []string{"x"}},
		{"m: {.nan: x, .NaN: y, 1: z}\n", []string{"x", "y", "z"}},
		{"m: {.nan: {a: yes}}\n", []string{"map[a:true]"}},
	} {
		var got nanValues
		if err := yaml.Unmarshal([]byte(tc.in), &got); err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		values := make([]string, 0, len(got.M))
		for _, v := range got.M {
			values = append(values, fmt.Sprint(v))
		}
		slices.Sort(values)
		if !slices.Equal(values, tc.want) {
			t.Errorf("%q: got values %q, want %q", tc.in, values, tc.want)
		}
	}
}
