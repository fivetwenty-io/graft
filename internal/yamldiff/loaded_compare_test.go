package yamldiff_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamldiff"
	"github.com/fivetwenty-io/graft/internal/yamldiff/load"
	"github.com/fivetwenty-io/graft/internal/yamlgolden"
)

func TestCompareLoadedFilesMatchesDyffGolden(t *testing.T) {
	yamlgolden.PinLocal(t)
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range yamldiff.CompareCases(t) {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(pkgDir, "testdata", "compare", name)
			from, to := filepath.Base(yamlgolden.OnlyMatch(t, dir, "from")), filepath.Base(yamlgolden.OnlyMatch(t, dir, "to"))
			t.Chdir(dir)
			f, g, err := load.Files(from, to)
			if err != nil {
				t.Fatalf("load.Files: %v", err)
			}
			report, err := yamldiff.CompareInputFiles(f, g)
			t.Chdir(pkgDir)
			yamldiff.CheckCompareGolden(t, name, report, err)
		})
	}
}
