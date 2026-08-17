package graft

import (
	"context"
	"errors"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// These tests pin graft's sort post-processing error parity with spruce,
// verified against the spruce binary. Once a (( sort by X )) marker has been
// queued (a prior document's list existed at the path), spruce treats every
// post-merge failure of that queued sort as fatal (exit 2, one error inside
// the "N error(s) detected:" framing):
//
//   - the path no longer resolves (e.g. pruned away):
//     `$.key` could not be found in the datastructure
//   - the path resolves to a map: $.key is a map (not a list)
//   - the path resolves to a scalar: $.key is a scalar (not a list)
//   - the list is unsortable by the requested key: $.key is a list with map
//     entries, where some do not contain name (...)
//
// All four cases fail identically with and without --skip-eval, because
// spruce applies queued sorts as post-processing that runs in both modes.
// spruce reports at most one sort error per run (it returns on the first
// bad path), so each error below must arrive alone in a MultiError.
func TestSortPostProcessingErrorParity(t *testing.T) {
	ctx := context.Background()

	mergeSorted := func(skipEval bool, prune []string, docs ...string) (Document, error) {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		parsed := make([]Document, 0, len(docs))
		for _, d := range docs {
			doc, err := engine.ParseYAML([]byte(d))
			So(err, ShouldBeNil)
			parsed = append(parsed, doc)
		}

		builder := engine.Merge(ctx, parsed...)
		if skipEval {
			builder = builder.SkipEvaluation()
		}
		if len(prune) > 0 {
			builder = builder.WithPrune(prune...)
		}
		return builder.Execute()
	}

	expectSingleSortError := func(err error, substrings ...string) {
		So(err, ShouldNotBeNil)
		var multi MultiError
		So(errors.As(err, &multi), ShouldBeTrue)
		So(multi.Errors, ShouldHaveLength, 1)
		for _, s := range substrings {
			So(err.Error(), ShouldContainSubstring, s)
		}
	}

	listDoc := "key:\n- charlie\n- alpha\n"
	sortMarker := "key: (( sort ))\n"

	Convey("A queued sort whose path was pruned away fails the merge", t, func() {
		for _, skipEval := range []bool{false, true} {
			_, err := mergeSorted(skipEval, []string{"key"}, listDoc, sortMarker)
			expectSingleSortError(err,
				"`", "$.key", "could not be found in the datastructure")
		}
	})

	Convey("A queued sort whose path was overwritten by a scalar fails the merge", t, func() {
		for _, skipEval := range []bool{false, true} {
			_, err := mergeSorted(skipEval, nil, listDoc, sortMarker, "key: scalar-now\n")
			expectSingleSortError(err, "$.key", "is a scalar (not", "a list")
		}
	})

	Convey("A queued sort whose path was overwritten by a map fails the merge", t, func() {
		for _, skipEval := range []bool{false, true} {
			_, err := mergeSorted(skipEval, nil, listDoc, sortMarker, "key:\n  sub: 1\n")
			expectSingleSortError(err, "$.key", "is a map (not", "a list")
		}
	})

	Convey("A queued sort with an unsatisfiable sort key fails the merge in both modes", t, func() {
		// The normal-evaluation half of this pair already worked before the
		// unresolvable/non-list cases were fixed; the --skip-eval half
		// silently skipped the SortList failure and returned the list
		// unsorted with exit 0.
		for _, skipEval := range []bool{false, true} {
			_, err := mergeSorted(skipEval, nil,
				"key:\n- name: b\n- age: 1\n", "key: (( sort by name ))\n")
			expectSingleSortError(err,
				"$.key", "is a list with map entries, where some do not contain name")
		}
	})

	Convey("A successful queued sort still sorts in both modes", t, func() {
		for _, skipEval := range []bool{false, true} {
			result, err := mergeSorted(skipEval, nil, listDoc, sortMarker)
			So(err, ShouldBeNil)

			list, err := result.GetSlice("key")
			So(err, ShouldBeNil)
			So(list, ShouldResemble, []interface{}{"alpha", "charlie"})
		}
	})
}

// nonMapDoc is a Document whose root is not a map — unreachable through
// the constructors and the merge path, but reachable in principle through
// a post-processor that replaces the document.
type nonMapDoc struct{ Document }

func (nonMapDoc) RawData() interface{} { return []interface{}{"a", "b"} }

// TestSortPathsNonMapRootFails pins the last silent-skip branch in
// applySortPaths: queued sort markers against a non-map document root
// must fail like any other unresolvable sort path, never be dropped.
func TestSortPathsNonMapRootFails(t *testing.T) {
	m := &mergeBuilderImpl{}

	// No queued sorts: a non-map root passes through untouched.
	doc, err := m.applySortPaths(nonMapDoc{}, nil)
	if err != nil {
		t.Fatalf("expected no error with no queued sorts, got %v", err)
	}
	if _, ok := doc.(nonMapDoc); !ok {
		t.Fatalf("expected document to pass through unchanged")
	}

	// Queued sorts: fail with the unresolvable-path error, reporting the
	// lexicographically first path.
	_, err = m.applySortPaths(nonMapDoc{}, map[string]string{"zz.list": "name", "aa.list": "name"})
	if err == nil {
		t.Fatalf("expected queued sorts against a non-map root to fail")
	}
	for _, want := range []string{"$.aa.list", "could not be found in the datastructure"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error containing %q, got %q", want, err.Error())
		}
	}
}
