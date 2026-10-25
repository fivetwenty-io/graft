package decimal

import (
	"go/build"
	"strings"
	"testing"
)

func TestImportsOnlyTheStandardLibrary(t *testing.T) {
	p, err := build.Import("github.com/fivetwenty-io/graft/pkg/decimal", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range p.Imports {
		if strings.Contains(imp, ".") {
			t.Errorf("pkg/decimal imports %s", imp)
		}
	}
}
