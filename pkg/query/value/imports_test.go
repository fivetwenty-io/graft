package value

import (
	"go/build"
	"strings"
	"testing"
)

func TestImportsOnlyTheStandardLibraryAndDecimal(t *testing.T) {
	p, err := build.Import("github.com/fivetwenty-io/graft/pkg/query/value", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range p.Imports {
		if strings.Contains(imp, ".") && imp != "github.com/fivetwenty-io/graft/pkg/decimal" {
			t.Errorf("pkg/query/value imports %s", imp)
		}
	}
}
