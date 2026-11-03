package graft

import (
	"context"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/postprocess"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// noopProcessor returns the document it is given.
type noopProcessor struct{}

func (noopProcessor) Name() string            { return "noop" }
func (noopProcessor) Phase() PostProcessPhase { return PhaseNormal }
func (noopProcessor) Process(_ context.Context, doc Document, _ *ProcessMetadata) (Document, error) {
	return doc, nil
}

// setProcessor adds a key through Document.Set.
type setProcessor struct{}

func (setProcessor) Name() string            { return "set" }
func (setProcessor) Phase() PostProcessPhase { return PhaseNormal }
func (setProcessor) Process(_ context.Context, doc Document, _ *ProcessMetadata) (Document, error) {
	if err := doc.Set("extra", 1); err != nil {
		return nil, err
	}
	return doc, nil
}

// TestLiteralSurvivesPostProcessors runs Execute with each built-in
// post-processor and two custom ones. The result still holds its
// wrappers, so a second Evaluate leaves the operator text inside them
// alone, and no processor acts on a prune marker or a << key inside a
// literal.
func TestLiteralSurvivesPostProcessors(t *testing.T) {
	params := func() map[string]interface{} {
		return map[string]interface{}{
			"pr":     "(( prune ))",
			"inj":    map[string]interface{}{"<<": map[string]interface{}{"x": 1}, "y": 2},
			"op":     "(( grab never.here ))",
			"secret": "hunter22",
		}
	}
	plan := "out: (( grab params ))\nother: 1\n"
	tests := []struct {
		name    string
		proc    PostProcessor
		check   func(t *testing.T, got Document)
		literal []string
	}{
		{name: "pruner", proc: NewPruner("other"), literal: []string{"params", "out"}},
		{
			name: "pruner inside a literal", proc: NewPruner("params.secret"), literal: []string{"params", "out"},
			check: func(t *testing.T, got Document) {
				if got.Has("params.secret") {
					t.Fatal("params.secret was not pruned")
				}
				if !got.Has("out.secret") {
					t.Fatal("pruning params.secret reached out.secret")
				}
			},
		},
		{name: "cherry picker", proc: NewCherryPicker("params", "out"), literal: []string{"params", "out"}},
		{name: "cherry picker inside a literal", proc: NewCherryPicker("params.inj", "out"), literal: []string{"params.inj", "out"}},
		{name: "key sorter", proc: NewKeySorter(true), literal: []string{"params", "out"}},
		{
			name: "security redactor", proc: NewSecurityRedactor([]string{"secret"}, ""), literal: []string{"params", "out"},
			check: func(t *testing.T, got Document) {
				if v, _ := got.Get("params.secret"); v != postprocess.DefaultRedactionMask {
					t.Fatalf("params.secret = %#v, want it redacted", v)
				}
			},
		},
		{name: "prune processor", proc: &wrappedProcessor{inner: postprocess.NewPruneProcessor()}, literal: []string{"params", "out"}},
		{name: "inject processor", proc: &wrappedProcessor{inner: postprocess.NewInjectProcessor()}, literal: []string{"params", "out"}},
		{name: "custom no-op", proc: noopProcessor{}, literal: []string{"params", "out"}},
		{name: "custom set", proc: setProcessor{}, literal: []string{"params", "out"}},
	}
	for _, mode := range literalEngineModes(t) {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				got, err := mode.eng.Merge(context.Background(), mustParse(t, mode.eng, plan), userDoc(params())).
					WithPostProcessors(tt.proc).Execute()
				if err != nil {
					t.Fatalf("Execute: %v", err)
				}
				root := treeOf(got)
				for _, path := range tt.literal {
					c, err := tree.ParseCursor(path)
					if err != nil {
						t.Fatal(err)
					}
					v, err := c.Resolve(root)
					if err != nil {
						t.Fatalf("%s: %v", path, err)
					}
					if _, ok := v.(LiteralValue); !ok {
						t.Fatalf("%s is %T after the post-processor, want a LiteralValue", path, v)
					}
				}
				for _, base := range []string{"params", "out"} {
					if !got.Has(base + ".inj") {
						continue
					}
					if v, _ := got.Get(base + ".pr"); v != nil && v != "(( prune ))" {
						t.Fatalf("%s.pr = %#v", base, v)
					}
					inj, err := got.GetMap(base + ".inj")
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(inj, map[string]interface{}{"<<": map[string]interface{}{"x": 1}, "y": 2}) {
						t.Fatalf("%s.inj = %#v, a processor acted on a << key inside a literal", base, inj)
					}
				}
				if got.Has("params.pr") {
					if v, _ := got.Get("params.pr"); v != "(( prune ))" {
						t.Fatalf("params.pr = %#v", v)
					}
				} else if tt.name != "cherry picker inside a literal" {
					t.Fatal("params.pr was pruned from inside a literal")
				}
				if tt.check != nil {
					tt.check(t, got)
				}

				again, err := mode.eng.Evaluate(context.Background(), got)
				if err != nil {
					t.Fatalf("second Evaluate ran text inside a literal: %v", err)
				}
				if v, _ := again.Get("out.op"); v != "(( grab never.here ))" {
					t.Fatalf("out.op = %#v after a second Evaluate", v)
				}
			})
		}
	}
}
