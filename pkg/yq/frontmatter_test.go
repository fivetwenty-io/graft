package yq

import (
	"strings"
	"testing"
)

func TestSplitFrontMatter(t *testing.T) {
	y, rest, found, err := SplitFrontMatter(strings.NewReader("---\ntitle: x\n---\n# body\n"))
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, string(y), "---\ntitle: x\n")
	wantEqual(t, string(rest), "---\n# body\n")
	wantEqual(t, found, true)
}
