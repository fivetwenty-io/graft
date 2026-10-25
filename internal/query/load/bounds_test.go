package load

import (
	"bufio"
	"runtime"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestHugeLiteralPassesThroughBounded(t *testing.T) {
	huge := strings.Repeat("9", 100001)
	read := func(s string) *value.Value {
		v, _, err := NewJSONReader(bufio.NewReader(strings.NewReader(s)), "test", LoadOptions{}).Next()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	var v, e *value.Value
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	v = read(huge)
	e = read("1e1000000000")
	runtime.ReadMemStats(&after)
	// The text passes through without building a coefficient, so the
	// allocation stays a small multiple of the text.
	if n := after.TotalAlloc - before.TotalAlloc; n > 4<<20 {
		t.Errorf("reading the literals allocated %d bytes", n)
	}
	wantEqual(t, v.Literal(), huge)
	wantEqual(t, v.Bounded(), false)
	wantEqual(t, e.Bounded(), false)
}
