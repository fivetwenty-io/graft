package graft

import (
	"encoding/json"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	yamlv2 "gopkg.in/yaml.v2"
	yamlv3 "gopkg.in/yaml.v3"
)

// TestYAMLv2vsv3Compatibility tests compatibility between yaml.v2 and yaml.v3.
func TestYAMLv2vsv3Compatibility(t *testing.T) {
	Convey("YAML v2 vs v3 Compatibility Analysis", t, func() {
		Convey("Map Type Differences", func() {
			yamlData := `
name: test
nested:
  key: value
  number: 42
`
			var v2Result interface{}
			var v3Result interface{}

			err := yamlv2.Unmarshal([]byte(yamlData), &v2Result)
			So(err, ShouldBeNil)

			err = yamlv3.Unmarshal([]byte(yamlData), &v3Result)
			So(err, ShouldBeNil)

			// v2 returns map[interface{}]interface{} (legacy type)
			_, v2OldOk := v2Result.(map[interface{}]interface{})
			So(v2OldOk, ShouldBeTrue)

			// v3 returns map[string]interface{} (modern type)
			v3Map, v3Ok := v3Result.(map[string]interface{})
			So(v3Ok, ShouldBeTrue)

			// v3 nested values are also map[string]interface{}
			v3Nested, v3NestedOk := v3Map["nested"].(map[string]interface{})
			So(v3NestedOk, ShouldBeTrue)
			So(v3Nested["key"], ShouldEqual, "value")
			So(v3Nested["number"], ShouldEqual, 42)
		})

		Convey("Boolean Value Compatibility", func() {
			testCases := []struct {
				yaml string
				desc string
			}{
				{`value: true`, "YAML 1.2 true"},
				{`value: false`, "YAML 1.2 false"},
				{`value: yes`, "YAML 1.1 yes"},
				{`value: no`, "YAML 1.1 no"},
				{`value: on`, "YAML 1.1 on"},
				{`value: off`, "YAML 1.1 off"},
			}

			for _, tc := range testCases {
				Convey("Testing "+tc.desc, func() {
					var v2Result map[string]interface{}
					var v3Result map[string]interface{}

					err := yamlv2.Unmarshal([]byte(tc.yaml), &v2Result)
					So(err, ShouldBeNil)

					err = yamlv3.Unmarshal([]byte(tc.yaml), &v3Result)
					So(err, ShouldBeNil)

					v2Value := v2Result["value"]
					v3Value := v3Result["value"]

					// Document any differences
					if v2Value != v3Value {
						t.Logf("DIFFERENCE in %s: v2=%v (%T), v3=%v (%T)",
							tc.desc, v2Value, v2Value, v3Value, v3Value)
					}

					// For YAML 1.2, they should be the same
					if tc.yaml == `value: true` || tc.yaml == `value: false` {
						So(v2Value, ShouldEqual, v3Value)
					}
				})
			}
		})

		Convey("JSON Compatibility Improvement", func() {
			yamlData := `
name: test
config:
  enabled: true
  count: 42
`
			var v2Result interface{}
			var v3Result interface{}

			err := yamlv2.Unmarshal([]byte(yamlData), &v2Result)
			So(err, ShouldBeNil)

			err = yamlv3.Unmarshal([]byte(yamlData), &v3Result)
			So(err, ShouldBeNil)

			// v2 should fail direct JSON marshaling
			_, err = json.Marshal(v2Result)
			So(err, ShouldNotBeNil)

			// v3 should succeed with direct JSON marshaling
			jsonBytes, err := json.Marshal(v3Result)
			So(err, ShouldBeNil)
			So(jsonBytes, ShouldNotBeNil)

			// Verify JSON content is valid
			var jsonResult interface{}
			err = json.Unmarshal(jsonBytes, &jsonResult)
			So(err, ShouldBeNil)
		})

		Convey("Type Conversion Compatibility", func() {
			yamlData := `
number: 42
float: 3.14
string: "hello"
boolean: true
null_value: null
array: [1, 2, 3]
`
			var v2Result map[string]interface{}
			var v3Result map[string]interface{}

			err := yamlv2.Unmarshal([]byte(yamlData), &v2Result)
			So(err, ShouldBeNil)

			err = yamlv3.Unmarshal([]byte(yamlData), &v3Result)
			So(err, ShouldBeNil)

			// Test that our conversion function can handle both
			v2Converted := convertToJSONCompatible(v2Result)
			v3Converted := convertToJSONCompatible(v3Result)

			// Both should be map[string]interface{} after conversion
			v2ConvertedMap, ok := v2Converted.(map[string]interface{})
			So(ok, ShouldBeTrue)

			v3ConvertedMap, ok := v3Converted.(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Values should match after conversion
			So(v2ConvertedMap["number"], ShouldEqual, v3ConvertedMap["number"])
			So(v2ConvertedMap["float"], ShouldEqual, v3ConvertedMap["float"])
			So(v2ConvertedMap["string"], ShouldEqual, v3ConvertedMap["string"])
			So(v2ConvertedMap["boolean"], ShouldEqual, v3ConvertedMap["boolean"])
		})

		Convey("Error Handling Compatibility", func() {
			invalidYAML := `
name: test
  invalid: indentation
`
			var v2Result interface{}
			var v3Result interface{}

			v2Err := yamlv2.Unmarshal([]byte(invalidYAML), &v2Result)
			v3Err := yamlv3.Unmarshal([]byte(invalidYAML), &v3Result)

			// Both should return errors
			So(v2Err, ShouldNotBeNil)
			So(v3Err, ShouldNotBeNil)

			// Error messages might differ, but both should indicate YAML issues
			So(v2Err.Error(), ShouldContainSubstring, "yaml")
			So(v3Err.Error(), ShouldContainSubstring, "yaml")
		})

		Convey("Environment Variable Parsing Compatibility", func() {
			testCases := []string{
				`true`,
				`false`,
				`null`,
				`[1,2,3]`,
				`{"key":"value"}`,
				`plain string`,
			}

			for _, envValue := range testCases {
				Convey("Testing env value: "+envValue, func() {
					var v2Result interface{}
					var v3Result interface{}

					v2Err := yamlv2.Unmarshal([]byte(envValue), &v2Result)
					v3Err := yamlv3.Unmarshal([]byte(envValue), &v3Result)

					// Both should either succeed or fail consistently
					if v2Err != nil && v3Err != nil {
						// Both failed - that's fine for invalid YAML
						return
					}

					if v2Err == nil && v3Err == nil {
						// Both succeeded - compare results
						// Note: v2 returns map[interface{}]interface{}, v3 returns map[string]interface{}
						// Only directly compare non-map scalar values
						_, v2IsMap := v2Result.(map[interface{}]interface{})
						_, v3IsMap := v3Result.(map[string]interface{})
						if v2IsMap || v3IsMap {
							// Maps: verify v3 returns the expected string-keyed type
							So(v3IsMap, ShouldBeTrue)
							return
						}

						// For non-map values (scalars, arrays, etc.), they should be identical
						So(v2Result, ShouldEqual, v3Result)
					} else {
						// One succeeded, one failed - document the difference
						t.Logf("PARSING DIFFERENCE for '%s': v2_err=%v, v3_err=%v",
							envValue, v2Err, v3Err)
					}
				})
			}
		})
	})
}

// TestMigrationHelpers tests that our existing helper functions work with both versions.
func TestMigrationHelpers(t *testing.T) {
	Convey("Migration Helper Function Compatibility", t, func() {
		Convey("convertToJSONCompatible works with string-keyed maps", func() {
			// Test with v3 style map (all keys are strings now)
			inputMap := map[string]interface{}{
				"string_key": "value",
				"123":        "numeric_key",
			}

			result := convertToJSONCompatible(inputMap)

			// Should produce map[string]interface{}
			resultMap, ok := result.(map[string]interface{})
			So(ok, ShouldBeTrue)

			So(resultMap["string_key"], ShouldEqual, "value")
			So(resultMap["123"], ShouldEqual, "numeric_key")
		})

		Convey("Document works with v3 maps through NewDocumentFromInterface", func() {
			yamlData := `
name: test
config:
  key: value
`
			// Test what happens when we use yaml.v3 data with current Document
			var v3Result map[string]interface{}
			err := yamlv3.Unmarshal([]byte(yamlData), &v3Result)
			So(err, ShouldBeNil)

			// Create a document with v3 data using the existing helper
			doc, err := NewDocumentFromInterface(v3Result)
			So(err, ShouldBeNil)

			// GetMap should work (it converts to map[string]interface{})
			resultMap, err := doc.GetMap("")
			So(err, ShouldBeNil)
			So(resultMap["name"], ShouldEqual, "test")

			configMap, err := doc.GetMap("config")
			So(err, ShouldBeNil)
			So(configMap["key"], ShouldEqual, "value")
		})
	})
}
