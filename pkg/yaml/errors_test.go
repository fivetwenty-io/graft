package yaml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// wantDecodeError checks that err is a *yaml.DecodeError of kind at
// line, and returns it.
func wantDecodeError(t *testing.T, err error, kind yaml.ErrorKind, line int) *yaml.DecodeError {
	t.Helper()
	var de *yaml.DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("got %T (%v), want *yaml.DecodeError", err, err)
	}
	if de.Kind != kind {
		t.Errorf("Kind = %d, want %d (%v)", de.Kind, kind, de)
	}
	if de.Line != line {
		t.Errorf("Line = %d, want %d (%v)", de.Line, line, de)
	}
	if de.Msg == "" {
		t.Errorf("Msg is empty")
	}
	if _, ok := any(de).(interface{ Unwrap() error }); ok {
		t.Errorf("DecodeError has an Unwrap method, which would expose the input")
	}
	return de
}

// wantNoLeak checks that the error text quotes none of the scalars the
// input holds.
func wantNoLeak(t *testing.T, err error, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(err.Error(), s) {
			t.Errorf("Error() %q quotes %q from the input", err.Error(), s)
		}
	}
}

// billionLaughs expands to 9^7 nodes, past the 400,000-node cap.
const billionLaughs = `a: &a ["lol","lol","lol","lol","lol","lol","lol","lol","lol"]
b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]
c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]
d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c]
e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d]
f: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e]
g: &g [*f,*f,*f,*f,*f,*f,*f,*f,*f]
`

// TestUnmarshalDecodeErrors records the kind and position graft's own
// parse path gives for each input, and checks that the error text quotes
// no scalar from it. Line is the input's line, counted from 1, so a pair
// of cases on line 1 and line 3 pins each kind of error the reader
// numbers itself.
func TestUnmarshalDecodeErrors(t *testing.T) {
	cases := []struct {
		name    string
		doc     string
		kind    yaml.ErrorKind
		line    int
		column  int
		secrets []string
	}{
		{"unclosed flow sequence", "a: 1\nb: [supersecret\nc: 3\n", yaml.ErrorKindSyntax, 3, 1, []string{"supersecret"}},
		{"unclosed quote", "a: 1\npassword: \"supersecret\n", yaml.ErrorKindSyntax, 2, 11, []string{"supersecret"}},
		{"duplicate key", "a: 1\npassword: supersecret\npassword: other\n", yaml.ErrorKindSyntax, 3, 1, []string{"supersecret", "other", "password"}},
		{"bool tag on a word", "a: !!bool supersecret\n", yaml.ErrorKindSyntax, 1, 10, []string{"supersecret"}},
		{"unknown escape", "a: \"sup\\qersecret\"\n", yaml.ErrorKindSyntax, 1, 9, []string{"supersecret", "ersecret", "'q'"}},
		{"self-containing anchor", "a: &x\n  b: *x\n  pw: supersecret\n", yaml.ErrorKindSyntax, 2, 6, []string{"supersecret"}},
		{"self-containing anchor on line 1", "c: &x [*x, supersecret]\n", yaml.ErrorKindSyntax, 1, 8, []string{"supersecret"}},
		{"self-containing anchor on line 3", "a: 1\npw: supersecret\nc: &x [*x]\n", yaml.ErrorKindSyntax, 3, 8, []string{"supersecret"}},
		{"unknown merge alias", "pw: supersecret\n<<: *S3KRITzq9\n", yaml.ErrorKindSyntax, 0, 0, []string{"supersecret", "S3KRITzq9"}},
		{"bad anchor name on line 1", "a: &x[1] supersecret\n", yaml.ErrorKindSyntax, 1, 0, []string{"supersecret"}},
		{"bad anchor name on line 3", "a: 1\npw: supersecret\nc: &x[1] y\n", yaml.ErrorKindSyntax, 3, 0, []string{"supersecret"}},
		{"bad tag on line 1", "c: !x^y supersecret\n", yaml.ErrorKindSyntax, 1, 0, []string{"supersecret"}},
		{"bad tag on line 3", "a: 1\npw: supersecret\nc: !x^y z\n", yaml.ErrorKindSyntax, 3, 0, []string{"supersecret"}},
		{"bad directive on line 1", "%FOO supersecret\n---\na: 1\n", yaml.ErrorKindSyntax, 1, 0, []string{"supersecret"}},
		{"bad directive on line 3", "# c\n# d\n%FOO supersecret\n---\na: 1\n", yaml.ErrorKindSyntax, 3, 0, []string{"supersecret"}},
		{"bad block header", "a: |supersecret\n  y\n", yaml.ErrorKindSyntax, 1, 4, []string{"supersecret"}},
		{"billion laughs", billionLaughs, yaml.ErrorKindLimit, 0, 0, []string{"lol"}},
		{"nesting depth on line 1", "x: " + strings.Repeat("[", 10001) + "\n", yaml.ErrorKindLimit, 1, 0, nil},
		{"nesting depth", "pw: supersecret\nx: " + strings.Repeat("[", 10001) + "\n", yaml.ErrorKindLimit, 2, 0, []string{"supersecret"}},
		{"nesting depth on line 3", "a: 1\npw: supersecret\nx: " + strings.Repeat("[", 10001) + "\n", yaml.ErrorKindLimit, 3, 0, []string{"supersecret"}},
		{"content after end marker", "pw: supersecret\n...\nb: 2\n", yaml.ErrorKindSyntax, 3, 0, []string{"supersecret"}},
	}
	for _, tc := range cases {
		for _, m := range modes {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				var got map[string]any
				err := yaml.UnmarshalWith([]byte(tc.doc), &got, m.opts)
				de := wantDecodeError(t, err, tc.kind, tc.line)
				if de.Column != tc.column {
					t.Errorf("Column = %d, want %d", de.Column, tc.column)
				}
				wantNoLeak(t, err, tc.secrets...)
				if strings.HasSuffix(de.Msg, ":") || strings.HasSuffix(de.Msg, " ") {
					t.Errorf("Msg %q ends where a dropped span was", de.Msg)
				}
				if strings.Contains(err.Error(), "\n") {
					t.Errorf("Error() %q spans lines, so it may carry source text", err.Error())
				}
				if got != nil {
					t.Errorf("target set to %v on error", got)
				}
			})
		}
	}
}

