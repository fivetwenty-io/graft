package yaml_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

type dupHook struct{ Val string }

func (h *dupHook) UnmarshalYAML(n *yaml.Node) error {
	h.Val = "hook:" + n.Value
	return nil
}

// dupScalarHook is a hook type that goccy can also fill natively from a
// scalar, so a field of it that goccy shadows still decodes.
type dupScalarHook string

func (h *dupScalarHook) UnmarshalYAML(n *yaml.Node) error {
	*h = dupScalarHook("hook:" + n.Value)
	return nil
}

// DupInner is inlined beside a hooked field that reads the same key.
type DupInner struct {
	X string `yaml:"x"`
	Y int    `yaml:"y"`
}

// dupOuter's hooked field shadows the field DupInner has for x.
type dupOuter struct {
	X        dupHook `yaml:"x"`
	DupInner `yaml:",inline"`
}

// DupPlainInner is inlined beside a plain field that reads the same key.
type DupPlainInner struct {
	X string `yaml:"x"`
}

type dupPlain struct {
	X             string `yaml:"x"`
	DupPlainInner `yaml:",inline"`
}

// DupHookInner puts the hook on the inlined side of the clash.
type DupHookInner struct {
	X dupScalarHook `yaml:"x"`
}

// dupOuterPlain's plain field shadows the hooked field of DupHookInner.
type dupOuterPlain struct {
	X            string `yaml:"x"`
	DupHookInner `yaml:",inline"`
}

// dupOuterPlainPointer is dupOuterPlain through a pointer embed.
type dupOuterPlainPointer struct {
	X             string `yaml:"x"`
	*DupHookInner `yaml:",inline"`
}

// dupPointerInner reaches the shadowed struct through a pointer.
type dupPointerInner struct {
	X         dupHook `yaml:"x"`
	*DupInner `yaml:",inline"`
}

// dupSiblings inlines two structs that both read x, so goccy fills both
// natively and the hook never runs.
type dupSiblings struct {
	DupInner     `yaml:",inline"`
	DupHookInner `yaml:",inline"`
}

// DupMid inlines the hooked struct one level further down.
type DupMid struct {
	DupHookInner `yaml:",inline"`
}

// dupTwoDeep's plain field shadows nothing two ,inline levels down, so
// goccy fills both fields natively.
type dupTwoDeep struct {
	X      string `yaml:"x"`
	DupMid `yaml:",inline"`
}

// dupMixed has a hook on one key and a plain duplicate on another.
type dupMixed struct {
	H             dupHook `yaml:"h"`
	X             string  `yaml:"x"`
	DupPlainInner `yaml:",inline"`
}

// dupMixedSiblings has a hook on one key and two plain inlined structs
// that both read another.
type dupMixedSiblings struct {
	H             dupHook `yaml:"h"`
	DupInner      `yaml:",inline"`
	DupPlainInner `yaml:",inline"`
}

const dupRefusal = "a key of Go type %s is read by more than one field, and one of them has an UnmarshalYAML method"

// wantDupRefusal checks that err is the refusal for a struct of Go type
// typ at line:column, and that it never quotes the input.
func wantDupRefusal(t *testing.T, err error, typ string, line, column int) {
	t.Helper()
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || de.Line != line || de.Column != column {
		t.Fatalf("err = %v, want a type error at %d:%d", err, line, column)
	}
	if want := fmt.Sprintf(dupRefusal, typ); de.Msg != want {
		t.Errorf("Msg = %q, want %q", de.Msg, want)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error quotes the input: %q", err.Error())
	}
}

