package graft

import (
	"context"
	"testing"
)

func TestEngineReadsTaggedMergeKeys(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	doc := mustParseYAMLDoc(t, engine, "base: &b {a: 1}\nm:\n  !!merge <<: *b\n  c: 2\n")
	out, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := out.GetInt("m.a"); v != 1 {
		t.Errorf("m.a = %d", v)
	}
	if _, err := out.Get("m.<<"); err == nil {
		t.Error("the merge key stayed in the output")
	}
}

// TestTaggedMergeKeysBothModes merges a document whose merge keys carry
// tags, with an operator that reads a merged key, under the parallel and
// the sequential evaluator, and expects the same result from both.
func TestTaggedMergeKeysBothModes(t *testing.T) {
	const base = "base: &b {a: 1}\nm:\n  ? !!merge <<\n  : *b\n  c: 2\ns:\n  !!str <<: kept\n"
	const overlay = "o:\n  !!merge <<: *b\nr: (( grab m.a ))\n"
	const want = "base:\n  a: 1\nm:\n  a: 1\n  c: 2\no:\n  a: 1\nr: 1\ns:\n  <<: kept\n"
	var results []string
	for _, parallel := range []string{"true", "false"} {
		t.Setenv("GRAFT_FEATURE_PARALLEL", parallel)
		engine, err := NewEngine()
		if err != nil {
			t.Fatal(err)
		}
		merged, err := engine.Merge(context.Background(),
			mustParseYAMLDoc(t, engine, base),
			mustParseYAMLDoc(t, engine, "base: &b {a: 1}\n"+overlay)).Execute()
		if err != nil {
			t.Fatalf("parallel=%s: %v", parallel, err)
		}
		out, err := merged.ToYAML()
		if err != nil {
			t.Fatalf("parallel=%s: %v", parallel, err)
		}
		results = append(results, string(out))
	}
	if results[0] != want {
		t.Errorf("parallel result = %q, want %q", results[0], want)
	}
	if results[1] != results[0] {
		t.Errorf("sequential result = %q, parallel %q", results[1], results[0])
	}
}
