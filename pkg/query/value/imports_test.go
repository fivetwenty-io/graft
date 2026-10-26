package value

import (
	"go/build"
	"strings"
	"testing"
)

func TestImportsOnlyTheStandardLibraryDecimalAndKeyorder(t *testing.T) {
	p, err := build.Import("github.com/fivetwenty-io/graft/pkg/query/value", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range p.Imports {
		if strings.Contains(imp, ".") && imp != "github.com/fivetwenty-io/graft/pkg/decimal" && imp != "github.com/fivetwenty-io/graft/pkg/keyorder" {
			t.Errorf("pkg/query/value imports %s", imp)
		}
	}
}
