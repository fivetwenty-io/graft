package yaml_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

type endpoint struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Line int    `yaml:"-"`
}

// UnmarshalYAML decodes through a private type, which has no method, so
// it does not call itself.
func (e *endpoint) UnmarshalYAML(n *yaml.Node) error {
	type plain endpoint
	var p plain
	if err := n.Decode(&p); err != nil {
		return fmt.Errorf("endpoint: %w", err)
	}
	*e = endpoint(p)
	e.Line = n.Line
	return nil
}

type upper string

func (u *upper) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	*u = upper(strings.ToUpper(s) + "|" + n.Tag)
	return nil
}

type counted struct {
	Seen bool `yaml:"-"`
}

func (c *counted) UnmarshalYAML(*yaml.Node) error { c.Seen = true; return nil }

var errSecret = errors.New("secret-text")

type valueHook struct{}

func (valueHook) UnmarshalYAML(*yaml.Node) error { return errSecret }

type textAndNode string

func (t *textAndNode) UnmarshalText([]byte) error     { *t = "text"; return nil }
func (t *textAndNode) UnmarshalYAML(*yaml.Node) error { *t = "node"; return nil }

type bytesHook struct{ Raw string }

func (b *bytesHook) UnmarshalYAML(raw []byte) error {
	b.Raw = strings.TrimSpace(string(raw))
	return nil
}

type site struct {
	Name    string              `yaml:"name"`
	Primary endpoint            `yaml:"primary"`
	Backups []*endpoint         `yaml:"backups"`
	ByZone  map[string]endpoint `yaml:"byZone"`
	Ptr     *endpoint           `yaml:"ptr"`
	Label   upper               `yaml:"label"`
	Pair    [2]upper            `yaml:"pair"`
	Skip    counted             `yaml:"skip"`
	Both    textAndNode         `yaml:"both"`
}

const siteYAML = `name: svc
primary: {host: a, port: 1}
backups:
  - host: b
    port: 2
  - null
byZone:
  z1: {host: c, port: 3}
ptr: {host: d, port: 4}
label: hello
pair: [!!str 5, !custom x]
skip: ~
both: anything
`

func wantSite() site {
	return site{
		Name:    "svc",
		Primary: endpoint{"a", 1, 2},
		Backups: []*endpoint{{"b", 2, 4}, nil},
		ByZone:  map[string]endpoint{"z1": {"c", 3, 8}},
		Ptr:     &endpoint{"d", 4, 9},
		Label:   "HELLO|!!str",
		Pair:    [2]upper{"5|!!str", "X|!custom"},
		Both:    "node",
	}
}

func decodeViaNode(t *testing.T, in string, v any, opts yaml.DecodeOptions) error {
	t.Helper()
	var n yaml.Node
	if err := yaml.UnmarshalWith([]byte(in), &n, opts); err != nil {
		t.Fatalf("reading the node: %v", err)
	}
	return n.Decode(v)
}

