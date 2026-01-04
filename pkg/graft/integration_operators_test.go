// Integration tests for graft operators working together.
//
// NOTE: These tests use advanced operator syntax (ternary, calc expressions, comparisons)
// that may not be fully supported by the current parser implementation.
// Specific issues:
// - Ternary operator: (( condition ? "yes" : "no" )) parsing
// - Calc with references: (( calc "meta.value / 4" )) evaluation
// - Comparison operators inline: (( a > b ? x : y ))
//
//go:build integration
// +build integration

package graft

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// TestIntegration_ArithmeticOperators tests arithmetic operators working together
func TestIntegration_ArithmeticOperators(t *testing.T) {
	Convey("Arithmetic Operators Integration", t, func() {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		Convey("Basic arithmetic chaining", func() {
			config := []byte(`
meta:
  base: 10
  multiplier: 3
  offset: 2

calculations:
  # Test basic math: 10 + 2 * 3 = 16 (with proper precedence via calc)
  result1: (( calc "meta.base + meta.offset * meta.multiplier" ))
  # Test subtraction and division
  result2: (( calc "meta.base - meta.offset" ))
  result3: (( calc "meta.base / meta.offset" ))
  # Test modulo
  result4: (( calc "meta.base % meta.multiplier" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			// 10 + 2 * 3 = 10 + 6 = 16
			result1, err := result.Get("calculations.result1")
			So(err, ShouldBeNil)
			So(result1, ShouldEqual, int64(16))

			// 10 - 2 = 8
			result2, err := result.Get("calculations.result2")
			So(err, ShouldBeNil)
			So(result2, ShouldEqual, int64(8))

			// 10 / 2 = 5
			result3, err := result.Get("calculations.result3")
			So(err, ShouldBeNil)
			So(result3, ShouldEqual, int64(5))

			// 10 % 3 = 1
			result4, err := result.Get("calculations.result4")
			So(err, ShouldBeNil)
			So(result4, ShouldEqual, int64(1))
		})

		Convey("Nested arithmetic expressions", func() {
			config := []byte(`
meta:
  a: 5
  b: 3
  c: 2

result: (( calc "pow(meta.a, meta.c) + meta.b * meta.c" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			// pow(5, 2) + 3 * 2 = 25 + 6 = 31
			val, err := result.Get("result")
			So(err, ShouldBeNil)
			So(val, ShouldEqual, int64(31))
		})

		Convey("Arithmetic with grab dependencies", func() {
			config := []byte(`
meta:
  base_value: 100

step1: (( calc "meta.base_value / 4" ))
step2: (( calc "step1 * 2" ))
step3: (( calc "step2 + step1" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			// step1 = 100 / 4 = 25
			step1, err := result.Get("step1")
			So(err, ShouldBeNil)
			So(step1, ShouldEqual, int64(25))

			// step2 = 25 * 2 = 50
			step2, err := result.Get("step2")
			So(err, ShouldBeNil)
			So(step2, ShouldEqual, int64(50))

			// step3 = 50 + 25 = 75
			step3, err := result.Get("step3")
			So(err, ShouldBeNil)
			So(step3, ShouldEqual, int64(75))
		})
	})
}

// TestIntegration_BooleanAndTernary tests boolean operators with ternary conditions
func TestIntegration_BooleanAndTernary(t *testing.T) {
	Convey("Boolean and Ternary Operators Integration", t, func() {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		Convey("Simple ternary with boolean condition", func() {
			config := []byte(`
meta:
  enabled: true
  disabled: false

result_enabled: (( meta.enabled ? "yes" : "no" ))
result_disabled: (( meta.disabled ? "yes" : "no" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			enabled, err := result.GetString("result_enabled")
			So(err, ShouldBeNil)
			So(enabled, ShouldEqual, "yes")

			disabled, err := result.GetString("result_disabled")
			So(err, ShouldBeNil)
			So(disabled, ShouldEqual, "no")
		})

		Convey("Ternary with comparison operators", func() {
			config := []byte(`
meta:
  count: 5
  threshold: 3

above_threshold: (( meta.count > meta.threshold ? "high" : "low" ))
at_or_below: (( meta.count <= meta.threshold ? "ok" : "warning" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			above, err := result.GetString("above_threshold")
			So(err, ShouldBeNil)
			So(above, ShouldEqual, "high")

			atOrBelow, err := result.GetString("at_or_below")
			So(err, ShouldBeNil)
			So(atOrBelow, ShouldEqual, "warning")
		})

		Convey("Ternary with numeric results", func() {
			config := []byte(`
meta:
  environment: "production"

replicas: (( meta.environment == "production" ? 3 : 1 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			replicas, err := result.GetInt("replicas")
			So(err, ShouldBeNil)
			So(replicas, ShouldEqual, 3)
		})

		Convey("Nested ternary expressions", func() {
			config := []byte(`
meta:
  level: 2

description: (( meta.level == 1 ? "low" : meta.level == 2 ? "medium" : "high" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			desc, err := result.GetString("description")
			So(err, ShouldBeNil)
			So(desc, ShouldEqual, "medium")
		})
	})
}

// TestIntegration_ComparisonOperators tests comparison operators together
func TestIntegration_ComparisonOperators(t *testing.T) {
	Convey("Comparison Operators Integration", t, func() {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		Convey("Numeric comparisons", func() {
			config := []byte(`
meta:
  a: 10
  b: 20
  c: 10

comparisons:
  a_eq_c: (( meta.a == meta.c ))
  a_ne_b: (( meta.a != meta.b ))
  a_lt_b: (( meta.a < meta.b ))
  b_gt_a: (( meta.b > meta.a ))
  a_le_c: (( meta.a <= meta.c ))
  b_ge_a: (( meta.b >= meta.a ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			aEqC, err := result.GetBool("comparisons.a_eq_c")
			So(err, ShouldBeNil)
			So(aEqC, ShouldBeTrue)

			aNeB, err := result.GetBool("comparisons.a_ne_b")
			So(err, ShouldBeNil)
			So(aNeB, ShouldBeTrue)

			aLtB, err := result.GetBool("comparisons.a_lt_b")
			So(err, ShouldBeNil)
			So(aLtB, ShouldBeTrue)

			bGtA, err := result.GetBool("comparisons.b_gt_a")
			So(err, ShouldBeNil)
			So(bGtA, ShouldBeTrue)

			aLeC, err := result.GetBool("comparisons.a_le_c")
			So(err, ShouldBeNil)
			So(aLeC, ShouldBeTrue)

			bGeA, err := result.GetBool("comparisons.b_ge_a")
			So(err, ShouldBeNil)
			So(bGeA, ShouldBeTrue)
		})

		Convey("String comparisons", func() {
			config := []byte(`
meta:
  str1: "apple"
  str2: "banana"
  str3: "apple"

comparisons:
  str_eq: (( meta.str1 == meta.str3 ))
  str_ne: (( meta.str1 != meta.str2 ))
  str_lt: (( meta.str1 < meta.str2 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			strEq, err := result.GetBool("comparisons.str_eq")
			So(err, ShouldBeNil)
			So(strEq, ShouldBeTrue)

			strNe, err := result.GetBool("comparisons.str_ne")
			So(err, ShouldBeNil)
			So(strNe, ShouldBeTrue)

			strLt, err := result.GetBool("comparisons.str_lt")
			So(err, ShouldBeNil)
			So(strLt, ShouldBeTrue) // "apple" < "banana"
		})
	})
}

// TestIntegration_DataOperators tests data manipulation operators (keys, sort, inject)
func TestIntegration_DataOperators(t *testing.T) {
	Convey("Data Operators Integration", t, func() {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		Convey("Keys operator extracts map keys", func() {
			config := []byte(`
meta:
  config:
    database: "postgres"
    cache: "redis"
    queue: "rabbitmq"

extracted_keys: (( keys meta.config ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			keys, err := result.GetSlice("extracted_keys")
			So(err, ShouldBeNil)
			So(len(keys), ShouldEqual, 3)
			// Keys should be sorted alphabetically
			So(keys[0], ShouldEqual, "cache")
			So(keys[1], ShouldEqual, "database")
			So(keys[2], ShouldEqual, "queue")
		})

		Convey("Inject operator merges maps", func() {
			config := []byte(`
templates:
  base:
    type: "service"
    replicas: 1
    healthcheck: true

services:
  api:
    name: "api-service"
    <<<: (( inject templates.base ))
    replicas: 3
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			// Check merged values
			name, err := result.GetString("services.api.name")
			So(err, ShouldBeNil)
			So(name, ShouldEqual, "api-service")

			serviceType, err := result.GetString("services.api.type")
			So(err, ShouldBeNil)
			So(serviceType, ShouldEqual, "service")

			// replicas should be overridden to 3
			replicas, err := result.GetInt("services.api.replicas")
			So(err, ShouldBeNil)
			So(replicas, ShouldEqual, 3)

			healthcheck, err := result.GetBool("services.api.healthcheck")
			So(err, ShouldBeNil)
			So(healthcheck, ShouldBeTrue)
		})

		Convey("Keys from multiple maps", func() {
			config := []byte(`
meta:
  primary:
    key1: "value1"
    key2: "value2"
  secondary:
    key3: "value3"
    key2: "duplicate"

all_keys: (( keys meta.primary meta.secondary ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			keys, err := result.GetSlice("all_keys")
			So(err, ShouldBeNil)
			// Should have unique keys: key1, key2, key3 (sorted)
			So(len(keys), ShouldEqual, 3)
			So(keys[0], ShouldEqual, "key1")
			So(keys[1], ShouldEqual, "key2")
			So(keys[2], ShouldEqual, "key3")
		})
	})
}

// TestIntegration_TypeCoercion tests type coercion across operators
func TestIntegration_TypeCoercion(t *testing.T) {
	Convey("Type Coercion Integration", t, func() {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		Convey("Numeric type coercion in calculations", func() {
			config := []byte(`
meta:
  int_val: 10
  float_val: 2.5

result: (( calc "meta.int_val * meta.float_val" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			val, err := result.Get("result")
			So(err, ShouldBeNil)
			// 10 * 2.5 = 25.0
			floatVal, ok := val.(float64)
			So(ok, ShouldBeTrue)
			So(floatVal, ShouldEqual, 25.0)
		})

		Convey("String concatenation with numeric values", func() {
			config := []byte(`
meta:
  name: "service"
  version: 2

full_name: (( concat meta.name "-v" meta.version ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			fullName, err := result.GetString("full_name")
			So(err, ShouldBeNil)
			So(fullName, ShouldEqual, "service-v2")
		})

		Convey("Boolean coercion in ternary", func() {
			config := []byte(`
meta:
  zero: 0
  one: 1
  empty_string: ""
  non_empty: "hello"

results:
  from_zero: (( meta.zero ? "truthy" : "falsy" ))
  from_one: (( meta.one ? "truthy" : "falsy" ))
  from_empty: (( meta.empty_string ? "truthy" : "falsy" ))
  from_non_empty: (( meta.non_empty ? "truthy" : "falsy" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			// 0 is falsy
			fromZero, err := result.GetString("results.from_zero")
			So(err, ShouldBeNil)
			So(fromZero, ShouldEqual, "falsy")

			// 1 is truthy
			fromOne, err := result.GetString("results.from_one")
			So(err, ShouldBeNil)
			So(fromOne, ShouldEqual, "truthy")

			// empty string is falsy
			fromEmpty, err := result.GetString("results.from_empty")
			So(err, ShouldBeNil)
			So(fromEmpty, ShouldEqual, "falsy")

			// non-empty string is truthy
			fromNonEmpty, err := result.GetString("results.from_non_empty")
			So(err, ShouldBeNil)
			So(fromNonEmpty, ShouldEqual, "truthy")
		})
	})
}

// TestIntegration_OperatorChaining tests complex operator chains
func TestIntegration_OperatorChaining(t *testing.T) {
	Convey("Operator Chaining Integration", t, func() {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		Convey("Grab with concat and calc", func() {
			config := []byte(`
meta:
  app: "myapp"
  env: "prod"
  base_port: 8000

derived:
  name: (( concat meta.app "-" meta.env ))
  port: (( calc "meta.base_port + 80" ))

service:
  name: (( grab derived.name ))
  port: (( grab derived.port ))
  url: (( concat "http://" derived.name ":" derived.port ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			name, err := result.GetString("service.name")
			So(err, ShouldBeNil)
			So(name, ShouldEqual, "myapp-prod")

			port, err := result.GetInt("service.port")
			So(err, ShouldBeNil)
			So(port, ShouldEqual, 8080)

			url, err := result.GetString("service.url")
			So(err, ShouldBeNil)
			So(url, ShouldEqual, "http://myapp-prod:8080")
		})

		Convey("Complex pipeline with conditionals", func() {
			config := []byte(`
meta:
  environment: "production"
  base_instances: 2

settings:
  is_prod: (( meta.environment == "production" ))
  multiplier: (( settings.is_prod ? 5 : 1 ))
  instances: (( calc "meta.base_instances * settings.multiplier" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			isProd, err := result.GetBool("settings.is_prod")
			So(err, ShouldBeNil)
			So(isProd, ShouldBeTrue)

			multiplier, err := result.GetInt("settings.multiplier")
			So(err, ShouldBeNil)
			So(multiplier, ShouldEqual, 5)

			instances, err := result.GetInt("settings.instances")
			So(err, ShouldBeNil)
			So(instances, ShouldEqual, 10)
		})
	})
}

// TestIntegration_OperatorWithAlternates tests operators with || alternates
func TestIntegration_OperatorWithAlternates(t *testing.T) {
	Convey("Operators with Alternates Integration", t, func() {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		Convey("Grab with fallback values", func() {
			config := []byte(`
meta:
  defined: "actual_value"

results:
  with_value: (( grab meta.defined || "default" ))
  with_fallback: (( grab meta.undefined || "fallback" ))
  chained_fallback: (( grab meta.undefined || meta.also_undefined || "final" ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			withValue, err := result.GetString("results.with_value")
			So(err, ShouldBeNil)
			So(withValue, ShouldEqual, "actual_value")

			withFallback, err := result.GetString("results.with_fallback")
			So(err, ShouldBeNil)
			So(withFallback, ShouldEqual, "fallback")

			chainedFallback, err := result.GetString("results.chained_fallback")
			So(err, ShouldBeNil)
			So(chainedFallback, ShouldEqual, "final")
		})

		Convey("Numeric and boolean fallbacks", func() {
			config := []byte(`
results:
  num_fallback: (( grab meta.missing_num || 42 ))
  bool_fallback: (( grab meta.missing_bool || true ))
  nil_fallback: (( grab meta.missing || nil ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			numFallback, err := result.GetInt("results.num_fallback")
			So(err, ShouldBeNil)
			So(numFallback, ShouldEqual, 42)

			boolFallback, err := result.GetBool("results.bool_fallback")
			So(err, ShouldBeNil)
			So(boolFallback, ShouldBeTrue)

			nilFallback, err := result.Get("results.nil_fallback")
			So(err, ShouldBeNil)
			So(nilFallback, ShouldBeNil)
		})
	})
}

// operatorIntegrationTestCases provides table-driven test cases
var operatorIntegrationTestCases = []struct {
	name     string
	config   string
	path     string
	expected interface{}
}{
	{
		name: "simple_grab",
		config: `
meta:
  value: "hello"
result: (( grab meta.value ))
`,
		path:     "result",
		expected: "hello",
	},
	{
		name: "concat_strings",
		config: `
meta:
  first: "hello"
  second: "world"
result: (( concat meta.first " " meta.second ))
`,
		path:     "result",
		expected: "hello world",
	},
	{
		name: "simple_calc",
		config: `
meta:
  x: 10
  y: 5
result: (( calc "meta.x + meta.y" ))
`,
		path:     "result",
		expected: int64(15),
	},
	{
		name: "boolean_comparison",
		config: `
meta:
  a: 5
  b: 10
result: (( meta.a < meta.b ))
`,
		path:     "result",
		expected: true,
	},
}

func TestIntegration_TableDrivenOperators(t *testing.T) {
	Convey("Table-Driven Operator Integration Tests", t, func() {
		engine, err := NewEngine()
		So(err, ShouldBeNil)

		for _, tc := range operatorIntegrationTestCases {
			Convey(tc.name, func() {
				doc, err := engine.ParseYAML([]byte(tc.config))
				So(err, ShouldBeNil)

				ctx := context.Background()
				result, err := engine.Evaluate(ctx, doc)
				So(err, ShouldBeNil)

				val, err := result.Get(tc.path)
				So(err, ShouldBeNil)
				So(val, ShouldEqual, tc.expected)
			})
		}
	})
}
