package yaml_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func TestUnmarshalCallsUnmarshaler(t *testing.T) {
	t.Parallel()
	var got site
	if err := yaml.Unmarshal([]byte(siteYAML), &got); err != nil {
		t.Fatal(err)
	}
	if want := wantSite(); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	var e endpoint
	if err := yaml.Unmarshal([]byte("host: r\nport: 5\n"), &e); err != nil || e != (endpoint{"r", 5, 1}) {
		t.Errorf("at the root: got %+v, %v", e, err)
	}
	var c counted
	var held struct {
		C counted `yaml:"c"`
	}
	if err := yaml.Unmarshal(nil, &c); err != nil || c.Seen {
		t.Errorf("empty input at the root: called %v, %v", c.Seen, err)
	}
	if err := yaml.Unmarshal([]byte("c: null\n"), &held); err != nil || held.C.Seen {
		t.Errorf("null field: called %v, %v", held.C.Seen, err)
	}
}

func TestUnmarshalWithUnmarshalerIsMarkerFree(t *testing.T) {
	t.Parallel()
	for _, yaml12 := range []bool{false, true} {
		var got struct {
			H flagsHook `yaml:"h"`
		}
		if err := yaml.UnmarshalWith([]byte(flagsYAML), &got, yaml.DecodeOptions{YAML12: yaml12}); err != nil {
			t.Fatal(err)
		}
		if want := wantFlags(yaml12); !reflect.DeepEqual(got.H.F, want) {
			t.Errorf("YAML12 %v: got %#v, want %#v", yaml12, got.H.F, want)
		}
		if strings.ContainsRune(fmt.Sprintf("%#v", got), '') {
			t.Errorf("YAML12 %v: the marker leaked", yaml12)
		}
	}
}

// plainFields are the fields that read the same with and without a hook.
type plainFields struct {
	Name  string
	Count int
}

// Each helper declares its own type named doc, so goccy's messages, which
// name the struct, read the same for both.
func decodeWithHook(in string) (plainFields, error) {
	type doc struct {
		Name  string `yaml:"name"`
		Count int    `yaml:"count"`
		Label upper  `yaml:"label"`
	}
	var d doc
	err := yaml.Unmarshal([]byte(in), &d)
	return plainFields{d.Name, d.Count}, err
}

func decodeWithoutHook(in string) (plainFields, error) {
	type doc struct {
		Name  string `yaml:"name"`
		Count int    `yaml:"count"`
		Label string `yaml:"label"`
	}
	var d doc
	err := yaml.Unmarshal([]byte(in), &d)
	return plainFields{d.Name, d.Count}, err
}

func TestUnmarshalWithUnmarshalerKeepsOtherErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in    string
		fails bool
	}{
		{"name: [\n", true},
		{"name: a\nname: b\n", true},
		{"name: [x]\nlabel: y\n", true},
		{"label: y\nname: {a: 1}\n", true},
		{"name: &a x\nlabel: *missing\n", true},
		{"name: a\n...\nlabel: b\n", true},
		{"name: !!int x\n", true},
		{"count: 99999999999999999999\nlabel: y\n", true},
		{"<<: {count: [1]}\nlabel: y\n", true},
		{"base: &b {name: a, count: 1, label: x}\n<<: *b\ncount: 2\n", false},
		{"", false},
		{"name: \"yes\"\ncount: 0x1F\nlabel: 'on'\n", false},
	} {
		gotValue, gotErr := decodeWithHook(tc.in)
		wantValue, wantErr := decodeWithoutHook(tc.in)
		if (gotErr != nil) != tc.fails || (wantErr != nil) != tc.fails {
			t.Errorf("%q: got errors %v and %v, want failure %v from both", tc.in, gotErr, wantErr, tc.fails)
			continue
		}
		if !tc.fails {
			if gotValue != wantValue {
				t.Errorf("%q: got %+v, want %+v", tc.in, gotValue, wantValue)
			}
			continue
		}
		var got, want *yaml.DecodeError
		if !errors.As(gotErr, &got) || !errors.As(wantErr, &want) {
			t.Errorf("%q: want a DecodeError from both", tc.in)
			continue
		}
		if got.Line != want.Line || got.Column != want.Column || got.Kind != want.Kind || got.Msg != want.Msg {
			t.Errorf("%q: got %+v, want %+v", tc.in, got, want)
		}
	}
}

func TestUnmarshalWithUnmarshalerReportsEachDuplicateOnce(t *testing.T) {
	t.Parallel()
	in := "primary: {host: a, port: 1}\nprimary: {host: b, port: 2}\n"
	var seen []yaml.DuplicateKey
	var got struct {
		Primary endpoint `yaml:"primary"`
	}
	opts := yaml.DecodeOptions{OnDuplicateKey: func(d yaml.DuplicateKey) { seen = append(seen, d) }}
	if err := yaml.UnmarshalWith([]byte(in), &got, opts); err != nil {
		t.Fatal(err)
	}
	if got.Primary != (endpoint{"b", 2, 2}) {
		t.Errorf("got %+v, want the last value", got.Primary)
	}
	if len(seen) != 1 || seen[0].Line != 2 || seen[0].FirstLine != 1 {
		t.Errorf("got %+v, want one report at line 2 of a key first seen at line 1", seen)
	}
}
