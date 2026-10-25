package regex

import (
	"go/build"
	"testing"
)

// TestImportsOnlyTheStandardLibrary pins the promise that a project can
// import pkg/regex without compiling anything else from graft or from a
// third party.
func TestImportsOnlyTheStandardLibrary(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	wantNil(t, err)
	imports := append(append(append([]string{}, pkg.Imports...), pkg.TestImports...), pkg.XTestImports...)
	for _, path := range imports {
		if path == "github.com/fivetwenty-io/graft/pkg/regex" {
			continue // the external examples import the package itself
		}
		if path == "regexp" {
			t.Errorf("pkg/regex imports regexp")
			continue
		}
		dep, err := build.Import(path, "", build.FindOnly)
		if err != nil {
			t.Errorf("resolving %s: %v", path, err)
			continue
		}
		if !dep.Goroot {
			t.Errorf("pkg/regex imports %s, which is outside the standard library", path)
		}
	}
}
