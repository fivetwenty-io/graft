package graft

import (
	"reflect"
	"testing"
)

// fusedCompatTree builds a fresh tree exercising every branch both walks
// care about; a fresh copy per use because both walks mutate in place.
func fusedCompatTree() map[string]interface{} {
	return map[string]interface{}{
		"plain":    "value",
		"bare-yes": "yes",
		"bare-off": "Off",
		"tagged":   yaml11QuotedBoolMarker + "yes",
		"taggedOn": yaml11QuotedBoolMarker + "On",
		"number":   int64(42),
		"unsigned": uint64(7),
		"huge":     uint64(1) << 63,
		"floaty":   float32(1.5),
		"nested": map[string]interface{}{
			"deep-no": "no",
			"deep":    yaml11QuotedBoolMarker + "NO",
		},
		"interfaceKeys": map[interface{}]interface{}{
			1: "on",
			2: yaml11QuotedBoolMarker + "off",
		},
		"list": []interface{}{
			"ON", yaml11QuotedBoolMarker + "Yes", int64(3),
			map[string]interface{}{"inner": "off"},
		},
	}
}

// sequentialReference applies the two walks exactly as the parse path
// composed them before fusion.
func sequentialReference(c *YAMLCompat, data map[string]interface{}) map[string]interface{} {
	converted := c.ConvertMapValues(data)
	if unprotected, ok := UnprotectYAML11QuotedBools(converted).(map[string]interface{}); ok {
		converted = unprotected
	}
	return converted
}

func TestConvertAndUnprotectMatchesSequential(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		c := &YAMLCompat{ConvertYAML11Booleans: enabled}
		got := c.ConvertAndUnprotect(fusedCompatTree())
		want := sequentialReference(c, fusedCompatTree())
		if !reflect.DeepEqual(got, want) {
			t.Errorf("compat=%v: fused walk diverges\n got: %#v\nwant: %#v", enabled, got, want)
		}
	}
}
