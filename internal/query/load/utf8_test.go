package load

import (
	"bufio"
	"strings"
	"testing"
)

func TestInvalidUTF8IsReplaced(t *testing.T) {
	v, _, err := NewJSONReader(bufio.NewReader(strings.NewReader("\"a\xffb\"")), "test", LoadOptions{}).Next()
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, v.Str(), "a�b")
}
