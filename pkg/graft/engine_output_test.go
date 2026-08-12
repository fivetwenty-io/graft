package graft

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEngineToYAML_EvaluatesThenSerializes is the F3 regression guard.
// Engine.ToYAML(doc) previously always returned "not implemented" despite
// engine.md documenting it as real. Document.ToJSONIndent's doc comment
// (api.go) already specifies the intended contract: Engine.ToYAML/ToJSON/
// ToJSONIndent evaluate doc's operators first (unlike the Document-level
// methods, which serialize the document as-is), then serialize the
// evaluated result. This proves the operator actually gets resolved, not
// just that a byte slice comes back.
func TestEngineToYAML_EvaluatesThenSerializes(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte("name: base\nfull: (( concat \"hello \" name ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	out, err := engine.ToYAML(doc)
	if err != nil {
		t.Fatalf("ToYAML failed: %v", err)
	}

	got := string(out)
	if strings.Contains(got, "(( concat") {
		t.Fatalf("ToYAML output still contains the unevaluated operator: %q", got)
	}
	if !strings.Contains(got, "hello base") {
		t.Fatalf("ToYAML output = %q, want it to contain the evaluated value %q", got, "hello base")
	}
}

// TestEngineToJSON_EvaluatesThenSerializes mirrors
// TestEngineToYAML_EvaluatesThenSerializes for ToJSON, additionally
// checking the output is valid, compact JSON.
func TestEngineToJSON_EvaluatesThenSerializes(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte("name: base\nfull: (( concat \"hello \" name ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	out, err := engine.ToJSON(doc)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("ToJSON output is not valid JSON: %v (output: %q)", err, out)
	}
	if decoded["full"] != "hello base" {
		t.Fatalf("ToJSON output[\"full\"] = %#v, want %q", decoded["full"], "hello base")
	}
}

// TestEngineToJSONIndent_EvaluatesThenSerializes mirrors
// TestEngineToYAML_EvaluatesThenSerializes for ToJSONIndent, additionally
// checking the requested indentation is honored.
func TestEngineToJSONIndent_EvaluatesThenSerializes(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte("name: base\nfull: (( concat \"hello \" name ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	out, err := engine.ToJSONIndent(doc, "  ")
	if err != nil {
		t.Fatalf("ToJSONIndent failed: %v", err)
	}

	got := string(out)
	if !strings.Contains(got, "\n  \"full\"") {
		t.Fatalf("ToJSONIndent output does not use the requested 2-space indent: %q", got)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("ToJSONIndent output is not valid JSON: %v (output: %q)", err, out)
	}
	if decoded["full"] != "hello base" {
		t.Fatalf("ToJSONIndent output[\"full\"] = %#v, want %q", decoded["full"], "hello base")
	}
}

// TestEngineOutputMethods_NilDocumentReturnsError proves ToYAML/ToJSON/
// ToJSONIndent return a clear error for a nil Document instead of
// panicking through Evaluate's doc.RawData() call.
func TestEngineOutputMethods_NilDocumentReturnsError(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if _, err := engine.ToYAML(nil); err == nil {
		t.Fatal("ToYAML(nil) succeeded, want an error")
	}
	if _, err := engine.ToJSON(nil); err == nil {
		t.Fatal("ToJSON(nil) succeeded, want an error")
	}
	if _, err := engine.ToJSONIndent(nil, "  "); err == nil {
		t.Fatal("ToJSONIndent(nil, \"  \") succeeded, want an error")
	}
}

// TestEngineToYAML_MutatesCallerDocumentInPlace is the F15 regression
// guard: Engine.ToYAML/ToJSON/ToJSONIndent call Evaluate, which resolves
// operators in place (pre-existing Evaluate behavior, see its own doc
// comment), so a name that reads as a non-mutating serialization call
// actually mutates the Document argument. This pins that behavior (so a
// future accidental fix - e.g. switching to doc.Clone() internally -
// doesn't silently change the documented contract without the docs and
// this test being updated together), and proves the documented
// workaround (doc.Clone() before calling) actually avoids it.
func TestEngineToYAML_MutatesCallerDocumentInPlace(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte("name: base\nfull: (( concat \"hello \" name ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	before, err := doc.ToYAML()
	if err != nil {
		t.Fatalf("doc.ToYAML() before: %v", err)
	}
	if !strings.Contains(string(before), "(( concat") {
		t.Fatalf("doc.ToYAML() before ToYAML = %q, want it to still contain the unevaluated operator", before)
	}

	if _, err := engine.ToYAML(doc); err != nil {
		t.Fatalf("engine.ToYAML(doc): %v", err)
	}

	after, err := doc.ToYAML()
	if err != nil {
		t.Fatalf("doc.ToYAML() after: %v", err)
	}
	if strings.Contains(string(after), "(( concat") {
		t.Fatalf("doc.ToYAML() after engine.ToYAML(doc) = %q, still contains the unevaluated operator - if Evaluate stopped mutating in place, update this test and the F15 doc comments together", after)
	}
	if !strings.Contains(string(after), "hello base") {
		t.Fatalf("doc.ToYAML() after engine.ToYAML(doc) = %q, want it to contain the evaluated value", after)
	}
}

// TestEngineToYAML_CloneAvoidsMutatingCallerDocument proves the
// documented workaround for F15 (pass doc.Clone() instead of doc) leaves
// the original Document unevaluated.
func TestEngineToYAML_CloneAvoidsMutatingCallerDocument(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte("name: base\nfull: (( concat \"hello \" name ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	if _, err := engine.ToYAML(doc.Clone()); err != nil {
		t.Fatalf("engine.ToYAML(doc.Clone()): %v", err)
	}

	after, err := doc.ToYAML()
	if err != nil {
		t.Fatalf("doc.ToYAML() after: %v", err)
	}
	if !strings.Contains(string(after), "(( concat") {
		t.Fatalf("doc.ToYAML() after engine.ToYAML(doc.Clone()) = %q, want the original doc still unevaluated - doc.Clone() should isolate it", after)
	}
}

// TestEngineOutputMethods_PropagateEvaluationErrors proves a document
// whose operators fail to evaluate (an operator referencing a path that
// does not exist) surfaces that failure through ToYAML/ToJSON/
// ToJSONIndent rather than silently serializing the unevaluated tree or
// swallowing the error.
func TestEngineOutputMethods_PropagateEvaluationErrors(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte("full: (( grab does.not.exist ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	if _, err := engine.ToYAML(doc); err == nil {
		t.Fatal("ToYAML with an unresolvable operator succeeded, want an error")
	}
	if _, err := engine.ToJSON(doc); err == nil {
		t.Fatal("ToJSON with an unresolvable operator succeeded, want an error")
	}
	if _, err := engine.ToJSONIndent(doc, "  "); err == nil {
		t.Fatal("ToJSONIndent with an unresolvable operator succeeded, want an error")
	}
}
