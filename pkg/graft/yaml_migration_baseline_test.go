package graft

import (
	"encoding/json"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"gopkg.in/yaml.v2"
)

// TestYAMLv2Baseline documents current yaml.v2 behavior before migration.
func TestYAMLv2Baseline(t *testing.T) {
	Convey("YAML v2 Baseline Behavior Documentation", t, func() {
		Convey("Map Type Behavior", func() {
			yamlData := `
name: test
nested:
  key: value
  number: 42
list:
  - item1
  - item2
`
			var result interface{}
			err := yaml.Unmarshal([]byte(yamlData), &result)
			So(err, ShouldBeNil)

			// Document that yaml.v2 returns map[interface{}]interface{} (NOT map[string]interface{})
			_, ok := result.(map[interface{}]interface{})
			So(ok, ShouldBeTrue)
		})

		Convey("Boolean Value Handling", func() {
			testCases := []struct {
				yaml     string
				expected interface{}
				desc     string
			}{
				{`value: true`, true, "YAML 1.2 true"},
				{`value: false`, false, "YAML 1.2 false"},
				{`value: yes`, true, "YAML 1.1 yes"},
				{`value: no`, false, "YAML 1.1 no"},
				{`value: on`, true, "YAML 1.1 on"},
				{`value: off`, false, "YAML 1.1 off"},
				{`value: True`, true, "YAML True"},
				{`value: False`, false, "YAML False"},
			}

			for _, tc := range testCases {
				Convey("Testing "+tc.desc, func() {
					var result map[string]interface{}
					err := yaml.Unmarshal([]byte(tc.yaml), &result)
					So(err, ShouldBeNil)
					So(result["value"], ShouldEqual, tc.expected)
				})
			}
		})

		Convey("JSON Compatibility", func() {
			yamlData := `
name: test
config:
  enabled: true
  count: 42
`
			var yamlResult interface{}
			err := yaml.Unmarshal([]byte(yamlData), &yamlResult)
			So(err, ShouldBeNil)

			// Document that direct JSON marshaling fails with yaml.v2 output
			// because yaml.v2 returns map[interface{}]interface{} which JSON cannot encode
			_, err = json.Marshal(yamlResult)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "unsupported type")
		})

		Convey("Environment Variable Parsing", func() {
			testCases := []struct {
				envValue string
				expected interface{}
				desc     string
			}{
				{`true`, true, "boolean true"},
				{`false`, false, "boolean false"},
				{`null`, nil, "null value"},
				{`[1,2,3]`, []interface{}{1, 2, 3}, "array"},
				{`{"key":"value"}`, map[string]interface{}{"key": "value"}, "object"},
				{`plain string`, "plain string", "plain string"},
			}

			for _, tc := range testCases {
				Convey("Testing env var: "+tc.desc, func() {
					var result interface{}
					err := yaml.Unmarshal([]byte(tc.envValue), &result)
					if err != nil {
						// Plain strings might fail YAML parsing, that's expected
						result = tc.envValue
					}

					switch expected := tc.expected.(type) {
					case map[string]interface{}:
						// yaml.v2 returns map[interface{}]interface{} for objects
						resultMap, ok := result.(map[interface{}]interface{})
						So(ok, ShouldBeTrue)
						for k, v := range expected {
							So(resultMap[k], ShouldEqual, v)
						}
					default:
						So(result, ShouldResemble, tc.expected)
					}
				})
			}
		})

		Convey("Error Handling", func() {
			invalidYAML := `
name: test
  invalid: indentation
`
			var result interface{}
			err := yaml.Unmarshal([]byte(invalidYAML), &result)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "yaml")
		})

		Convey("Multi-Document YAML", func() {
			multiDoc := `---
name: doc1
---
name: doc2
`
			// yaml.v2 behavior with multi-document (parses only first)
			// and returns map[interface{}]interface{}
			var result interface{}
			err := yaml.Unmarshal([]byte(multiDoc), &result)
			So(err, ShouldBeNil)

			resultMap, ok := result.(map[interface{}]interface{})
			So(ok, ShouldBeTrue)
			So(resultMap["name"], ShouldEqual, "doc1")
		})

		Convey("Empty and Null Values", func() {
			testCases := []struct {
				yaml     string
				expected interface{}
				desc     string
			}{
				{`value: `, nil, "empty value"},
				{`value: null`, nil, "explicit null"},
				{`value: ~`, nil, "tilde null"},
				{`value: ""`, "", "empty string"},
			}

			for _, tc := range testCases {
				Convey("Testing "+tc.desc, func() {
					var result map[string]interface{}
					err := yaml.Unmarshal([]byte(tc.yaml), &result)
					So(err, ShouldBeNil)
					So(result["value"], ShouldEqual, tc.expected)
				})
			}
		})
	})
}

// Helper function to document current convertToJSONCompatible behavior.
func TestConvertToJSONCompatibleBaseline(t *testing.T) {
	Convey("convertToJSONCompatible Baseline", t, func() {
		input := map[string]interface{}{
			"string_key":  "value",
			"numeric_key": "123",
			"bool_key":    "true",
			"nil_key":     "<nil>",
		}

		result := convertToJSONCompatible(input)
		resultMap, ok := result.(map[string]interface{})
		So(ok, ShouldBeTrue)

		// Document how string keys are handled
		So(resultMap["string_key"], ShouldEqual, "value")
		So(resultMap["numeric_key"], ShouldEqual, "123")
		So(resultMap["bool_key"], ShouldEqual, "true")
		So(resultMap["nil_key"], ShouldEqual, "<nil>")
	})
}
