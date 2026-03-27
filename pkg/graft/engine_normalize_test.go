package graft

import (
	"testing"
)

func TestParseYAML_IntegerKeysPreserved(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// YAML with integer keys - should parse without error
	yaml := []byte("top:\n  1: value1\n  name: test\n")
	doc, err := engine.ParseYAML(yaml)
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	data := doc.RawData().(map[string]interface{})
	// The top-level map should have "top" key
	if _, ok := data["top"]; !ok {
		t.Fatal("expected 'top' key in parsed data")
	}
}

func TestParseYAML_AllIntegerKeysAtRoot(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// YAML where all root keys are integers
	yaml := []byte("1: first\n2: second\n")
	doc, err := engine.ParseYAML(yaml)
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	data := doc.RawData().(map[string]interface{})
	if data["1"] != "first" {
		t.Fatalf("expected 'first' for key '1', got %v", data["1"])
	}
	if data["2"] != "second" {
		t.Fatalf("expected 'second' for key '2', got %v", data["2"])
	}
}
