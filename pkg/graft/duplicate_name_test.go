package graft

import (
	"context"
	"strings"
	"testing"
)

// mergeToYAML merges the given documents and returns the rendered
// output, or the merge error.
func mergeToYAML(t *testing.T, docs ...string) (string, error) {
	t.Helper()

	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	parsed := make([]Document, 0, len(docs))
	for i, d := range docs {
		doc, err := engine.ParseYAML([]byte(d))
		if err != nil {
			t.Fatalf("ParseYAML(doc %d) error = %v", i, err)
		}
		parsed = append(parsed, doc)
	}

	result, err := engine.Merge(context.Background(), parsed...).Execute()
	if err != nil {
		return "", err
	}
	out, err := result.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML() error = %v", err)
	}
	return string(out), nil
}

// A duplicated name in a list makes every path under it ambiguous: the
// name-keyed cursor resolves to the first match, so operators under the
// second and later entries collide onto the first entry's slot. Left
// alone, the last one silently wins and its value lands in the FIRST
// entry, while the others keep their raw operator text. That is silent
// cross-entry corruption, so the merge is refused instead.
func TestMergeRefusesAnOperatorUnderADuplicateName(t *testing.T) {
	_, err := mergeToYAML(t, `meta:
  first: FIRST
  second: SECOND
jobs:
  - name: alpha
    cmd: (( grab meta.first ))
  - name: alpha
    cmd: (( grab meta.second ))
`)
	if err == nil {
		t.Fatalf("Execute() succeeded; want an ambiguous-name error")
	}

	got := err.Error()
	for _, want := range []string{
		"$.jobs:",
		`duplicate name "alpha"`,
		"jobs.0",
		"jobs.1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("error missing %q\ngot:\n%s", want, got)
		}
	}
}

// The error names every colliding index, not just the pair, so a list
// with three same-named entries points at all three.
func TestDuplicateNameErrorNamesEveryCollidingIndex(t *testing.T) {
	_, err := mergeToYAML(t, `meta:
  v: V
jobs:
  - name: alpha
    cmd: (( grab meta.v ))
  - name: alpha
    cmd: (( grab meta.v ))
  - name: alpha
    cmd: (( grab meta.v ))
`)
	if err == nil {
		t.Fatalf("Execute() succeeded; want an ambiguous-name error")
	}
	for _, want := range []string{"jobs.0", "jobs.1", "jobs.2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q\ngot:\n%s", want, err.Error())
		}
	}
}

// Load-bearing spruce-parity guard. Duplicate names are ordinary in
// real documents; only an OPERATOR under one is ambiguous. A list that
// repeats a name and carries no operator must merge exactly as it
// always has, or this change breaks a large class of working input for
// no benefit.
func TestDuplicateNamesWithoutOperatorsStillMerge(t *testing.T) {
	out, err := mergeToYAML(t, `jobs:
  - name: alpha
    cmd: one
  - name: alpha
    cmd: two
`)
	if err != nil {
		t.Fatalf("Execute() error = %v; duplicate names alone must not fail", err)
	}
	for _, want := range []string{"one", "two"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
}

// An operator elsewhere in the document is unaffected by a duplicate
// name that has no operator under it.
func TestOperatorOutsideADuplicateNamedEntryStillRuns(t *testing.T) {
	out, err := mergeToYAML(t, `meta:
  v: RESOLVED
jobs:
  - name: alpha
    cmd: one
  - name: alpha
    cmd: two
top: (( grab meta.v ))
`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out, "top: RESOLVED") {
		t.Errorf("operator outside the duplicate entries did not run\ngot:\n%s", out)
	}
}

// Control: unique names keep evaluating per entry, unchanged.
func TestUniqueNamesEvaluateEveryEntry(t *testing.T) {
	out, err := mergeToYAML(t, `meta:
  first: FIRST
  second: SECOND
jobs:
  - name: alpha
    cmd: (( grab meta.first ))
  - name: beta
    cmd: (( grab meta.second ))
`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"FIRST", "SECOND"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, out)
		}
	}
}

// Ambiguity is inherited by everything beneath the entry, not just its
// immediate fields: a name-keyed cursor cannot address any depth under
// a shared name. The reported path is the operator's own, so a deeply
// nested one still points at itself.
func TestDuplicateNameErrorReachesNestedOperators(t *testing.T) {
	_, err := mergeToYAML(t, `meta:
  v: V
jobs:
  - name: alpha
    props:
      inner:
        deep: (( grab meta.v ))
  - name: alpha
    props:
      inner:
        deep: plain
`)
	if err == nil {
		t.Fatalf("Execute() succeeded; want an ambiguous-name error")
	}
	want := "$.jobs.0.props.inner.deep"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error does not name the nested operator %q\ngot:\n%s", want, err.Error())
	}
}

// The same document evaluated through the parallel path must reject it
// too; DataFlow is shared, but the dispatch is not, so pin both.
func TestDuplicateNameErrorHoldsUnderParallelEvaluation(t *testing.T) {
	engine, err := NewEngine(WithParallel(true))
	if err != nil {
		t.Fatalf("NewEngine(WithParallel) error = %v", err)
	}
	doc, err := engine.ParseYAML([]byte(`meta:
  v: V
jobs:
  - name: alpha
    cmd: (( grab meta.v ))
  - name: alpha
    cmd: (( grab meta.v ))
`))
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}
	if _, err := engine.Merge(context.Background(), doc).Execute(); err == nil {
		t.Fatalf("Execute() succeeded under parallel evaluation; want an error")
	} else if !strings.Contains(err.Error(), `duplicate name "alpha"`) {
		t.Errorf("unexpected error under parallel evaluation: %v", err)
	}
}
