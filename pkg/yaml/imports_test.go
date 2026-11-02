package yaml_test

import (
	"go/build"
	"strings"
	"testing"
)

// allowedImports is every import path outside the standard library that
// pkg/yaml, its tests, or anything below them may reach. It is an
// allow-list, so a new dependency fails the test until someone adds it
// here on purpose.
var allowedImports = map[string]bool{
	"github.com/fivetwenty-io/graft/pkg/yaml":           true, // the external tests import the package itself
	"github.com/fivetwenty-io/graft/pkg/sortkeys":       true,
	"github.com/fivetwenty-io/graft/pkg/decimal":        true, // sortkeys compares numbers with it
	"github.com/fivetwenty-io/graft/pkg/query/value":    true, // sortkeys orders these values
	"github.com/fivetwenty-io/graft/internal/keyorder":  true, // sortkeys' key comparator
	"github.com/fivetwenty-io/graft/internal/yamlcodec": true,
	"github.com/fivetwenty-io/graft/internal/yamlnode":  true,
	"github.com/fivetwenty-io/graft/internal/yamlprep":  true,
}

// bannedImports are the packages pkg/yaml exists to replace, plus graft's
// own engine, which would make a program that only wants a YAML reader
// compile all of graft.
var bannedImports = map[string]string{
	"gopkg.in/yaml.v2":                         "pkg/yaml replaces yaml.v2",
	"gopkg.in/yaml.v3":                         "pkg/yaml replaces yaml.v3",
	"go.yaml.in/yaml":                          "pkg/yaml must not depend on the yaml.v3 fork",
	"github.com/fivetwenty-io/graft/pkg/graft": "pkg/yaml must not depend on the merge engine",
}

const goccyPrefix = "github.com/goccy/go-yaml"

// TestImportsStayInsideTheAllowList pins the whole set of non-standard
// packages that pkg/yaml reaches, directly and through what it imports.
func TestImportsStayInsideTheAllowList(t *testing.T) {
	t.Parallel()
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	roots := append(append(append([]string{}, pkg.Imports...), pkg.TestImports...), pkg.XTestImports...)
	seen := map[string]bool{}
	var walk func(path, from string)
	walk = func(path, from string) {
		if seen[path] {
			return
		}
		seen[path] = true
		if why, banned := bannedImports[path]; banned {
			t.Errorf("%s imports %s, which is banned: %s", from, path, why)
			return
		}
		dep, err := build.Import(path, ".", 0)
		if err != nil {
			t.Errorf("resolving %s: %v", path, err)
			return
		}
		if dep.Goroot {
			return
		}
		if !allowedImports[path] && !strings.HasPrefix(path, goccyPrefix) {
			t.Errorf("%s imports %s, which is not in the allow-list", from, path)
			return
		}
		for _, next := range dep.Imports {
			walk(next, path)
		}
	}
	for _, path := range roots {
		walk(path, "pkg/yaml")
	}
}