// wantDecoded decodes src into a new value of want's type and checks
// that it succeeds with want.
func wantDecoded[T any](t *testing.T, src string, want T) {
	t.Helper()
	var got T
	if err := yaml.Unmarshal([]byte(src), &got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestHookFieldThatShadowsAnInlinedFieldRuns(t *testing.T) {
	t.Parallel()
	// goccy zeroes the inlined field that the outer one shadows, so the
	// outer field is the key's only reader and its method runs.
	wantDecoded(t, "y: 1\nx: secret\n", dupOuter{X: dupHook{Val: "hook:secret"}, DupInner: DupInner{Y: 1}})
	wantDecoded(t, "x: secret\n", dupPointerInner{X: dupHook{Val: "hook:secret"}, DupInner: &DupInner{}})
}

func TestHookStructWithoutTheKeyStillDecodes(t *testing.T) {
	t.Parallel()
	var v dupOuter
	if err := yaml.Unmarshal([]byte("y: 1\n"), &v); err != nil || v.Y != 1 {
		t.Errorf("got %+v, %v", v, err)
	}
}

func TestPlainDuplicateFieldKeepsTodaysBehavior(t *testing.T) {
	t.Parallel()
	var v dupPlain
	if err := yaml.Unmarshal([]byte("x: a\n"), &v); err != nil {
		t.Errorf("a struct with no hook now fails: %v", err)
	}
}

func TestShadowedHookFieldDecodesAsGoccyShadowsIt(t *testing.T) {
	t.Parallel()
	// The plain outer field wins, and goccy zeroes the hooked field, so
	// the method has nothing to fill and the decode is goccy's own.
	wantDecoded(t, "x: secret\n", dupOuterPlain{X: "secret"})
	wantDecoded(t, "x: secret\n", dupOuterPlainPointer{X: "secret", DupHookInner: &DupHookInner{}})
}

func TestPlainDuplicateInAHookedStructDecodesAsBefore(t *testing.T) {
	t.Parallel()
	wantDecoded(t, "h: a\nx: b\n", dupMixed{H: dupHook{Val: "hook:a"}, X: "b"})
	wantDecoded(t, "h: a\nx: b\n", dupMixedSiblings{
		H:             dupHook{Val: "hook:a"},
		DupInner:      DupInner{X: "b"},
		DupPlainInner: DupPlainInner{X: "b"},
	})
}

func TestHookKeyReadByTwoInlinedFieldsIsRefused(t *testing.T) {
	t.Parallel()
	var v dupSiblings
	err := yaml.Unmarshal([]byte("y: 1\nx: secret\n"), &v)
	wantDupRefusal(t, err, "yaml_test.dupSiblings", 2, 1)
}

func TestHookKeyTwoInlineLevelsDownIsRefused(t *testing.T) {
	t.Parallel()
	var v dupTwoDeep
	err := yaml.Unmarshal([]byte("x: secret\n"), &v)
	wantDupRefusal(t, err, "yaml_test.dupTwoDeep", 1, 1)
}

func TestHookKeyRefusedInsideAMapAndASequence(t *testing.T) {
	t.Parallel()
	var m map[string]dupSiblings
	err := yaml.Unmarshal([]byte("a:\n  y: 1\nb:\n  x: secret\n"), &m)
	wantDupRefusal(t, err, "yaml_test.dupSiblings", 4, 3)

	var s []*dupSiblings
	err = yaml.Unmarshal([]byte("- y: 1\n- y: 2\n  x: secret\n"), &s)
	wantDupRefusal(t, err, "yaml_test.dupSiblings", 3, 3)
}

func TestHookKeyRefusedOnNodeDecode(t *testing.T) {
	t.Parallel()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("y: 1\nx: secret\n"), &n); err != nil {
		t.Fatal(err)
	}
	var v dupSiblings
	wantDupRefusal(t, n.Decode(&v), "yaml_test.dupSiblings", 2, 1)
}

func TestHookKeyRefusalIsTheSameOnEveryRun(t *testing.T) {
	t.Parallel()
	// Two refused keys, each in a value of a map, so the first one in the
	// document must win every time, whatever order a Go map would give.
	src := []byte("a:\n  x: secret\nb:\n  x: secret\nc:\n  y: nope\n")
	for range 200 {
		var m map[string]dupSiblings
		err := yaml.Unmarshal(src, &m)
		wantDupRefusal(t, err, "yaml_test.dupSiblings", 2, 3)
	}
}

func TestHookKeyRefusalCountsLinesFromTheStreamStart(t *testing.T) {
	t.Parallel()
	d := yaml.NewDecoder(strings.NewReader("y: 1\n---\ny: 2\nx: secret\n"))
	var first dupSiblings
	if err := d.Decode(&first); err != nil || first.Y != 1 {
		t.Fatalf("got %+v, %v", first, err)
	}
	var second dupSiblings
	wantDupRefusal(t, d.Decode(&second), "yaml_test.dupSiblings", 4, 1)
}
