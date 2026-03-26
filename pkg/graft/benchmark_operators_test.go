package graft_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/operators"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// =============================================================================
// Arithmetic Operator Benchmarks
// =============================================================================

func BenchmarkArithmeticOperators(b *testing.B) {
	testCases := []struct {
		name string
		yaml string
	}{
		{
			name: "Addition",
			yaml: `
meta:
  x: 10
  y: 20
result: (( calc meta.x + meta.y ))
`,
		},
		{
			name: "Subtraction",
			yaml: `
meta:
  x: 100
  y: 30
result: (( calc meta.x - meta.y ))
`,
		},
		{
			name: "Multiplication",
			yaml: `
meta:
  x: 7
  y: 8
result: (( calc meta.x * meta.y ))
`,
		},
		{
			name: "Division",
			yaml: `
meta:
  x: 100
  y: 4
result: (( calc meta.x / meta.y ))
`,
		},
		{
			name: "Modulo",
			yaml: `
meta:
  x: 17
  y: 5
result: (( calc meta.x % meta.y ))
`,
		},
		{
			name: "ComplexExpression",
			yaml: `
meta:
  a: 10
  b: 20
  c: 5
  d: 2
result: (( calc (meta.a + meta.b) * meta.c / meta.d ))
`,
		},
		{
			name: "NestedCalculations",
			yaml: `
meta:
  base: 100
level1: (( calc meta.base * 2 ))
level2: (( calc meta.base * 3 ))
level3: (( calc meta.base * 4 ))
total: (( calc meta.base + 200 + 300 + 400 ))
`,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			engine, err := graft.NewEngine()
			if err != nil {
				b.Fatalf("Failed to create engine: %v", err)
			}

			doc, err := engine.ParseYAML([]byte(tc.yaml))
			if err != nil {
				b.Fatalf("Failed to parse YAML: %v", err)
			}

			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := engine.Evaluate(ctx, doc)
				if err != nil {
					b.Fatalf("Evaluate error: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// Comparison Operator Benchmarks
// =============================================================================

func BenchmarkComparisonOperators(b *testing.B) {
	testCases := []struct {
		name string
		yaml string
	}{
		{
			name: "Equality",
			yaml: `
meta:
  x: 10
  y: 10
result: (( calc meta.x == meta.y ))
`,
		},
		{
			name: "NotEqual",
			yaml: `
meta:
  x: 10
  y: 20
result: (( calc meta.x != meta.y ))
`,
		},
		{
			name: "LessThan",
			yaml: `
meta:
  x: 5
  y: 10
result: (( calc meta.x < meta.y ))
`,
		},
		{
			name: "GreaterThan",
			yaml: `
meta:
  x: 15
  y: 10
result: (( calc meta.x > meta.y ))
`,
		},
		{
			name: "LessOrEqual",
			yaml: `
meta:
  x: 10
  y: 10
result: (( calc meta.x <= meta.y ))
`,
		},
		{
			name: "GreaterOrEqual",
			yaml: `
meta:
  x: 10
  y: 10
result: (( calc meta.x >= meta.y ))
`,
		},
		{
			name: "ComplexComparison",
			yaml: `
meta:
  a: 10
  b: 20
  c: 5
result: (( calc meta.a > meta.c && meta.b > meta.a ))
`,
		},
		{
			name: "TernaryOperator",
			yaml: `
meta:
  value: 75
  threshold: 50
result: (( calc meta.value > meta.threshold ? "high" : "low" ))
`,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			engine, err := graft.NewEngine()
			if err != nil {
				b.Fatalf("Failed to create engine: %v", err)
			}

			doc, err := engine.ParseYAML([]byte(tc.yaml))
			if err != nil {
				b.Fatalf("Failed to parse YAML: %v", err)
			}

			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := engine.Evaluate(ctx, doc)
				if err != nil {
					b.Fatalf("Evaluate error: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// String Operation Benchmarks
// =============================================================================

func BenchmarkStringOperations(b *testing.B) {
	b.Run("Concat_Simple", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
meta:
  prefix: "hello"
  suffix: "world"
result: (( concat meta.prefix "-" meta.suffix ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("Concat_Multiple", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
meta:
  a: "one"
  b: "two"
  c: "three"
  d: "four"
  e: "five"
result: (( concat meta.a "-" meta.b "-" meta.c "-" meta.d "-" meta.e ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("Join", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
items:
  - "one"
  - "two"
  - "three"
  - "four"
  - "five"
result: (( join ", " items ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("Split", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
input: "one,two,three,four,five"
result: (( split "," input ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("Concat_LargeStrings", func(b *testing.B) {
		largeString := strings.Repeat("x", 1000)
		yaml := fmt.Sprintf(`
meta:
  a: "%s"
  b: "%s"
result: (( concat meta.a meta.b ))
`, largeString, largeString)

		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(yaml))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})
}

// =============================================================================
// List Operation Benchmarks
// =============================================================================

func BenchmarkListOperations(b *testing.B) {
	b.Run("Keys_SmallMap", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
data:
  key1: value1
  key2: value2
  key3: value3
result: (( keys data ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("Keys_LargeMap", func(b *testing.B) {
		var yamlBuilder strings.Builder
		yamlBuilder.WriteString("data:\n")
		for i := 0; i < 100; i++ {
			yamlBuilder.WriteString(fmt.Sprintf("  key%d: value%d\n", i, i))
		}
		yamlBuilder.WriteString("result: (( keys data ))\n")

		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(yamlBuilder.String()))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("Grab_Simple", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
source:
  nested:
    deep:
      value: "target"
result: (( grab source.nested.deep.value ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("Grab_List", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
list1:
  - a
  - b
  - c
list2:
  - d
  - e
  - f
result: (( grab list1 list2 ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})
}

// =============================================================================
// Complex Expression Benchmarks
// =============================================================================

func BenchmarkComplexExpressions(b *testing.B) {
	b.Run("ChainedGrab", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
meta:
  base: "app"
  env: "prod"
name: (( concat meta.base "-" meta.env ))
config:
  app_name: (( grab name ))
deployment:
  name: (( grab config.app_name ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("NestedOperators", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
meta:
  app: "myapp"
  version: "1.0"
  env: "production"
name: (( concat meta.app "-" meta.version ))
full_name: (( concat name "-" meta.env ))
config:
  app_name: (( grab name ))
  full_name: (( grab full_name ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("ManyOperators", func(b *testing.B) {
		var yamlBuilder strings.Builder
		yamlBuilder.WriteString("meta:\n  base: app\n  version: 1.0\n")

		for i := 0; i < 50; i++ {
			yamlBuilder.WriteString(fmt.Sprintf("value%d: (( concat meta.base \"_%d\" ))\n", i, i))
		}

		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(yamlBuilder.String()))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("MixedOperatorTypes", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
meta:
  app_name: "myapp"
  version: "1.0"
  replicas: 3
  threshold: 80

name: (( concat meta.app_name "-" meta.version ))
config:
  app_name: (( grab name ))
  replicas: (( grab meta.replicas ))
  doubled_replicas: (( calc meta.replicas * 2 ))
  is_scaled: (( calc meta.replicas > 1 ))
  alert_threshold: (( calc meta.threshold + 10 ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})
}

// =============================================================================
// Direct Operator Benchmarks (without full evaluation pipeline)
// =============================================================================

//nolint:dupl // Benchmark test intentionally similar to BenchmarkConcatOperatorMemory for comparison
func BenchmarkDirectOperatorExecution(b *testing.B) {
	b.Run("ConcatOperator_Direct", func(b *testing.B) {
		ev := &graft.Evaluator{
			Tree: map[string]interface{}{
				"name":  "test",
				"value": "data",
			},
			Here: func() *tree.Cursor {
				c, _ := tree.ParseCursor("$")
				return c
			}(),
		}

		args := []*graft.Expr{
			{Type: graft.Literal, Literal: "prefix-"},
			{Type: graft.Reference, Reference: func() *tree.Cursor {
				c, _ := tree.ParseCursor("name")
				return c
			}()},
			{Type: graft.Literal, Literal: "-"},
			{Type: graft.Reference, Reference: func() *tree.Cursor {
				c, _ := tree.ParseCursor("value")
				return c
			}()},
			{Type: graft.Literal, Literal: "-suffix"},
		}

		op := operators.ConcatOperator{}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = op.Run(ev, args)
		}
	})

	b.Run("GrabOperator_Direct", func(b *testing.B) {
		ev := &graft.Evaluator{
			Tree: map[string]interface{}{
				"meta": map[string]interface{}{
					"app":     "myapp",
					"version": "1.0",
				},
			},
			Here: func() *tree.Cursor {
				c, _ := tree.ParseCursor("$")
				return c
			}(),
		}

		args := []*graft.Expr{
			{Type: graft.Reference, Reference: func() *tree.Cursor {
				c, _ := tree.ParseCursor("meta.app")
				return c
			}()},
		}

		op := operators.GrabOperator{}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = op.Run(ev, args)
		}
	})

	b.Run("KeysOperator_Direct", func(b *testing.B) {
		dataMap := make(map[string]interface{})
		for i := 0; i < 50; i++ {
			dataMap[fmt.Sprintf("key%d", i)] = fmt.Sprintf("value%d", i)
		}

		ev := &graft.Evaluator{
			Tree: map[string]interface{}{
				"data": dataMap,
			},
			Here: func() *tree.Cursor {
				c, _ := tree.ParseCursor("$")
				return c
			}(),
		}

		args := []*graft.Expr{
			{Type: graft.Reference, Reference: func() *tree.Cursor {
				c, _ := tree.ParseCursor("data")
				return c
			}()},
		}

		op := operators.KeysOperator{}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = op.Run(ev, args)
		}
	})

	b.Run("JoinOperator_Direct", func(b *testing.B) {
		items := make([]interface{}, 100)
		for i := 0; i < 100; i++ {
			items[i] = fmt.Sprintf("item%d", i)
		}

		ev := &graft.Evaluator{
			Tree: map[string]interface{}{
				"items": items,
			},
			Here: func() *tree.Cursor {
				c, _ := tree.ParseCursor("$")
				return c
			}(),
		}

		args := []*graft.Expr{
			{Type: graft.Literal, Literal: ", "},
			{Type: graft.Reference, Reference: func() *tree.Cursor {
				c, _ := tree.ParseCursor("items")
				return c
			}()},
		}

		op := operators.JoinOperator{}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = op.Run(ev, args)
		}
	})
}

// =============================================================================
// Memory Pool Benchmarks
// =============================================================================

func BenchmarkWithMemoryPools(b *testing.B) {
	b.Run("StringSlice_WithPool", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			s := operators.GetStringSlice()
			*s = append(*s, "one", "two", "three", "four", "five")
			_ = strings.Join(*s, "")
			operators.PutStringSlice(s)
		}
	})

	b.Run("StringSlice_WithoutPool", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			s := make([]string, 0, 10)
			s = append(s, "one", "two", "three", "four", "five")
			_ = strings.Join(s, "")
		}
	})

	b.Run("MapDeepCopy", func(b *testing.B) {
		src := make(map[string]interface{})
		for i := 0; i < 100; i++ {
			src[fmt.Sprintf("key%d", i)] = map[string]interface{}{
				"nested": fmt.Sprintf("value%d", i),
			}
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = graft.DeepCopyMap(src)
		}
	})
}

// =============================================================================
// Operator Resolution Benchmarks
// =============================================================================

func BenchmarkOperatorResolution(b *testing.B) {
	b.Run("ParseOpcall", func(b *testing.B) {
		expressions := []string{
			"(( grab meta.name ))",
			"(( concat \"prefix-\" name ))",
			"(( calc x + y ))",
			"(( join \", \" items ))",
			"(( keys data ))",
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			for _, expr := range expressions {
				_, _ = graft.ParseOpcall(graft.EvalPhase, expr)
			}
		}
	})

	b.Run("OperatorFor", func(b *testing.B) {
		operatorNames := []string{
			"grab", "concat", "calc", "join", "keys", "sort", "prune", "param",
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			for _, name := range operatorNames {
				_ = graft.OperatorFor(name)
			}
		}
	})
}

// =============================================================================
// Concurrent Operator Benchmarks
// =============================================================================

func BenchmarkConcurrentOperatorAccess(b *testing.B) {
	b.Run("ConcurrentParseOpcall", func(b *testing.B) {
		expressions := []string{
			"(( grab meta.name ))",
			"(( concat \"prefix-\" name ))",
			"(( calc x + y ))",
			"(( join \", \" items ))",
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				expr := expressions[i%len(expressions)]
				_, _ = graft.ParseOpcall(graft.EvalPhase, expr)
				i++
			}
		})
	})

	b.Run("ConcurrentOperatorFor", func(b *testing.B) {
		operatorNames := []string{
			"grab", "concat", "calc", "join", "keys",
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				name := operatorNames[i%len(operatorNames)]
				_ = graft.OperatorFor(name)
				i++
			}
		})
	})

	b.Run("ConcurrentEvaluation", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(`
meta:
  app: "myapp"
  version: "1.0"
name: (( concat meta.app "-" meta.version ))
config:
  name: (( grab name ))
`))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})
	})
}

// =============================================================================
// Scaling Benchmarks
// =============================================================================

func BenchmarkOperatorScaling(b *testing.B) {
	operatorCounts := []int{10, 50, 100, 200}

	for _, count := range operatorCounts {
		b.Run(fmt.Sprintf("Operators_%d", count), func(b *testing.B) {
			var yamlBuilder strings.Builder
			yamlBuilder.WriteString("meta:\n  base: app\n  version: 1.0\n")

			for i := 0; i < count; i++ {
				switch i % 4 {
				case 0:
					yamlBuilder.WriteString(fmt.Sprintf("value%d: (( concat meta.base \"_%d\" ))\n", i, i))
				case 1:
					yamlBuilder.WriteString(fmt.Sprintf("value%d: (( grab meta.version ))\n", i))
				case 2:
					yamlBuilder.WriteString(fmt.Sprintf("value%d: (( calc %d + %d ))\n", i, i, i))
				case 3:
					yamlBuilder.WriteString(fmt.Sprintf("value%d: (( calc %d > 50 ? \"large\" : \"small\" ))\n", i, i))
				}
			}

			engine, _ := graft.NewEngine()
			doc, _ := engine.ParseYAML([]byte(yamlBuilder.String()))
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})
	}
}

// =============================================================================
// DataFlow Benchmarks
// =============================================================================

func BenchmarkDataFlow(b *testing.B) {
	b.Run("SimpleDataFlow", func(b *testing.B) {
		ev := &graft.Evaluator{
			Tree: map[string]interface{}{
				"meta": map[string]interface{}{
					"value": "test",
				},
				"result": "(( grab meta.value ))",
			},
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = ev.DataFlow(graft.EvalPhase)
		}
	})

	b.Run("ComplexDataFlow", func(b *testing.B) {
		evalTree := make(map[string]interface{})
		evalTree["meta"] = map[string]interface{}{
			"base": "app",
		}

		for i := 0; i < 20; i++ {
			evalTree[fmt.Sprintf("level1_%d", i)] = fmt.Sprintf("(( concat meta.base \"_%d\" ))", i)
		}

		for i := 0; i < 20; i++ {
			evalTree[fmt.Sprintf("level2_%d", i)] = fmt.Sprintf("(( grab level1_%d ))", i%20)
		}

		ev := &graft.Evaluator{Tree: evalTree}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = ev.DataFlow(graft.EvalPhase)
		}
	})

	b.Run("DeepDependencyChain", func(b *testing.B) {
		evalTree := make(map[string]interface{})
		evalTree["value_0"] = "base"

		for i := 1; i < 50; i++ {
			evalTree[fmt.Sprintf("value_%d", i)] = fmt.Sprintf("(( grab value_%d ))", i-1)
		}

		ev := &graft.Evaluator{Tree: evalTree}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = ev.DataFlow(graft.EvalPhase)
		}
	})
}

// =============================================================================
// Sync Pool Usage Pattern Benchmarks
// =============================================================================

func BenchmarkSyncPoolPatterns(b *testing.B) {
	b.Run("PooledSlice", func(b *testing.B) {
		pool := &sync.Pool{
			New: func() interface{} {
				s := make([]string, 0, 16)
				return &s
			},
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			sp, ok := pool.Get().(*[]string)
			if !ok {
				b.Fatal("expected *[]string from pool")
			}
			s := (*sp)[:0]
			s = append(s, "a", "b", "c", "d", "e")
			_ = strings.Join(s, "-")
			*sp = s
			pool.Put(sp)
		}
	})

	b.Run("AllocatedSlice", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			s := make([]string, 0, 16)
			s = append(s, "a", "b", "c", "d", "e")
			_ = strings.Join(s, "-")
		}
	})

	b.Run("PooledBuffer", func(b *testing.B) {
		pool := &sync.Pool{
			New: func() interface{} {
				return new(strings.Builder)
			},
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			sb, ok := pool.Get().(*strings.Builder)
			if !ok {
				b.Fatal("expected *strings.Builder from pool")
			}
			sb.Reset()
			sb.WriteString("prefix-")
			sb.WriteString("middle-")
			sb.WriteString("suffix")
			_ = sb.String()
			pool.Put(sb)
		}
	})

	b.Run("AllocatedBuffer", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var sb strings.Builder
			sb.WriteString("prefix-")
			sb.WriteString("middle-")
			sb.WriteString("suffix")
			_ = sb.String()
		}
	})
}
