package value_test

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

// FromGo carries its own copy of spruce's key order, because pkg/sortkeys
// imports this package. The two must never disagree.
func TestFromGoKeyOrderAgreesWithSortkeys(t *testing.T) {
	in := map[string]interface{}{}
	keys := []string{"b", "a", "10", "9", "1.5", "01", "int_val", "int64_val", "z1", "z10", "z2", "_x", "-3", "1e3", "Ab", "ab"}
	for _, k := range keys {
		in[k] = nil
	}
	v, err := value.FromGo(in)
	if err != nil {
		t.Fatal(err)
	}
	want := sortkeys.Keys(keys, sortkeys.Spruce)
	got := v.Keys()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FromGo order %v, sortkeys order %v", got, want)
		}
	}
}
