package sortkeys_test

import (
	"fmt"

	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

func ExampleMode() {
	keys := []string{"name", "Zone", "2", "10", "_id"}
	for _, m := range []sortkeys.Mode{sortkeys.None, sortkeys.Lexical, sortkeys.Spruce} {
		fmt.Println(m, sortkeys.Keys(keys, m))
	}
	// Output:
	// none [name Zone 2 10 _id]
	// lexical [10 2 Zone _id name]
	// spruce [2 10 _id Zone name]
}

func ExampleParse() {
	m, err := sortkeys.Parse("lexical")
	fmt.Println(m, err)
	_, err = sortkeys.Parse("random")
	fmt.Println(err)
	// Output:
	// lexical <nil>
	// unknown sort mode "random" (none, lexical, or spruce)
}

func ExampleKeys() {
	fmt.Println(sortkeys.Keys([]string{"b", "a", "c"}, sortkeys.Lexical))
	// Output: [a b c]
}

func ExampleLess() {
	less := sortkeys.Less(sortkeys.Lexical)
	fmt.Println(less("a", "b"), less("b", "a"), sortkeys.Less(sortkeys.None) == nil)
	// Output: true false true
}

func ExampleSort() {
	v := value.Obj(
		value.Pair{Key: "b", Value: value.Obj(value.Pair{Key: "y", Value: value.Num(1)}, value.Pair{Key: "x", Value: value.Num(2)})},
		value.Pair{Key: "a", Value: value.Num(3)},
	)
	sorted := sortkeys.Sort(v, sortkeys.Lexical)
	b, _ := sorted.Get("b")
	fmt.Println(sorted.Keys(), b.Keys(), v.Keys())
	// Output: [a b] [x y] [b a]
}