func TestNodeDecodeCallsUnmarshalerWhereverItSits(t *testing.T) {
	t.Parallel()
	var got site
	if err := decodeViaNode(t, siteYAML, &got, yaml.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if want := wantSite(); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestNodeDecodeUnmarshalerSeesTheAnchoredNode(t *testing.T) {
	t.Parallel()
	in := "base: &b {host: x, port: 9}\nprimary: *b\nptr: {<<: *b, port: 10}\nbackups: [*b, *b]\n"
	var got struct {
		Primary endpoint   `yaml:"primary"`
		Ptr     *endpoint  `yaml:"ptr"`
		Backups []endpoint `yaml:"backups"`
	}
	if err := decodeViaNode(t, in, &got, yaml.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got.Primary != (endpoint{"x", 9, 1}) || *got.Ptr != (endpoint{"x", 10, 3}) ||
		!reflect.DeepEqual(got.Backups, []endpoint{{"x", 9, 1}, {"x", 9, 1}}) {
		t.Errorf("got %+v, %+v, %+v", got.Primary, *got.Ptr, got.Backups)
	}
}

func TestNodeDecodeCallsUnmarshalerAtTheRoot(t *testing.T) {
	t.Parallel()
	var e endpoint
	if err := decodeViaNode(t, "host: r\nport: 5\n", &e, yaml.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if e != (endpoint{"r", 5, 1}) {
		t.Errorf("got %+v", e)
	}
	var zero yaml.Node
	c := counted{}
	if err := zero.Decode(&c); err != nil || c.Seen {
		t.Errorf("a zero Node: got %v, called %v, want no call", err, c.Seen)
	}
}

type flags struct {
	V any            `yaml:"v"`
	W any            `yaml:"w"`
	X any            `yaml:"x"`
	M map[string]any `yaml:"m"`
	B any            `yaml:"b"`
}

type flagsHook struct{ F flags }

func (h *flagsHook) UnmarshalYAML(n *yaml.Node) error { return n.Decode(&h.F) }

const flagsYAML = "h:\n  v: \"yes\"\n  w: 'on'\n  x: |\n    no\n  m: {q: \"off\"}\n  b: yes\n"

func wantFlags(yaml12 bool) flags {
	f := flags{V: "yes", W: "on", X: "no\n", M: map[string]any{"q": "off"}, B: true}
	if yaml12 {
		f.B = "yes"
	}
	return f
}

func TestNodeDecodeUnmarshalerIsMarkerFree(t *testing.T) {
	t.Parallel()
	for _, yaml12 := range []bool{false, true} {
		var got struct {
			H flagsHook `yaml:"h"`
		}
		if err := decodeViaNode(t, flagsYAML, &got, yaml.DecodeOptions{YAML12: yaml12}); err != nil {
			t.Fatal(err)
		}
		if want := wantFlags(yaml12); !reflect.DeepEqual(got.H.F, want) {
			t.Errorf("YAML12 %v: got %#v, want %#v", yaml12, got.H.F, want)
		}
		if strings.ContainsRune(fmt.Sprintf("%#v", got), '\uE0DA') {
			t.Errorf("YAML12 %v: the marker leaked", yaml12)
		}
	}
}

func TestNodeDecodeUnmarshalerErrorKeepsTheInnerPosition(t *testing.T) {
	t.Parallel()
	in := "name: svc\nprimary: {host: a, port: x}\n"
	var withHook site
	err := decodeViaNode(t, in, &withHook, yaml.DecodeOptions{})
	var plain struct {
		Primary struct {
			Port int `yaml:"port"`
		} `yaml:"primary"`
	}
	base := decodeViaNode(t, in, &plain, yaml.DecodeOptions{})
	var got, want *yaml.DecodeError
	if !errors.As(err, &got) || !errors.As(base, &want) {
		t.Fatalf("got %v and %v, want two DecodeErrors", err, base)
	}
	if got.Line != 2 || got.Column != want.Column || got.Kind != want.Kind || !strings.Contains(got.Msg, "Port of type int") {
		t.Errorf("got %+v, want the place and kind of %+v", got, want)
	}
	if strings.Contains(err.Error(), "endpoint:") {
		t.Errorf("Error() = %q, which shows the method's own text", err.Error())
	}
	if inner := errors.Unwrap(err); inner == nil || !strings.HasPrefix(inner.Error(), "endpoint: ") {
		t.Errorf("Unwrap gave %v, want the method's error", inner)
	}
}

func TestNodeDecodeUnmarshalerPlainErrorBecomesATypeError(t *testing.T) {
	t.Parallel()
	var got struct {
		V valueHook `yaml:"v"`
	}
	err := decodeViaNode(t, "a: 1\nv:\n  k: hunter2\n", &got, yaml.DecodeOptions{})
	var de *yaml.DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("got %v", err)
	}
	want := yaml.DecodeError{Line: 3, Column: 3, Kind: yaml.ErrorKindType, Msg: "cannot unmarshal into Go value of type yaml_test.valueHook"}
	if de.Line != want.Line || de.Column != want.Column || de.Kind != want.Kind || de.Msg != want.Msg {
		t.Errorf("got %+v, want %+v", de, want)
	}
	if !errors.Is(err, errSecret) {
		t.Error("errors.Is does not reach the method's error")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("Error() = %q, which quotes the method's error or the input", err.Error())
	}
}

func TestDecodeRefusesUnmarshalerWhereItCannotRun(t *testing.T) {
	t.Parallel()
	var keyed map[upper]int
	err := decodeViaNode(t, "a: 1\n", &keyed, yaml.DecodeOptions{})
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType ||
		!strings.HasPrefix(de.Msg, "Decode cannot call the UnmarshalYAML method of yaml_test.upper, which ") ||
		!strings.HasSuffix(de.Msg, " as a map key") {
		t.Errorf("a map key: got %v", err)
	}
	var inline struct {
		E endpoint `yaml:",inline"`
	}
	err = decodeViaNode(t, "host: a\n", &inline, yaml.DecodeOptions{})
	if !errors.As(err, &de) || !strings.HasSuffix(de.Msg, " as an inline field") {
		t.Errorf("an inline field: got %v", err)
	}
}

func TestGoccyStyleHooksStillRun(t *testing.T) {
	t.Parallel()
	var viaText, viaNode struct {
		B bytesHook `yaml:"b"`
	}
	if err := yaml.Unmarshal([]byte("b: hello\n"), &viaText); err != nil || viaText.B.Raw != "hello" {
		t.Errorf("Unmarshal: got %q, %v", viaText.B.Raw, err)
	}
	if err := decodeViaNode(t, "b: hello\n", &viaNode, yaml.DecodeOptions{}); err != nil || viaNode.B.Raw != "hello" {
		t.Errorf("Node.Decode: got %q, %v", viaNode.B.Raw, err)
	}
}
