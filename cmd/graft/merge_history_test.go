package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/history"
	"github.com/fivetwenty-io/graft/log"
)

// TestMergeHistory locks `graft merge --history`/`--trace-path`/
// `--show-changes`/`--changes-only` (docs/user-guide/history-tracking.md)
// against assets/history/{base,env,secrets}.yml, a fixture chosen to
// exercise every history.Phase: base.yml (LOAD), env.yml overwriting two
// of base.yml's keys and adding a third (MERGE), secrets.yml adding an
// unevaluated `(( grab ... ))` expression that the synthetic evaluation
// step resolves (EVAL), and --prune removing a key post-evaluation (POST).
func TestMergeHistory(t *testing.T) {
	var stdout, stderr string
	printStdOutf = func(format string, args ...interface{}) {
		stdout += fmt.Sprintf(format, args...)
	}
	log.PrintStdErrf = func(format string, args ...interface{}) {
		stderr += fmt.Sprintf(format, args...)
	}

	rc := 256
	exit = func(code int) { rc = code }
	usage = func() {
		stderr = "usage was called"
		exit(1)
	}

	reset := func() {
		stdout = ""
		stderr = ""
		rc = 256
	}

	Convey("graft merge --history/--trace-path/--show-changes/--changes-only", t, func() {
		Convey("--history prints every path's full derivation, ending in Final", func() {
			reset()
			os.Args = []string{"graft", "merge", "--history",
				"../../assets/history/base.yml", "../../assets/history/env.yml", "../../assets/history/secrets.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `Merge History:

database.host:
  [0] ../../assets/history/base.yml → localhost
  [1] ../../assets/history/env.yml → db.prod.example.com
  Final              → db.prod.example.com

database.password:
  [2] ../../assets/history/secrets.yml → (( grab meta.version ))
  [3] <evaluated>    → "1.0"
  Final              → "1.0"

database.pool_size:
  [0] ../../assets/history/base.yml → 10
  [1] ../../assets/history/env.yml → 50
  Final              → 50

database.port:
  [0] ../../assets/history/base.yml → 5432
  Final              → 5432  (unchanged)

meta.version:
  [0] ../../assets/history/base.yml → "1.0"
  Final              → "1.0"  (unchanged)

server.timeout:
  [1] ../../assets/history/env.yml → 60
  Final              → 60  (unchanged)
`)
		})

		Convey("--trace-path reports one path's history with a Type annotation per entry", func() {
			reset()
			os.Args = []string{"graft", "merge", "--trace-path", "database.password",
				"../../assets/history/base.yml", "../../assets/history/env.yml", "../../assets/history/secrets.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `database.password:
  [2] ../../assets/history/secrets.yml → (( grab meta.version ))
      Type: operator (grab)

  [3] <evaluated>    → "1.0"
      Type: value

  Final              → "1.0"
`)
		})

		Convey("--trace-path on a path with no recorded history is an error", func() {
			reset()
			os.Args = []string{"graft", "merge", "--trace-path", "does.not.exist",
				"../../assets/history/base.yml", "../../assets/history/env.yml"}
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, "No history found for path")
			So(stderr, ShouldContainSubstring, "does.not.exist")
			So(rc, ShouldEqual, 2)
		})

		Convey("--show-changes summarizes changed/added/removed paths, omitting untouched ones", func() {
			reset()
			os.Args = []string{"graft", "merge", "--show-changes",
				"../../assets/history/base.yml", "../../assets/history/env.yml", "../../assets/history/secrets.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `Merge Summary: 3 files → 6 keys (3 changed, 1 added, 0 removed)

database.host:
  ✗ ../../assets/history/base.yml localhost
  ✓ ../../assets/history/env.yml db.prod.example.com

database.password:
  ✗ ../../assets/history/secrets.yml (( grab meta.version ))
  ✓ <evaluated>      "1.0"

database.pool_size:
  ✗ ../../assets/history/base.yml 10
  ✓ ../../assets/history/env.yml 50

server.timeout:
  + ../../assets/history/env.yml 60
`)
			So(stdout, ShouldNotContainSubstring, "database.port:")
			So(stdout, ShouldNotContainSubstring, "meta.version:")
		})

		Convey("--show-changes combined with --prune adds a POST removal entry", func() {
			reset()
			os.Args = []string{"graft", "merge", "--show-changes", "--prune", "meta",
				"../../assets/history/base.yml", "../../assets/history/env.yml", "../../assets/history/secrets.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `Merge Summary: 3 files → 6 keys (3 changed, 1 added, 1 removed)

database.host:
  ✗ ../../assets/history/base.yml localhost
  ✓ ../../assets/history/env.yml db.prod.example.com

database.password:
  ✗ ../../assets/history/secrets.yml (( grab meta.version ))
  ✓ <evaluated>      "1.0"

database.pool_size:
  ✗ ../../assets/history/base.yml 10
  ✓ ../../assets/history/env.yml 50

meta.version:
  ✗ ../../assets/history/base.yml "1.0"
  - <pruned>

server.timeout:
  + ../../assets/history/env.yml 60
`)
		})

		Convey("--show-changes under -m counts documents, not CLI file arguments, in its header", func() {
			reset()
			os.Args = []string{"graft", "merge", "-m", "--show-changes", "../../assets/history/multi.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			// One file argument, two "---"-separated documents inside it:
			// the header must say "2 files" (documents merged), not "1
			// file" (CLI arguments given).
			So(stdout, ShouldStartWith, "Merge Summary: 2 files")
		})

		Convey("--changes-only lists one line per changed path with old and new values", func() {
			reset()
			os.Args = []string{"graft", "merge", "--changes-only",
				"../../assets/history/base.yml", "../../assets/history/env.yml", "../../assets/history/secrets.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `Changed paths (4 paths of 6):
  database.host        localhost → db.prod.example.com
  database.password    <none> → "1.0"
  database.pool_size   10 → 50
  server.timeout       <none> → 60
`)
		})

		Convey("combining more than one history flag is a usage error", func() {
			reset()
			os.Args = []string{"graft", "merge", "--history", "--show-changes",
				"../../assets/history/base.yml", "../../assets/history/env.yml"}
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, "mutually exclusive")
			So(rc, ShouldEqual, 1)
		})

		Convey("a plain merge (no history flags) is completely unaffected", func() {
			reset()
			os.Args = []string{"graft", "merge",
				"../../assets/history/base.yml", "../../assets/history/env.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `---
database:
  host: db.prod.example.com
  pool_size: 50
  port: 5432
meta:
  version: "1.0"
server:
  timeout: 60

`)
		})

		Convey("--history on an operator (( prune )) shows the file where the prune arrived and Final → <pruned>", func() {
			reset()
			os.Args = []string{"graft", "merge", "--history",
				"../../assets/history/prune-marker.yml", "../../assets/history/prune-override.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `Merge History:

database.host:
  [0] ../../assets/history/prune-marker.yml → localhost
  [1] ../../assets/history/prune-override.yml → db.prod.example.com
  Final              → db.prod.example.com

secret:
  [0] ../../assets/history/prune-marker.yml → (( prune ))
  [2] <pruned>       → <pruned>
  Final              → <pruned>
`)
		})

		Convey("--show-changes on an operator (( prune )) counts a removal, with no --prune/--cherry-pick flag given", func() {
			reset()
			os.Args = []string{"graft", "merge", "--show-changes",
				"../../assets/history/prune-marker.yml", "../../assets/history/prune-override.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `Merge Summary: 2 files → 2 keys (1 changed, 0 added, 1 removed)

database.host:
  ✗ ../../assets/history/prune-marker.yml localhost
  ✓ ../../assets/history/prune-override.yml db.prod.example.com

secret:
  ✗ ../../assets/history/prune-marker.yml (( prune ))
  - <pruned>
`)
		})

		Convey("--trace-path on an operator (( prune )) marks the removal entry Type: removed", func() {
			reset()
			os.Args = []string{"graft", "merge", "--trace-path", "secret",
				"../../assets/history/prune-marker.yml", "../../assets/history/prune-override.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `secret:
  [0] ../../assets/history/prune-marker.yml → (( prune ))
      Type: operator (prune)

  [2] <pruned>       → <pruned>
      Type: removed

  Final              → <pruned>
`)
		})

		Convey("a key overridden to an explicit YAML null is never rendered as <pruned>", func() {
			// The overlay's unrelated (( append )) array marker (on
			// "tags") routes the whole document through the legacy merger
			// (pkg/graft/merger), which preserves an explicit null overlay
			// value as-is. The sibling case below exercises the same null
			// override on the engine's simple-merge fast path (two plain
			// files, no markers), which preserves it identically.
			reset()
			os.Args = []string{"graft", "merge", "--history",
				"../../assets/history/null-base.yml", "../../assets/history/null-override.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `Merge History:

database.pool_size:
  [0] ../../assets/history/null-base.yml → 10
  [1] ../../assets/history/null-override.yml → ~
  Final              → ~

tags:
  [0] ../../assets/history/null-base.yml → - a …
  Final              → - a …  (unchanged)

tags[0]:
  [1] ../../assets/history/null-override.yml → c
  Final              → c  (unchanged)
`)
			So(stdout, ShouldNotContainSubstring, "<pruned>")
		})

		Convey("a null override on the simple-merge fast path keeps the key, crediting the overlay file", func() {
			// No array/prune/sort markers anywhere, so the merge routes
			// through the engine's simple-merge fast path rather than the
			// legacy merger — the combination that once deleted the key
			// outright, leaving --history to render <pruned> with the
			// source file lost.
			reset()
			os.Args = []string{"graft", "merge", "--history",
				"../../assets/history/null-base.yml", "../../assets/history/null-fast-override.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `Merge History:

database.pool_size:
  [0] ../../assets/history/null-base.yml → 10
  [1] ../../assets/history/null-fast-override.yml → ~
  Final              → ~

tags:
  [0] ../../assets/history/null-base.yml → - a …
  Final              → - a …  (unchanged)
`)
			So(stdout, ShouldNotContainSubstring, "<pruned>")
		})

		Convey("--show-changes counts a fast-path null override as a change, not a removal", func() {
			reset()
			os.Args = []string{"graft", "merge", "--show-changes",
				"../../assets/history/null-base.yml", "../../assets/history/null-fast-override.yml"}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldEqual, `Merge Summary: 2 files → 2 keys (1 changed, 0 added, 0 removed)

database.pool_size:
  ✗ ../../assets/history/null-base.yml 10
  ✓ ../../assets/history/null-fast-override.yml ~
`)
			So(stdout, ShouldNotContainSubstring, "<pruned>")
		})

		Convey("--history on a merge error reports the error and exits 2, printing no report", func() {
			reset()
			os.Args = []string{"graft", "merge", "--history",
				"../../assets/history/base.yml", "../../assets/json/malformed.yml"}
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldNotEqual, "")
			So(rc, ShouldEqual, 2)
		})
	})
}

// TestMergeHistoryPostPhaseRenderingOnlyLabelsGenuineRemovals locks the
// renderer functions directly (writeHistoryEntryLine, writeHistoryFinalLine,
// renderShowChanges) against a hand-built history.PathHistory whose PhasePost
// step holds a sibling entry that is NOT Removed (e.g. a parent map/list
// that merely shrank when a --prune/--cherry-pick flag removed one of its
// other children, or was otherwise touched but not removed at that exact
// path) alongside one that IS. Before the fix, these renderers keyed off
// e.Phase == history.PhasePost alone, so every entry recorded at that step -
// removed or not - printed "<pruned>", corrupting the "merely shrank" case's
// real value.
func TestMergeHistoryPostPhaseRenderingOnlyLabelsGenuineRemovals(t *testing.T) {
	Convey("a PhasePost entry that is not Removed prints its real value, not <pruned>", t, func() {
		// Both entries' Source is realistically "<pruned>" here: that is
		// the step LABEL buildMergeHistorySteps gives the synthetic
		// --prune/--cherry-pick post-processing step, regardless of
		// whether any given path was actually removed by it - only
		// Removed (and, downstream, the rendered VALUE) tells the two
		// apart. "tags" here represents a list that merely shrank (one
		// element pruned by index): it is present in the step's own Data
		// with a different value, not absent from it.
		survived := history.PathHistory{
			Path: "tags",
			Entries: []history.Entry{
				{Index: 0, Source: "base.yml", Phase: history.PhaseLoad, Value: []interface{}{"a", "b", "c"}},
				{Index: 1, Source: "<pruned>", Phase: history.PhasePost, Value: []interface{}{"a", "c"}, Removed: false},
			},
			Final:   []interface{}{"a", "c"},
			FinalOK: true,
		}
		removed := history.PathHistory{
			Path: "secret",
			Entries: []history.Entry{
				{Index: 0, Source: "base.yml", Phase: history.PhaseLoad, Value: "shh"},
				{Index: 1, Source: "<pruned>", Phase: history.PhasePost, Value: nil, Removed: true},
			},
			FinalOK: false,
		}

		var buf strings.Builder
		for _, e := range survived.Entries {
			writeHistoryEntryLine(&buf, e)
		}
		writeHistoryFinalLine(&buf, survived, "Final", false)
		So(buf.String(), ShouldNotContainSubstring, "→ <pruned>")
		So(buf.String(), ShouldContainSubstring, "- a …")

		buf.Reset()
		for _, e := range removed.Entries {
			writeHistoryEntryLine(&buf, e)
		}
		writeHistoryFinalLine(&buf, removed, "Final", false)
		So(buf.String(), ShouldContainSubstring, "→ <pruned>")

		out := renderShowChanges([]history.PathHistory{survived, removed}, 1)
		So(out, ShouldContainSubstring, "tags:\n")
		tagsStart := strings.Index(out, "tags:\n")
		secretStart := strings.Index(out, "secret:\n")
		So(tagsStart, ShouldBeGreaterThanOrEqualTo, 0)
		So(secretStart, ShouldBeGreaterThanOrEqualTo, 0)
		tagsSection := out[tagsStart:secretStart]
		So(tagsSection, ShouldNotContainSubstring, "- <pruned>")
		So(tagsSection, ShouldContainSubstring, "- a …")

		secretSection := out[secretStart:]
		So(secretSection, ShouldContainSubstring, "- <pruned>\n")
	})
}