// TestUnmarshalRefusesInterfaceKeyedMaps checks that the target check
// refuses map[interface{}]interface{} wherever it sits in the target type,
// before anything parses.
func TestUnmarshalRefusesInterfaceKeyedMaps(t *testing.T) {
	type plan struct {
		Name     string                      `yaml:"name"`
		Manifest map[interface{}]interface{} `json:"-"`
	}
	type wrapper struct {
		Plans []*plan `yaml:"plans"`
	}
	type named map[interface{}]interface{}
	type key struct {
		Extra map[interface{}]interface{}
	}
	var (
		top      map[interface{}]interface{}
		field    plan
		nested   wrapper
		slice    []map[interface{}]interface{}
		mapValue map[string]map[interface{}]interface{}
		alias    named
		pointer  *map[interface{}]interface{}
		mapKey   map[*key]string
	)
	cases := []struct {
		name   string
		target any
		where  string
	}{
		{"top level", &top, "map[interface {}]interface {}"},
		{"struct field", &field, "Manifest"},
		{"nested struct field", &nested, "Manifest"},
		{"slice element", &slice, "[]"},
		{"map value", &mapValue, "[]"},
		{"named type", &alias, "named"},
		{"pointer to map", &pointer, "map[interface {}]interface {}"},
		{"map key", &mapKey, "{key}.Extra"},
	}
	// The input is broken, so a check that ran after parsing would
	// report a syntax error instead.
	const doc = "name: [supersecret\n"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := yaml.Unmarshal([]byte(doc), tc.target)
			de := wantDecodeError(t, err, yaml.ErrorKindType, 0)
			if !strings.Contains(de.Msg, "map[interface {}]interface {}") {
				t.Errorf("Msg %q does not name map[interface {}]interface {}", de.Msg)
			}
			if !strings.Contains(de.Msg, tc.where) {
				t.Errorf("Msg %q does not say where the map sits (%q)", de.Msg, tc.where)
			}
			wantNoLeak(t, err, "supersecret")
		})
	}
}

// TestUnmarshalRefusesNilAndNonPointerTargets checks that a target
// Unmarshal cannot write through is refused rather than panicking.
func TestUnmarshalRefusesNilAndNonPointerTargets(t *testing.T) {
	var (
		nilAny   *any
		nilMap   *map[string]any
		nilSlice *[]any
	)
	cases := []struct {
		name   string
		target any
		want   string
	}{
		{"nil interface", nil, "Unmarshal(nil)"},
		{"nil *any", nilAny, "Unmarshal(nil *interface {})"},
		{"nil *map", nilMap, "Unmarshal(nil *map[string]interface {})"},
		{"nil *slice", nilSlice, "Unmarshal(nil *[]interface {})"},
		{"map value", map[string]any{}, "Unmarshal(non-pointer map[string]interface {})"},
		{"slice value", []any{}, "Unmarshal(non-pointer []interface {})"},
		{"string value", "s", "Unmarshal(non-pointer string)"},
		{"struct value", struct{ A int }{}, "Unmarshal(non-pointer struct { A int })"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := yaml.Unmarshal([]byte("a: supersecret\n"), tc.target)
			de := wantDecodeError(t, err, yaml.ErrorKindType, 0)
			if de.Msg != tc.want {
				t.Errorf("Msg = %q, want %q", de.Msg, tc.want)
			}
			wantNoLeak(t, err, "supersecret")
		})
	}
}

