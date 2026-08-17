package operators_test

import (
	"context"
	"reflect"
	"sort"
	"strconv"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

func TestShuffleOperator(t *testing.T) {
	Convey("Shuffle Operator", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		Convey("shuffles list elements", func() {
			config := []byte(`
items:
  - 1
  - 2
  - 3
  - 4
  - 5
  - 6
  - 7
  - 8
  - 9
  - 10
result: (( shuffle items ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 10)

			// Verify all elements are preserved
			resultInts := make([]int, len(resultSlice))
			for i, v := range resultSlice {
				resultInts[i], _ = v.(int)
			}
			sort.Ints(resultInts)
			So(resultInts, ShouldResemble, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
		})

		Convey("preserves all elements", func() {
			config := []byte(`
items:
  - "a"
  - "b"
  - "c"
  - "d"
result: (( shuffle items ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 4)

			// Verify all elements are preserved
			resultStrs := make([]string, len(resultSlice))
			for i, v := range resultSlice {
				resultStrs[i], _ = v.(string)
			}
			sort.Strings(resultStrs)
			So(resultStrs, ShouldResemble, []string{"a", "b", "c", "d"})
		})

		Convey("handles empty list", func() {
			config := []byte(`
items: []
result: (( shuffle items ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 0)
		})

		Convey("handles single element list", func() {
			config := []byte(`
items:
  - "only"
result: (( shuffle items ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 1)
			So(resultSlice[0], ShouldEqual, "only")
		})

		Convey("rejects map input", func() {
			config := []byte(`
items:
  key: value
result: (( shuffle items ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			_, err = engine.Evaluate(ctx, doc)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "shuffle only accepts arrays and scalar values")
		})

		Convey("handles scalar values (wraps them)", func() {
			config := []byte(`
a: "hello"
b: "world"
result: (( shuffle a b ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 2)

			// Verify elements are preserved
			resultStrs := make([]string, len(resultSlice))
			for i, v := range resultSlice {
				resultStrs[i], _ = v.(string)
			}
			sort.Strings(resultStrs)
			So(resultStrs, ShouldResemble, []string{"hello", "world"})
		})

		Convey("produces different results (statistical test)", func() {
			// Run shuffle multiple times and verify we get different orderings
			config := []byte(`
items:
  - 1
  - 2
  - 3
  - 4
  - 5
  - 6
  - 7
  - 8
  - 9
  - 10
result: (( shuffle items ))
`)
			orderings := make(map[string]bool)
			for i := 0; i < 20; i++ {
				doc, err := engine.ParseYAML(config)
				So(err, ShouldBeNil)

				ctx := context.Background()
				result, err := engine.Evaluate(ctx, doc)
				So(err, ShouldBeNil)

				resultSlice, err := result.GetSlice("result")
				So(err, ShouldBeNil)

				// Create a string representation of the ordering
				var ordering string
				for _, v := range resultSlice {
					if intVal, ok := v.(int); ok {
						ordering += strconv.Itoa(intVal)
					}
				}
				orderings[ordering] = true
			}
			// With 20 shuffles of 10 elements, we should see more than 1 unique ordering
			// The probability of getting the same ordering 20 times is astronomically low
			So(len(orderings), ShouldBeGreaterThan, 1)
		})
	})
}

func TestCartesianProductOperator(t *testing.T) {
	Convey("Cartesian Product Operator", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		Convey("computes product of two lists", func() {
			config := []byte(`
list1:
  - 1
  - 2
list2:
  - "a"
  - "b"
result: (( cartesian-product list1 list2 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 4)

			// cartesian-product concatenates elements into strings
			// Expected: ["1a", "1b", "2a", "2b"]
			expected := []string{"1a", "1b", "2a", "2b"}
			for i, combo := range resultSlice {
				So(combo, ShouldEqual, expected[i])
			}
		})

		Convey("works with alias cartesian", func() {
			config := []byte(`
list1:
  - "x"
  - "y"
list2:
  - 1
  - 2
result: (( cartesian list1 list2 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 4)
		})

		Convey("computes product of three lists", func() {
			config := []byte(`
list1:
  - 1
  - 2
list2:
  - "a"
list3:
  - "x"
  - "y"
result: (( cartesian-product list1 list2 list3 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			// 2 * 1 * 2 = 4 combinations
			So(len(resultSlice), ShouldEqual, 4)

			// cartesian-product concatenates elements into strings
			// Expected: ["1ax", "1ay", "2ax", "2ay"]
			expected := []string{"1ax", "1ay", "2ax", "2ay"}
			for i, combo := range resultSlice {
				So(combo, ShouldEqual, expected[i])
			}
		})

		Convey("handles single list input", func() {
			config := []byte(`
list1:
  - "a"
  - "b"
  - "c"
result: (( cartesian-product list1 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 3)

			// Each element becomes a string
			expected := []string{"a", "b", "c"}
			for i, combo := range resultSlice {
				So(combo, ShouldEqual, expected[i])
			}
		})

		Convey("handles empty list input", func() {
			config := []byte(`
list1:
  - 1
  - 2
list2: []
result: (( cartesian-product list1 list2 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 0)
		})

		Convey("handles scalar values", func() {
			config := []byte(`
list1:
  - 1
  - 2
scalar: "single"
result: (( cartesian-product list1 scalar ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 2)

			// cartesian-product concatenates into strings
			// Expected: ["1single", "2single"]
			expected := []string{"1single", "2single"}
			for i, combo := range resultSlice {
				So(combo, ShouldEqual, expected[i])
			}
		})

		Convey("rejects map input", func() {
			config := []byte(`
list1:
  - 1
  - 2
map_val:
  key: value
result: (( cartesian-product list1 map_val ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			_, err = engine.Evaluate(ctx, doc)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "cartesian-product operator only accepts arrays and scalar values")
		})

		Convey("rejects nested lists in input", func() {
			config := []byte(`
list1:
  - - 1
    - 2
result: (( cartesian-product list1 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			_, err = engine.Evaluate(ctx, doc)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "cartesian-product operator can only operate on lists of scalar values")
		})

		Convey("rejects maps in input lists", func() {
			config := []byte(`
list1:
  - key: value
result: (( cartesian-product list1 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			_, err = engine.Evaluate(ctx, doc)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "cartesian-product operator can only operate on lists of scalar values")
		})

		Convey("errors with no arguments", func() {
			config := []byte(`
result: (( cartesian-product ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			_, err = engine.Evaluate(ctx, doc)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "no arguments specified")
		})

		Convey("handles large input efficiently", func() {
			config := []byte(`
list1:
  - 1
  - 2
  - 3
  - 4
  - 5
list2:
  - "a"
  - "b"
  - "c"
  - "d"
  - "e"
list3:
  - "x"
  - "y"
  - "z"
result: (( cartesian-product list1 list2 list3 ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			// 5 * 5 * 3 = 75 combinations
			So(len(resultSlice), ShouldEqual, 75)

			// Verify each result is a concatenated string
			for _, combo := range resultSlice {
				comboStr, ok := combo.(string)
				So(ok, ShouldBeTrue)
				So(len(comboStr), ShouldBeGreaterThan, 0)
			}
		})

		Convey("concatenates all types into strings", func() {
			config := []byte(`
ints:
  - 1
  - 2
strings:
  - "a"
  - "b"
bools:
  - true
  - false
result: (( cartesian-product ints strings bools ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			// 2 * 2 * 2 = 8 combinations
			So(len(resultSlice), ShouldEqual, 8)

			// All results should be strings (concatenated)
			// Expected first few: "1atrue", "1afalse", "1btrue", "1bfalse", "2atrue", ...
			for _, combo := range resultSlice {
				So(reflect.TypeOf(combo).Kind(), ShouldEqual, reflect.String)
			}
		})
	})
}

func TestShuffleAndCartesianProductCombined(t *testing.T) {
	Convey("Combined Usage", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		Convey("shuffle can be applied to cartesian product result", func() {
			config := []byte(`
list1:
  - 1
  - 2
list2:
  - "a"
  - "b"
product: (( cartesian-product list1 list2 ))
result: (( shuffle product ))
`)
			doc, err := engine.ParseYAML(config)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			resultSlice, err := result.GetSlice("result")
			So(err, ShouldBeNil)
			So(len(resultSlice), ShouldEqual, 4)

			// Verify all combinations are present (order may differ due to shuffle)
			// cartesian-product returns concatenated strings: "1a", "1b", "2a", "2b"
			combinations := make(map[string]bool)
			for _, combo := range resultSlice {
				comboStr, _ := combo.(string)
				combinations[comboStr] = true
			}
			So(combinations["1a"], ShouldBeTrue)
			So(combinations["1b"], ShouldBeTrue)
			So(combinations["2a"], ShouldBeTrue)
			So(combinations["2b"], ShouldBeTrue)
		})
	})
}
