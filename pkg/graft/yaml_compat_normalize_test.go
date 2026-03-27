package graft

import (
	"testing"
)

func TestNormalizeMap_StringKeys(t *testing.T) {
	// Already map[string]interface{} — should pass through unchanged
	input := map[string]interface{}{
		"name":  "test",
		"count": 42,
	}
	result := NormalizeMap(input)
	if result["name"] != "test" {
		t.Fatalf("expected 'test', got %v", result["name"])
	}
}

func TestNormalizeMap_InterfaceKeysNested(t *testing.T) {
	// Nested map[interface{}]interface{} should be converted
	input := map[string]interface{}{
		"top": map[interface{}]interface{}{
			1:      "value1",
			"name": "test",
			2: map[interface{}]interface{}{
				"nested": "deep",
			},
		},
	}
	result := NormalizeMap(input)
	top, ok := result["top"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result["top"])
	}
	if top["1"] != "value1" {
		t.Fatalf("expected '1' key with 'value1', got %v", top["1"])
	}
	nested, ok := top["2"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested map[string]interface{}, got %T", top["2"])
	}
	if nested["nested"] != "deep" {
		t.Fatalf("expected 'deep', got %v", nested["nested"])
	}
}

func TestNormalizeMap_ArraysWithMixedMaps(t *testing.T) {
	input := map[string]interface{}{
		"items": []interface{}{
			map[interface{}]interface{}{
				"key": "value",
			},
			"string-item",
		},
	}
	result := NormalizeMap(input)
	items := result["items"].([]interface{})
	m, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", items[0])
	}
	if m["key"] != "value" {
		t.Fatalf("expected 'value', got %v", m["key"])
	}
}

func TestNormalizeMap_NilInput(t *testing.T) {
	result := NormalizeMap(nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestNormalizeMap_EmptyInput(t *testing.T) {
	result := NormalizeMap(map[string]interface{}{})
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}
}
