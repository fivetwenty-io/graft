package yaml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

type dupHook struct{ Val string }

func (h *dupHook) UnmarshalYAML(n *yaml.Node) error {
	h.Val = "hook:" + n.Value
	return nil
}

// DupInner is inlined beside a hooked field that reads the same key.
type DupInner struct {
	X string `yaml:"x"`
	Y int    `yaml:"y"`
}

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
	X dupHook `yaml:"x"`
}

type dupOuterPlain struct {
	X            string `yaml:"x"`
	DupHookInner `yaml:",inline"`
}

// dupPointerInner reaches the inlined struct through a pointer.
type dupPointerInner struct {
	X         dupHook `yaml:"x"`
	*DupInner `yaml:",inline"`
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
	if want := strings.Replace(dupRefusal, "%s", typ, 1); de.Msg != want {
		t.Errorf("Msg = %q, want %q", de.Msg, want)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error quotes the input: %q", err.Error())
	}
}

func TestHookKeyReadByTwoFieldsIsRefused(t *testing.T) {
	t.Parallel()
	var v dupOuter
	err := yaml.Unmarshal([]byte("y: 1\nx: secret\n"), &v)
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || de.Line != 2 || de.Column != 1 {
		t.Fatalf("err = %v, want a type error at 2:1", err)
	}
	want := "a key of Go type yaml_test.dupOuter is read by more than one field, and one of them has an UnmarshalYAML method"
	if de.Msg != want {
		t.Errorf("Msg = %q", de.Msg)
	}
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

func TestHookKeyRefusedWhenTheInlinedFieldHasTheHook(t *testing.T) {
	t.Parallel()
	var v dupOuterPlain
	err := yaml.Unmarshal([]byte("x: secret\n"), &v)
	wantDupRefusal(t, err, "yaml_test.dupOuterPlain", 1, 1)
}

func TestHookKeyRefusedThroughAnInlinePointer(t *testing.T) {
	t.Parallel()
	var v dupPointerInner
	err := yaml.Unmarshal([]byte("x: secret\n"), &v)
	wantDupRefusal(t, err, "yaml_test.dupPointerInner", 1, 1)
}

func TestHookKeyRefusedInsideAMapAndASequence(t *testing.T) {
	t.Parallel()
	var m map[string]dupOuter
	err := yaml.Unmarshal([]byte("a:\n  y: 1\nb:\n  x: secret\n"), &m)
	wantDupRefusal(t, err, "yaml_test.dupOuter", 4, 3)

	var s []*dupOuter
	err = yaml.Unmarshal([]byte("- y: 1\n- y: 2\n  x: secret\n"), &s)
	wantDupRefusal(t, err, "yaml_test.dupOuter", 3, 3)
}

func TestHookKeyRefusedOnNodeDecode(t *testing.T) {
	t.Parallel()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("y: 1\nx: secret\n"), &n); err != nil {
		t.Fatal(err)
	}
	var v dupOuter
	wantDupRefusal(t, n.Decode(&v), "yaml_test.dupOuter", 2, 1)
}

func TestHookKeyRefusalIsTheSameOnEveryRun(t *testing.T) {
	t.Parallel()
	// Two refused keys, each in a value of a map, so the first one in the
	// document must win every time, whatever order a Go map would give.
	src := []byte("a:\n  x: secret\nb:\n  x: secret\nc:\n  y: nope\n")
	for range 200 {
		var m map[string]dupOuter
		err := yaml.Unmarshal(src, &m)
		wantDupRefusal(t, err, "yaml_test.dupOuter", 2, 3)
	}
}

func TestHookKeyRefusalCountsLinesFromTheStreamStart(t *testing.T) {
	t.Parallel()
	d := yaml.NewDecoder(strings.NewReader("y: 1\n---\ny: 2\nx: secret\n"))
	var first dupOuter
	if err := d.Decode(&first); err != nil || first.Y != 1 {
		t.Fatalf("got %+v, %v", first, err)
	}
	var second dupOuter
	wantDupRefusal(t, d.Decode(&second), "yaml_test.dupOuter", 4, 1)
}
