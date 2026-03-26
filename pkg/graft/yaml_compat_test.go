package graft

import (
	"testing"
)

func TestDefaultYAMLCompat(t *testing.T) {
	compat := DefaultYAMLCompat()
	if !compat.ConvertYAML11Booleans {
		t.Error("DefaultYAMLCompat should have ConvertYAML11Booleans=true")
	}
}

func TestYAMLCompatConvertValueEnabled(t *testing.T) {
	compat := DefaultYAMLCompat()

	trueVals := []string{"yes", "Yes", "YES", "on", "On", "ON"}
	for _, v := range trueVals {
		result := compat.ConvertValue(v)
		if result != true {
			t.Errorf("ConvertValue(%q) = %v, want true", v, result)
		}
	}

	falseVals := []string{"no", "No", "NO", "off", "Off", "OFF"}
	for _, v := range falseVals {
		result := compat.ConvertValue(v)
		if result != false {
			t.Errorf("ConvertValue(%q) = %v, want false", v, result)
		}
	}

	passthrough := []string{"maybe", "true", "false", "", "hello"}
	for _, v := range passthrough {
		result := compat.ConvertValue(v)
		if result != v {
			t.Errorf("ConvertValue(%q) = %v, want %q", v, result, v)
		}
	}
}

func TestYAMLCompatConvertValueDisabled(t *testing.T) {
	compat := &YAMLCompat{ConvertYAML11Booleans: false}

	vals := []string{"yes", "no", "on", "off", "Yes", "No"}
	for _, v := range vals {
		result := compat.ConvertValue(v)
		if result != v {
			t.Errorf("ConvertValue(%q) with compat disabled = %v, want %q", v, result, v)
		}
	}
}

func TestYAMLCompatConvertMapValues(t *testing.T) {
	compat := DefaultYAMLCompat()

	data := map[string]interface{}{
		"enabled": "yes",
		"disabled": "no",
		"name": "hello",
		"count": 42,
		"nested": map[string]interface{}{
			"flag": "on",
			"value": "world",
		},
		"list": []interface{}{"off", "keep", map[string]interface{}{"inner": "YES"}},
	}

	result := compat.ConvertMapValues(data)

	if result["enabled"] != true {
		t.Errorf("enabled = %v, want true", result["enabled"])
	}
	if result["disabled"] != false {
		t.Errorf("disabled = %v, want false", result["disabled"])
	}
	if result["name"] != "hello" {
		t.Errorf("name = %v, want hello", result["name"])
	}
	if result["count"] != 42 {
		t.Errorf("count = %v, want 42", result["count"])
	}

	nested := result["nested"].(map[string]interface{})
	if nested["flag"] != true {
		t.Errorf("nested.flag = %v, want true", nested["flag"])
	}
	if nested["value"] != "world" {
		t.Errorf("nested.value = %v, want world", nested["value"])
	}

	list := result["list"].([]interface{})
	if list[0] != false {
		t.Errorf("list[0] = %v, want false", list[0])
	}
	if list[1] != "keep" {
		t.Errorf("list[1] = %v, want keep", list[1])
	}
	innerMap := list[2].(map[string]interface{})
	if innerMap["inner"] != true {
		t.Errorf("list[2].inner = %v, want true", innerMap["inner"])
	}
}

func TestYAMLCompatConvertMapValuesDisabled(t *testing.T) {
	compat := &YAMLCompat{ConvertYAML11Booleans: false}

	data := map[string]interface{}{
		"flag": "yes",
	}

	result := compat.ConvertMapValues(data)
	if result["flag"] != "yes" {
		t.Errorf("flag = %v, want 'yes' (disabled)", result["flag"])
	}
}
