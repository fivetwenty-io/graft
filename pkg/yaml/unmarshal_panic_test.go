package yaml_test

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

type hiddenPart struct {
	V string
}

// hiddenEmbedded embeds an unexported struct with no ,inline option,
// which goccy sets as an ordinary field and panics on.
type hiddenEmbedded struct {
	hiddenPart
	N int
}

// hiddenInline embeds an unexported struct as ,inline, which goccy
// refuses with an error.
type hiddenInline struct {
	hiddenPart `yaml:",inline"`
	N          int
}

// panicky panics from its own UnmarshalYAML method.
type panicky struct{}

func (*panicky) UnmarshalYAML([]byte) error { panic("panicky") }

// TestUnmarshalTargetShapesNeverPanic checks that a target shape goccy
// cannot fill gives a type error that quotes nothing from the input.
func TestUnmarshalTargetShapesNeverPanic(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		v    func() any
		line int
	}{
		{"unexported embedded struct", "hiddenpart: {v: S3KRITzq9}\nn: 1\n", func() any { return &hiddenEmbedded{} }, 0},
		{"unexported inline struct", "v: S3KRITzq9\nn: 1\n", func() any { return &hiddenInline{} }, 0},
		{"pointer to pointer", "v: S3KRITzq9\n", func() any { return &struct{ V **string }{} }, 1},
	}
	for _, m := range modes {
		for _, tc := range cases {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				var err error
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("UnmarshalWith panicked: %v", r)
						}
					}()
					err = yaml.UnmarshalWith([]byte(tc.doc), tc.v(), m.opts)
				}()
				wantDecodeError(t, err, yaml.ErrorKindType, tc.line)
				wantNoLeak(t, err, "S3KRIT")
			})
		}
	}
	t.Run("embedded key absent", func(t *testing.T) {
		var got hiddenEmbedded
		if err := yaml.Unmarshal([]byte("n: 1\n"), &got); err != nil || got.N != 1 {
			t.Errorf("got %+v, %v, want N 1 and no error", got, err)
		}
	})
}

// TestUnmarshalUserPanicPropagates checks that a panic from a caller's
// own method is not turned into an error.
func TestUnmarshalUserPanicPropagates(t *testing.T) {
	defer func() {
		if r := recover(); r != "panicky" {
			t.Errorf("recovered %v, want the method's own panic", r)
		}
	}()
	var got struct{ P panicky }
	_ = yaml.Unmarshal([]byte("p: x\n"), &got)
	t.Error("Unmarshal returned")
}
