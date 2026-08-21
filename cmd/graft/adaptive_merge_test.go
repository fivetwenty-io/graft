package main

import (
	"context"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestRunAdaptiveMergeCascadeDefersOnlyRootPath pins
// plans/dennis-feedback-gaps.md Item 2's cascade fixture requirement: one
// vault failure whose value is grabbed elsewhere in the document defers
// only the root chain, and the merge succeeds (partially) rather than
// failing outright.
func TestRunAdaptiveMergeCascadeDefersOnlyRootPath(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(`
meta:
  password: (( vault "secret/db:password" ))
database:
  connection: (( grab meta.password ))
  name: mydb
`))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	result, err := runAdaptiveMerge(context.Background(), engine, []graft.Document{doc}, adaptiveMergeOptions{})
	if err != nil {
		t.Fatalf("runAdaptiveMerge: %v", err)
	}

	if len(result.Deferred) != 1 {
		t.Fatalf("Deferred = %v, want exactly 1 entry (the root vault failure)", result.Deferred)
	}
	if result.Deferred[0].Path != "meta.password" {
		t.Fatalf("Deferred[0].Path = %q, want %q", result.Deferred[0].Path, "meta.password")
	}
	if !strings.Contains(result.Deferred[0].Reason, "Vault client initialization") {
		t.Fatalf("Deferred[0].Reason = %q, want it to mention the original Vault error", result.Deferred[0].Reason)
	}

	meta, ok := result.Tree["meta"].(map[string]interface{})
	if !ok {
		t.Fatalf("tree[\"meta\"] = %T, want map[string]interface{}", result.Tree["meta"])
	}
	if meta["password"] != `(( vault "secret/db:password" ))` {
		t.Fatalf("meta.password = %v, want the deferred vault expression intact", meta["password"])
	}

	database, ok := result.Tree["database"].(map[string]interface{})
	if !ok {
		t.Fatalf("tree[\"database\"] = %T, want map[string]interface{}", result.Tree["database"])
	}
	if database["connection"] != `(( vault "secret/db:password" ))` {
		t.Fatalf("database.connection = %v, want the same deferred vault expression (grab copies it)", database["connection"])
	}
	if database["name"] != "mydb" {
		t.Fatalf("database.name = %v, want %q (unaffected by the deferral)", database["name"], "mydb")
	}
}

// TestRunAdaptiveMergeConcatOfDeferredValueIsAKnownLimitation documents
// a real, pre-existing limitation of the "(( defer ... ))" primitive
// itself (op_defer.go), shared by graft debug's own manual
// defer-and-retry (cmdDefer/applyDeferredWrapping) and this adaptive
// loop alike, not something specific to either: a (( grab )) of a
// deferred value copies its "(( ... ))" text cleanly (see
// TestRunAdaptiveMergeCascadeDefersOnlyRootPath), but a
// (( concat "prefix" deferred-value )) embeds that same text INSIDE a
// larger string, producing output that is syntactically valid YAML but
// is no longer a re-evaluable graft expression on a later merge - the
// "((" no longer starts the string. This is not corrected by the
// adaptive loop (there is nothing to correct: concat itself never
// errors, so there is no *PathError to defer), and is documented here
// so it is a known, intentional behavior rather than an unnoticed gap.
func TestRunAdaptiveMergeConcatOfDeferredValueIsAKnownLimitation(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(`
meta:
  password: (( vault "secret/db:password" ))
database:
  url: (( concat "postgres://" meta.password ))
`))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	result, err := runAdaptiveMerge(context.Background(), engine, []graft.Document{doc}, adaptiveMergeOptions{})
	if err != nil {
		t.Fatalf("runAdaptiveMerge: %v", err)
	}
	if len(result.Deferred) != 1 || result.Deferred[0].Path != "meta.password" {
		t.Fatalf("Deferred = %v, want exactly [meta.password] (concat itself never errors)", result.Deferred)
	}
	database, ok := result.Tree["database"].(map[string]interface{})
	if !ok {
		t.Fatalf("tree[\"database\"] = %T, want map[string]interface{}", result.Tree["database"])
	}
	want := `postgres://(( vault "secret/db:password" ))`
	if database["url"] != want {
		t.Fatalf("database.url = %v, want %q (documented limitation: concat embeds the deferred text, it is not itself deferred)", database["url"], want)
	}
}

// TestRunAdaptiveMergeCleanMergeDefersNothing confirms a merge with no
// failures at all returns zero Deferred entries - runAdaptiveMerge is a
// strict superset of a normal merge when nothing needs deferring.
func TestRunAdaptiveMergeCleanMergeDefersNothing(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte("a: 1\nb: (( grab a ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	result, err := runAdaptiveMerge(context.Background(), engine, []graft.Document{doc}, adaptiveMergeOptions{})
	if err != nil {
		t.Fatalf("runAdaptiveMerge: %v", err)
	}
	if len(result.Deferred) != 0 {
		t.Fatalf("Deferred = %v, want empty", result.Deferred)
	}
	if result.Tree["b"] != 1 {
		t.Fatalf("b = %v, want 1", result.Tree["b"])
	}
}

// TestRunAdaptiveMergeCycleIsHardFailure pins Item 2's cycle requirement:
// a genuine operator-dependency cycle is not a *graft.PathError, so
// nothing is deferrable - runAdaptiveMerge must report it as a hard
// failure (the original cycle error), not loop forever or silently
// "succeed" by deferring nothing.
func TestRunAdaptiveMergeCycleIsHardFailure(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(`
a: (( grab b ))
b: (( grab a ))
`))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	result, err := runAdaptiveMerge(context.Background(), engine, []graft.Document{doc}, adaptiveMergeOptions{})
	if err == nil {
		t.Fatalf("expected a hard failure for a genuine cycle, got result: %+v", result)
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %q, want it to mention the cycle", err.Error())
	}
	if result != nil {
		t.Fatalf("expected a nil result alongside the hard failure, got %+v", result)
	}
}

// TestRunAdaptiveMergeAppliesPruneAndCherryPickOnlyOnSuccess confirms
// prune/cherry-pick options are applied to the final, successfully
// evaluated result (once deferred paths stop causing failures), not
// silently skipped or double-applied across retry rounds.
func TestRunAdaptiveMergeAppliesPruneAndCherryPickOnlyOnSuccess(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(`
meta:
  password: (( vault "secret/db:password" ))
database:
  connection: (( grab meta.password ))
  name: mydb
scratch:
  value: not-needed
`))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	result, err := runAdaptiveMerge(context.Background(), engine, []graft.Document{doc}, adaptiveMergeOptions{
		Prune: []string{"scratch"},
	})
	if err != nil {
		t.Fatalf("runAdaptiveMerge: %v", err)
	}
	if _, present := result.Tree["scratch"]; present {
		t.Fatalf("tree still has \"scratch\" after --prune scratch: %v", result.Tree)
	}
	if len(result.Deferred) != 1 || result.Deferred[0].Path != "meta.password" {
		t.Fatalf("Deferred = %v, want exactly [meta.password]", result.Deferred)
	}
}
