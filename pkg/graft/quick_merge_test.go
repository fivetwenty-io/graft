package graft_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

func TestQuickMerge(t *testing.T) {
	Convey("QuickMerge", t, func() {
		Convey("merges YAML sources in order and evaluates operators", func() {
			out, err := graft.QuickMerge(
				"name: myapp\nversion: \"1.0\"\n",
				"version: \"2.0\"\ngreeting: (( concat \"hello \" name ))\n",
			)
			So(err, ShouldBeNil)
			So(string(out), ShouldContainSubstring, "name: myapp")
			So(string(out), ShouldContainSubstring, "version: \"2.0\"")
			So(string(out), ShouldContainSubstring, "greeting: hello myapp")
		})

		Convey("propagates parse errors", func() {
			_, err := graft.QuickMerge("name: myapp\n", ":\t{not yaml")
			So(err, ShouldNotBeNil)
		})

		Convey("propagates evaluation errors", func() {
			_, err := graft.QuickMerge("v: (( grab missing.path ))\n")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "could not be found")
		})

		Convey("with no sources returns an empty document", func() {
			out, err := graft.QuickMerge()
			So(err, ShouldBeNil)
			So(string(out), ShouldEqual, "{}\n")
		})
	})
}

func TestQuickMergeFiles(t *testing.T) {
	Convey("QuickMergeFiles", t, func() {
		dir := t.TempDir()
		base := filepath.Join(dir, "base.yml")
		override := filepath.Join(dir, "override.yml")
		So(os.WriteFile(base, []byte("name: myapp\nreplicas: 1\n"), 0o600), ShouldBeNil)
		So(os.WriteFile(override, []byte("replicas: 3\n"), 0o600), ShouldBeNil)

		Convey("merges files in order", func() {
			out, err := graft.QuickMergeFiles(base, override)
			So(err, ShouldBeNil)
			So(string(out), ShouldContainSubstring, "name: myapp")
			So(string(out), ShouldContainSubstring, "replicas: 3")
		})

		Convey("propagates missing-file errors", func() {
			_, err := graft.QuickMergeFiles(base, filepath.Join(dir, "nope.yml"))
			So(err, ShouldNotBeNil)
		})

		Convey("with no paths returns an empty document", func() {
			out, err := graft.QuickMergeFiles()
			So(err, ShouldBeNil)
			So(string(out), ShouldEqual, "{}\n")
		})
	})
}

// TestQuickMergeReleasesEngineGoroutines pins that the throwaway engine a
// QuickMerge call builds does not outlive the call: the engine's
// ShardedCache starts a background cleanupLoop goroutine, and before
// QuickMerge closed the cache each call leaked one goroutine for the
// process lifetime — fatal for a service calling QuickMerge per request.
func TestQuickMergeReleasesEngineGoroutines(t *testing.T) {
	// Warm-up call, so anything lazily started process-wide on first use
	// is already running before the baseline is taken.
	if _, err := graft.QuickMerge("a: 1\n"); err != nil {
		t.Fatalf("warm-up QuickMerge failed: %v", err)
	}

	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		if _, err := graft.QuickMerge("a: 1\n"); err != nil {
			t.Fatalf("QuickMerge failed: %v", err)
		}
	}

	// Engine cache Close() is synchronous, but give the runtime a few
	// scheduling turns to retire unrelated transients before judging.
	var after int
	for i := 0; i < 50; i++ {
		after = runtime.NumGoroutine()
		if after <= before+2 {
			return
		}
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines leaked across QuickMerge calls: %d before, %d after 20 calls", before, after)
}
