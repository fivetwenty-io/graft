package keyorder_test

import (
	"fmt"

	"github.com/fivetwenty-io/graft/internal/keyorder"
)

func ExampleSpruceLess() {
	fmt.Println(keyorder.SpruceLess("2", "10"), keyorder.SpruceLess("10", "name"), keyorder.SpruceLess("name", "Zone"))
	// Output: true true false
}
