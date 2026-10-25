package load

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

func TestHugeLiteralPassesThroughBounded(t *testing.T) {
	start := time.Now()
	huge := strings.Repeat("9", 100001)
	v, _, err := NewJSONReader(bufio.NewReader(strings.NewReader(huge)), "test", LoadOptions{}).Next()
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, v.Literal(), huge)
	wantEqual(t, v.Bounded(), false)
	e, _, err := NewJSONReader(bufio.NewReader(strings.NewReader("1e1000000000")), "test", LoadOptions{}).Next()
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, e.Bounded(), false)
	if el := time.Since(start); el > time.Second {
		t.Fatalf("the pass-through took %v", el)
	}
}
