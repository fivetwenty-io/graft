package graft_test

// A cyclic Go map reaches the merge pipeline through the library API:
// NewDocument(map[string]interface{}) accepts an arbitrary caller-built map
// with no validation, and a caller can build one that contains itself.
// Confirmed the realistic route: Execute() calls hasArrayOperators (and its
// siblings hasArraysWithMaps/hasPruneOperators/hasSortOperators) on every
// document before deepCopyMap ever runs, and those walkers recursed without
// cycle detection too — so a document-level guard in Execute() is required
// in addition to hardening deepCopyMap/deepCopyValue themselves (see
// deep_copy_cycle_test.go), or the crash still happens one step earlier.

import (
	"context"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

func TestMergeCyclicDocument_ReturnsErrorNotStackOverflow(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	cyclic := map[string]interface{}{"x": 1}
	cyclic["self"] = cyclic // a real Go-level cycle, built directly via the library API

	doc1 := graft.NewDocument(cyclic)
	doc2 := graft.NewDocument(map[string]interface{}{"y": 2})

	_, err = engine.Merge(context.Background(), doc1, doc2).Execute()
	if err == nil {
		t.Fatalf("expected an error for a cyclic document, got success")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected a cyclic-reference error, got: %v", err)
	}
}

func TestMergeCyclicDocument_SingleDocumentReturnsErrorNotStackOverflow(t *testing.T) {
	// Execute()'s single-document path runs its own arrays-with-maps /
	// prune / sort pre-checks before mergeDocuments is ever reached, so it
	// needs the same guard exercised independently.
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	cyclic := map[string]interface{}{"x": 1}
	cyclic["self"] = cyclic

	doc := graft.NewDocument(cyclic)

	_, err = engine.Merge(context.Background(), doc).Execute()
	if err == nil {
		t.Fatalf("expected an error for a cyclic document, got success")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected a cyclic-reference error, got: %v", err)
	}
}

func TestMergeCyclicDocument_IndirectCycleThroughListReturnsError(t *testing.T) {
	// The cycle need not be a direct self-reference: a map's list value
	// containing the map itself must also be caught.
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	a := map[string]interface{}{"name": "a"}
	a["children"] = []interface{}{a}

	doc := graft.NewDocument(a)

	_, err = engine.Merge(context.Background(), doc).Execute()
	if err == nil {
		t.Fatalf("expected an error for a list-mediated cycle, got success")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected a cyclic-reference error, got: %v", err)
	}
}

// TestMergeDiamondDocument_MergesCorrectly pins that Execute()'s new cycle
// guard does not misclassify a diamond-shaped (shared-but-acyclic)
// document, which is a legitimate and common shape (the same sub-map
// referenced from two different keys, e.g. two services sharing a config
// block object in memory before being handed to NewDocument).
func TestMergeDiamondDocument_MergesCorrectly(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	shared := map[string]interface{}{"port": 8080}
	root := map[string]interface{}{
		"serviceA": shared,
		"serviceB": shared,
	}

	doc := graft.NewDocument(root)

	result, err := engine.Merge(context.Background(), doc).Execute()
	if err != nil {
		t.Fatalf("unexpected error merging diamond-shaped document: %v", err)
	}

	a, err := result.Get("serviceA.port")
	if err != nil {
		t.Fatalf("failed to read serviceA.port: %v", err)
	}
	b, err := result.Get("serviceB.port")
	if err != nil {
		t.Fatalf("failed to read serviceB.port: %v", err)
	}
	if a != 8080 || b != 8080 {
		t.Fatalf("diamond values not preserved: serviceA=%v serviceB=%v", a, b)
	}
}
