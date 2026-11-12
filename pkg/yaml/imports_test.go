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
	"github.com/fivetwenty-io/graft/pkg/yaml":             true, // the external tests import the package itself
	"github.com/fivetwenty-io/graft/pkg/sortkeys":         true,
	"github.com/fivetwenty-io/graft/pkg/decimal":          true, // sortkeys compares numbers with it
	"github.com/fivetwenty-io/graft/pkg/query/value":      true, // sortkeys orders these values
	"github.com/fivetwenty-io/graft/internal/keyorder":    true, // sortkeys' key comparator
	"github.com/fivetwenty-io/graft/internal/cursorpath":  true, // yaml.Document reads graft's cursor paths
	"github.com/fivetwenty-io/graft/internal/literal":     true, // yamlcodec strips literal wrappers before encoding
	"github.com/fivetwenty-io/graft/internal/yamlcodec":   true,
	"github.com/fivetwenty-io/graft/internal/yamlerr":     true, // gives pkg/graft the plain text of a decoding error
	"github.com/fivetwenty-io/graft/internal/yamlnode":    true,
	"github.com/fivetwenty-io/graft/internal/yamldiff":    true, // yaml.Diff runs graft diff's engine
	"github.com/fivetwenty-io/graft/internal/humanreport": true, // DiffReport.Write renders as graft diff does
	"github.com/fivetwenty-io/graft/internal/termstyle":   true, // humanreport styles its output with it
	"github.com/mattn/go-isatty":                          true, // termstyle checks whether output is a terminal
	"golang.org/x/sys/unix":                               true, // isatty and term call it
	"golang.org/x/term":                                   true, // termstyle reads the terminal width
	"github.com/sergi/go-diff/diffmatchpatch":             true, // humanreport marks the changed words of a string
	"github.com/fivetwenty-io/graft/internal/yamlprep":    true,
}

// bannedImports are the packages pkg/yaml exists to replace, plus graft's
// own engine, which would make a program that only wants a YAML reader
// compile all of graft.
var bannedImports = map[string]string{
	"gopkg.in/yaml.v2":                         "pkg/yaml replaces yaml.v2",
	"gopkg.in/yaml.v3":                         "pkg/yaml replaces yaml.v3",
	"go.yaml.in/yaml":                          "pkg/yaml must not depend on the yaml.v3 fork",
	"github.com/fivetwenty-io/graft/pkg/graft": "pkg/yaml must not depend on the merge engine",
	"net/http":                                 "pkg/yaml reads only the bytes it's given",
	"github.com/BurntSushi/toml":               "pkg/yaml reads only the bytes it's given",
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
		if why, banned := bannedReason(path); banned {
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

// bannedReason says why path is banned, matching a banned entry exactly or
// as a path prefix.
func bannedReason(path string) (string, bool) {
	for banned, why := range bannedImports {
		if path == banned || strings.HasPrefix(path, banned+"/") {
			return why, true
		}
	}
	return "", false
}
