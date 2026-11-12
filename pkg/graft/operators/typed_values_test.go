package operators_test

import (
	"context"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

func evaluateTypedGoDoc(t *testing.T, parallel bool) graft.Document {
	t.Helper()
	engine, err := graft.NewEngine(graft.WithParallel(parallel))
	if err != nil {
		t.Fatal(err)
	}
	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		t.Fatalf("engine is %T, want *graft.DefaultEngine", engine)
	}
	if got := de.GetWorkerPool() != nil; got != parallel {
		t.Fatalf("worker pool present = %v, want %v", got, parallel)
	}
	doc := graft.NewDocument(map[string]interface{}{
		"l": []string{"a", "b"},
		"j": "(( join \",\" l ))",
		"c": "(( grab l.1 ))",
	})
	out, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("parallel=%v: %v", parallel, err)
	}
	return out
}

func TestOperatorsReadTypedGoValues(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		out := evaluateTypedGoDoc(t, parallel)
		if v, _ := out.GetString("j"); v != "a,b" {
			t.Errorf("parallel=%v: j = %q", parallel, v)
		}
		if v, _ := out.GetString("c"); v != "b" {
			t.Errorf("parallel=%v: c = %q", parallel, v)
		}
	}
}
