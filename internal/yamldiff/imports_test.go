package yamldiff

import (
	"go/build"
	"strings"
	"testing"
)

// TestCoreReadsNoFilesOrNetwork keeps the diff core free of the
// loaders, so pkg/yaml can import it without net/http or TOML.
func TestCoreReadsNoFilesOrNetwork(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		if imp == "net/http" || imp == "net/url" || strings.HasPrefix(imp, "github.com/BurntSushi/toml") {
			t.Errorf("internal/yamldiff imports %s", imp)
		}
	}
}