// TestUnmarshalGenericTypeMismatch checks that a root that does not fit
// the generic target is a type error that names both types.
func TestUnmarshalGenericTypeMismatch(t *testing.T) {
	cases := []struct {
		name   string
		doc    string
		target any
		want   string
	}{
		{"sequence into map", "- supersecret\n", new(map[string]any), "cannot unmarshal []interface {} into Go value of type map[string]interface {}"},
		{"scalar into map", "supersecret\n", new(map[string]any), "cannot unmarshal string into Go value of type map[string]interface {}"},
		{"mapping into slice", "a: supersecret\n", new([]any), "cannot unmarshal map[string]interface {} into Go value of type []interface {}"},
		{"bool into slice", "yes\n", new([]any), "cannot unmarshal bool into Go value of type []interface {}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := yaml.Unmarshal([]byte(tc.doc), tc.target)
			de := wantDecodeError(t, err, yaml.ErrorKindType, 0)
			if de.Msg != tc.want {
				t.Errorf("Msg = %q, want %q", de.Msg, tc.want)
			}
			wantNoLeak(t, err, "supersecret")
		})
	}
}

// TestDecodeErrorFormat pins the Error() format the spec gives.
func TestDecodeErrorFormat(t *testing.T) {
	cases := []struct {
		name string
		err  yaml.DecodeError
		want string
	}{
		{"with line", yaml.DecodeError{Line: 3, Column: 7, Kind: yaml.ErrorKindSyntax, Msg: "found a problem"}, "yaml: line 3: found a problem"},
		{"line one", yaml.DecodeError{Line: 1, Kind: yaml.ErrorKindType, Msg: "bad type"}, "yaml: line 1: bad type"},
		{"no line", yaml.DecodeError{Kind: yaml.ErrorKindLimit, Msg: "too big"}, "yaml: too big"},
		{"column without line", yaml.DecodeError{Column: 4, Kind: yaml.ErrorKindSyntax, Msg: "odd"}, "yaml: odd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestErrorKindValues pins the kinds' values, which callers may store.
func TestErrorKindValues(t *testing.T) {
	if yaml.ErrorKindSyntax != 1 || yaml.ErrorKindType != 2 || yaml.ErrorKindLimit != 3 {
		t.Errorf("kinds are %d, %d, %d, want 1, 2, 3", yaml.ErrorKindSyntax, yaml.ErrorKindType, yaml.ErrorKindLimit)
	}
}

// TestNodeRouteErrorsMatchOtherRoutes checks that a syntax error read
// into a Node carries the column and the wording the generic and typed
// routes give the same input.
func TestNodeRouteErrorsMatchOtherRoutes(t *testing.T) {
	t.Parallel()
	for _, doc := range []string{"a: [1, 2", "a: {b: 1", "a: *nope", "a:\n  - x\n *y"} {
		var node yaml.Node
		nodeErr := wantDecodeError(t, yaml.Unmarshal([]byte(doc), &node), yaml.ErrorKindSyntax, lineOf(doc))
		var generic any
		genericErr := wantDecodeError(t, yaml.Unmarshal([]byte(doc), &generic), yaml.ErrorKindSyntax, lineOf(doc))
		if nodeErr.Column == 0 {
			t.Errorf("%q: Node route Column = 0, want the column %d the other routes report", doc, genericErr.Column)
		}
		if *nodeErr != *genericErr {
			t.Errorf("%q: Node route error %+v, other routes %+v", doc, *nodeErr, *genericErr)
		}
		wantNoLeak(t, nodeErr, "nope")
	}
}

// lineOf returns the line of the last line break's successor in doc,
// which is the line the errors above lie on.
func lineOf(doc string) int {
	return strings.Count(strings.TrimSuffix(doc, "\n"), "\n") + 1
}

// TestTaggedIntWithoutDigits checks the two routes' answers for an !!int
// tag on text that is not an integer. The generic route decodes it to 0,
// as graft merge does, and the typed route reports an error that names
// the mismatch once and quotes nothing from the input.
func TestTaggedIntWithoutDigits(t *testing.T) {
	t.Parallel()
	const doc = "a: !!int S3KRIT\n"

	var generic map[string]any
	if err := yaml.Unmarshal([]byte(doc), &generic); err != nil || generic["a"] != 0 {
		t.Errorf("generic route gave %v, %v, want a: 0 and no error", generic, err)
	}

	var typed struct {
		A int `yaml:"a"`
	}
	err := yaml.Unmarshal([]byte(doc), &typed)
	de := wantDecodeError(t, err, yaml.ErrorKindType, 1)
	wantNoLeak(t, err, "S3KRIT")
	const want = "cannot unmarshal a !!int value that is not a valid int into Go struct field .A of type int"
	if de.Msg != want {
		t.Errorf("Msg = %q, want %q", de.Msg, want)
	}
}
