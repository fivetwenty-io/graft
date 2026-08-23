package main

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/history"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

func TestParseTreeArgs(t *testing.T) {
	Convey("parseTreeArgs", t, func() {
		Convey("bare tree parses to the root path with defaults", func() {
			opts, err := parseTreeArgs(nil)
			So(err, ShouldBeNil)
			So(opts.path, ShouldEqual, "")
			So(opts.depth, ShouldEqual, 0)
			So(opts.keysOnly, ShouldBeFalse)
		})

		Convey("path and every flag parse together", func() {
			opts, err := parseTreeArgs([]string{"database", "--depth", "2", "--keys", "--annotate", "--history", "--no-color"})
			So(err, ShouldBeNil)
			So(opts.path, ShouldEqual, "database")
			So(opts.depth, ShouldEqual, 2)
			So(opts.annotate, ShouldBeTrue)
			So(opts.historyList, ShouldBeTrue)
			So(opts.noColor, ShouldBeTrue)
		})

		Convey("short flags parse", func() {
			opts, err := parseTreeArgs([]string{"-d", "3", "-k", "-H", "database"})
			So(err, ShouldBeNil)
			So(opts.depth, ShouldEqual, 3)
			So(opts.keysOnly, ShouldBeTrue)
			So(opts.historyList, ShouldBeTrue)
			So(opts.path, ShouldEqual, "database")
		})

		Convey("--depth=N and -d=N parse", func() {
			opts, err := parseTreeArgs([]string{"--depth=2"})
			So(err, ShouldBeNil)
			So(opts.depth, ShouldEqual, 2)

			opts, err = parseTreeArgs([]string{"-d=4"})
			So(err, ShouldBeNil)
			So(opts.depth, ShouldEqual, 4)
		})

		Convey("--annotate overrides --keys", func() {
			opts, err := parseTreeArgs([]string{"--keys", "--annotate"})
			So(err, ShouldBeNil)
			So(opts.annotate, ShouldBeTrue)
			So(opts.keysOnly, ShouldBeFalse)
		})

		Convey("-h and --help set help", func() {
			opts, err := parseTreeArgs([]string{"-h"})
			So(err, ShouldBeNil)
			So(opts.help, ShouldBeTrue)

			opts, err = parseTreeArgs([]string{"--help"})
			So(err, ShouldBeNil)
			So(opts.help, ShouldBeTrue)
		})

		Convey("a leading $ or $. is stripped from the path", func() {
			opts, err := parseTreeArgs([]string{"$.database.host"})
			So(err, ShouldBeNil)
			So(opts.path, ShouldEqual, "database.host")

			opts, err = parseTreeArgs([]string{"$"})
			So(err, ShouldBeNil)
			So(opts.path, ShouldEqual, "")
		})

		Convey("--history without a path errors", func() {
			_, err := parseTreeArgs([]string{"--history"})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "--history requires a path")
		})

		Convey("--depth without a number errors", func() {
			_, err := parseTreeArgs([]string{"database", "--depth"})
			So(err, ShouldNotBeNil)
		})

		Convey("--depth with a non-number or zero errors", func() {
			_, err := parseTreeArgs([]string{"--depth", "x"})
			So(err, ShouldNotBeNil)

			_, err = parseTreeArgs([]string{"--depth", "0"})
			So(err, ShouldNotBeNil)
		})

		Convey("an unknown flag errors", func() {
			_, err := parseTreeArgs([]string{"--wat"})
			So(err, ShouldNotBeNil)
		})
	})
}

func TestRenderDebugTree(t *testing.T) {
	tree := map[string]interface{}{
		"database": map[string]interface{}{
			"host":      "localhost",
			"port":      5432,
			"pool_size": 10,
		},
		"jobs": []interface{}{
			map[string]interface{}{"name": "web", "instances": 2},
			map[string]interface{}{"name": "worker", "instances": 1},
		},
	}

	Convey("renderDebugTree with color off", t, func() {
		prevColor := ansi.IsColorEnabled()
		ansi.Color(false)
		Reset(func() { ansi.Color(prevColor) })

		Convey("renders sorted keys with inline leaf values", func() {
			out := renderDebugTree(tree["database"], treeOptions{path: "database"}, nil)
			So(out, ShouldEqual, "database\n"+
				"├─ host: localhost\n"+
				"├─ pool_size: 10\n"+
				"└─ port: 5432\n")
		})

		Convey("renders list elements as dim [N] indices", func() {
			out := renderDebugTree(tree["jobs"], treeOptions{path: "jobs"}, nil)
			So(out, ShouldEqual, "jobs\n"+
				"├─ [0]\n"+
				"│  ├─ instances: 2\n"+
				"│  └─ name: web\n"+
				"└─ [1]\n"+
				"   ├─ instances: 1\n"+
				"   └─ name: worker\n")
		})

		Convey("an empty path labels the root as $", func() {
			out := renderDebugTree(tree, treeOptions{}, nil)
			So(out, ShouldStartWith, "$\n")
			So(out, ShouldContainSubstring, "├─ database\n")
			So(out, ShouldContainSubstring, "└─ jobs\n")
		})

		Convey("--keys drops leaf values", func() {
			out := renderDebugTree(tree["database"], treeOptions{path: "database", keysOnly: true}, nil)
			So(out, ShouldContainSubstring, "├─ host\n")
			So(out, ShouldNotContainSubstring, "localhost")
		})

		Convey("--depth collapses containers past the cutoff", func() {
			out := renderDebugTree(tree, treeOptions{depth: 1}, nil)
			So(out, ShouldEqual, "$\n"+
				"├─ database {3 keys}\n"+
				"└─ jobs [2 items]\n")
		})

		Convey("a scalar at the path renders as a single root line", func() {
			out := renderDebugTree("localhost", treeOptions{path: "database.host"}, nil)
			So(out, ShouldEqual, "database.host: localhost\n")
		})

		Convey("annotation entries print in history's line format", func() {
			ann := map[string][]history.Entry{
				"database.host": {
					{Index: 0, Source: "base.yml", Phase: history.PhaseLoad, Value: "localhost"},
					{Index: 1, Source: "env.yml", Phase: history.PhaseMerge, Value: "db.prod.example.com"},
				},
			}
			out := renderDebugTree(tree["database"], treeOptions{path: "database"}, ann)
			So(out, ShouldContainSubstring, "├─ host: localhost\n"+
				fmt.Sprintf("│    %-*s → %s\n", sourceColumnWidth, "[0] base.yml", "localhost")+
				fmt.Sprintf("│    %-*s → %s\n", sourceColumnWidth, "[1] env.yml", "db.prod.example.com"))
		})

		Convey("history below a list never leaks another path's entries", func() {
			collide := map[string]interface{}{
				"name": "top",
				"jobs": []interface{}{
					map[string]interface{}{"name": "web"},
				},
			}
			ann := map[string][]history.Entry{
				"name": {{Index: 0, Source: "base.yml", Phase: history.PhaseLoad, Value: "top"}},
				"jobs": {{Index: 0, Source: "base.yml", Phase: history.PhaseLoad, Value: []interface{}{"x"}}},
			}
			out := renderDebugTree(collide, treeOptions{}, ann)
			// The list's own entry prints under the jobs label...
			So(out, ShouldContainSubstring, "├─ jobs\n│    [0] base.yml")
			// ...and the name leaf inside the list carries nothing: the
			// top-level name's entry appears exactly once (under the real
			// top-level name leaf, not borrowed by jobs.[0].name).
			So(strings.Count(out, fmt.Sprintf("%-*s → top", sourceColumnWidth, "[0] base.yml")), ShouldEqual, 1)
		})

		Convey("a root inside a list never borrows the list's entries", func() {
			ann := map[string][]history.Entry{
				"jobs": {{Index: 0, Source: "base.yml", Phase: history.PhaseLoad, Value: []interface{}{"x"}}},
				"name": {{Index: 0, Source: "base.yml", Phase: history.PhaseLoad, Value: "top"}},
			}
			out := renderDebugTree(map[string]interface{}{"name": "web"}, treeOptions{path: "jobs.[0]"}, ann)
			So(out, ShouldNotContainSubstring, "[0] base.yml")
		})

		Convey("--depth collapse hides annotations beneath the cutoff", func() {
			ann := map[string][]history.Entry{
				"database.host": {{Index: 0, Source: "base.yml", Phase: history.PhaseLoad, Value: "localhost"}},
			}
			out := renderDebugTree(tree, treeOptions{depth: 1}, ann)
			So(out, ShouldContainSubstring, "├─ database {3 keys}\n")
			So(out, ShouldNotContainSubstring, "[0] base.yml")
		})
	})

	Convey("renderDebugTree with color on", t, func() {
		prevColor := ansi.IsColorEnabled()
		ansi.Color(true)
		Reset(func() { ansi.Color(prevColor) })

		Convey("keys are cyan, operators are yellow, indices are dim", func() {
			withOp := map[string]interface{}{
				"password": "(( grab meta.version ))",
				"list":     []interface{}{"x"},
			}
			out := renderDebugTree(withOp, treeOptions{path: "database"}, nil)
			So(out, ShouldContainSubstring, "\033[36mpassword\033[0m")
			So(out, ShouldContainSubstring, "\033[33m(( grab meta.version ))\033[0m")
			So(out, ShouldContainSubstring, "\033[2m[0]\033[0m")
		})
	})
}

// TestTreeUsage locks the usage string Task 3 prints on a parse error
// and for -h/--help; treeUsage has no other reference until that REPL
// wiring lands.
func TestTreeUsage(t *testing.T) {
	Convey("treeUsage documents every flag", t, func() {
		So(treeUsage, ShouldEqual, "tree [path] [--depth|-d N] [--keys|-k] [--annotate|-a] [--history|-H] [--no-color]")
	})
}

// TestDebugTreeCommand drives `tree` through the real REPL against the
// same assets/history fixture the other debug tests use (base.yml then
// env.yml then secrets.yml; secrets.yml holds the unevaluated
// (( grab meta.version )) operator).
func TestDebugTreeCommand(t *testing.T) {
	files := []string{
		"../../assets/history/base.yml",
		"../../assets/history/env.yml",
		"../../assets/history/secrets.yml",
	}
	listFiles := []string{"../../assets/debug/tree-list.yml"}
	collideFiles := []string{"../../assets/debug/tree-collide.yml"}
	pruneFiles := []string{"../../assets/debug/tree-prune.yml"}

	Convey("graft debug tree", t, func() {
		prevColor := ansi.IsColorEnabled()
		ansi.Color(false)
		Reset(func() { ansi.Color(prevColor) })

		Convey("tree before load asks for load first", func() {
			out, rc := runDebugSession(files, "tree\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "No documents loaded. Run 'load' first.\n")
		})

		Convey("tree shows the current tree, reflecting session progress", func() {
			out, rc := runDebugSession(files, "load\ntree database\nstep\ntree database\nquit\n")
			So(rc, ShouldEqual, 0)
			// Step 0: base.yml only.
			So(out, ShouldContainSubstring, "database\n"+
				"├─ host: localhost\n"+
				"├─ pool_size: 10\n"+
				"└─ port: 5432\n")
			// Step 1: env.yml merged.
			So(out, ShouldContainSubstring, "database\n"+
				"├─ host: db.prod.example.com\n"+
				"├─ pool_size: 50\n"+
				"└─ port: 5432\n")
		})

		Convey("bare tree renders the whole document from $", func() {
			out, rc := runDebugSession(files, "load\ntree\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "$\n├─ database\n")
			So(out, ShouldContainSubstring, "└─ meta\n")
		})

		Convey("a leading $. is accepted on paths", func() {
			out, rc := runDebugSession(files, "load\ntree $.database\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "database\n├─ host: localhost\n")
		})

		Convey("tree renders list elements with [N] indices", func() {
			out, rc := runDebugSession(listFiles, "load\ntree jobs\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "jobs\n"+
				"├─ [0]\n"+
				"│  ├─ instances: 2\n"+
				"│  └─ name: web\n"+
				"└─ [1]\n"+
				"   ├─ instances: 1\n"+
				"   └─ name: worker\n")
		})

		Convey("--depth collapses deeper structure", func() {
			out, rc := runDebugSession(files, "load\ntree --depth 1\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "├─ database {3 keys}\n")
			So(out, ShouldContainSubstring, "└─ meta {1 key}\n")
		})

		Convey("--keys drops values", func() {
			out, rc := runDebugSession(files, "load\ntree database --keys\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "├─ host\n")
			So(out, ShouldNotContainSubstring, "localhost")
		})

		Convey("--annotate inlines history truncated to the current step", func() {
			out, rc := runDebugSession(files, "load\nstep\ntree database --annotate\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "├─ host: db.prod.example.com\n"+
				"│    [0] ../../assets/history/base.yml → localhost\n"+
				"│    [1] ../../assets/history/env.yml → db.prod.example.com\n")
			// secrets.yml has not merged: no password leaf, no eval entries.
			So(out, ShouldNotContainSubstring, "password")
			So(out, ShouldNotContainSubstring, "<evaluated>")
		})

		Convey("--annotate overrides --keys so values show", func() {
			out, rc := runDebugSession(files, "load\ntree database --keys --annotate\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "├─ host: localhost\n")
		})

		Convey("--annotate keeps list history on the list itself", func() {
			out, rc := runDebugSession(collideFiles, "load\ntree --annotate\nquit\n")
			So(rc, ShouldEqual, 0)
			// The jobs list's own load entry prints under the jobs label.
			So(out, ShouldContainSubstring, "├─ jobs\n│    [0] ../../assets/debug/tree-collide.yml")
			// The jobs.[0].name leaf must not borrow the top-level name's
			// history: its "→ top" annotation appears exactly once.
			So(strings.Count(out, "→ top\n"), ShouldEqual, 1)
		})

		Convey("--depth collapse hides annotations beneath the cutoff", func() {
			out, rc := runDebugSession(files, "load\nstep\ntree --depth 1 --annotate\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "├─ database {3 keys}\n")
			// Match the annotation line's arrow, not the bare file path -
			// both `load`'s file listing and `step`'s own merge-diff report
			// already mention this file/value without an annotation.
			So(out, ShouldNotContainSubstring, "../../assets/history/base.yml →")
		})

		Convey("--history appends per-path blocks truncated to the current step", func() {
			out, rc := runDebugSession(files, "load\ntree database --history\nquit\n")
			So(rc, ShouldEqual, 0)
			// One block per tracked path under the prefix.
			So(out, ShouldContainSubstring, "database.host:\n")
			So(out, ShouldContainSubstring, "database.pool_size:\n")
			So(out, ShouldContainSubstring, "database.port:\n")
			So(out, ShouldContainSubstring, "localhost")
			So(out, ShouldContainSubstring, "As of step 0")
			// At step 0 only base.yml has loaded. (Match the entry line's
			// arrow, not the bare filename - `load`'s own file listing
			// already prints "env.yml".)
			So(out, ShouldNotContainSubstring, "env.yml →")
		})

		Convey("--history after full evaluation includes the evaluated entry", func() {
			out, rc := runDebugSession(files, "load\ncontinue\ntree database.password --history\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "database.password:\n")
			So(out, ShouldContainSubstring, "<evaluated>")
			So(out, ShouldContainSubstring, "1.0")
			So(out, ShouldContainSubstring, "As of step 3")
		})

		Convey("a targeted eval moves the tree ahead of the replayed history", func() {
			out, rc := runDebugSession(files, "load\nstep\nstep\neval database.password\ntree database.password --history\nquit\n")
			So(rc, ShouldEqual, 0)
			// The tree half shows the value eval just produced...
			So(out, ShouldContainSubstring, "database.password: \"1.0\"")
			// ...while the replay stops at step 2, where the operator is
			// still unevaluated - which is exactly why the final line
			// names the step instead of claiming to be "current".
			So(out, ShouldContainSubstring, "As of step 2")
		})

		Convey("a pruned path still gets a terminated block", func() {
			out, rc := runDebugSession(pruneFiles, "load\ncontinue\ntree config --history\nquit\n")
			So(rc, ShouldEqual, 0)
			// config.secret is gone from the tree but tracked in history.
			// internal/history.Track always rewrites a removed entry's
			// Source to "<pruned>", regardless of which step produced the
			// removal (history.go's Entry doc comment), so the literal step
			// label "<evaluated>" never survives for a removed path; "[1]"
			// is what ties the removal to the eval step (index 1: one file
			// step, then eval).
			So(out, ShouldContainSubstring, "config.secret:\n")
			So(out, ShouldContainSubstring, "[1] <pruned>")
			So(out, ShouldContainSubstring, fmt.Sprintf("%-*s → <pruned>", sourceColumnWidth, "As of step 1"))
		})

		Convey("a path inside a list still renders; history notes the limit", func() {
			out, rc := runDebugSession(listFiles, "load\ntree jobs.[0] --history\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "jobs.[0]\n")
			So(out, ShouldContainSubstring, "└─ name: web\n")
			So(out, ShouldContainSubstring, "history does not descend into lists")
		})

		Convey("tree on a missing path reports it like inspect", func() {
			out, rc := runDebugSession(files, "load\ntree no.such.path\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Path not found: no.such.path\n")
		})

		Convey("a bad flag prints the error and usage", func() {
			out, rc := runDebugSession(files, "load\ntree --wat\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "unknown flag")
			So(out, ShouldContainSubstring, "Usage: "+treeUsage)
		})

		Convey("bare --history requires a path", func() {
			out, rc := runDebugSession(files, "load\ntree --history\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "--history requires a path")
			So(out, ShouldContainSubstring, "Usage: "+treeUsage)
		})

		Convey("-h prints usage", func() {
			out, rc := runDebugSession(files, "load\ntree -h\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Usage: "+treeUsage)
		})

		Convey("help lists the tree command", func() {
			out, rc := runDebugSession(files, "help\nhelp tree\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "tree")
			So(out, ShouldContainSubstring, treeUsage)
		})
	})

	Convey("graft debug tree with color enabled", t, func() {
		prevColor := ansi.IsColorEnabled()
		ansi.Color(true)
		Reset(func() { ansi.Color(prevColor) })

		Convey("keys are cyan and unevaluated operators yellow", func() {
			out, rc := runDebugSession(files, "load\nstep\nstep\ntree database\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "\033[36mpassword\033[0m")
			So(out, ShouldContainSubstring, "\033[33m(( grab meta.version ))\033[0m")
		})

		Convey("--no-color strips color for that command only", func() {
			out, rc := runDebugSession(files, "load\ntree database --no-color\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "├─ host: localhost\n")
			So(out, ShouldNotContainSubstring, "\033[36m")
		})
	})
}

func TestHistoryKeyForPath(t *testing.T) {
	Convey("historyKeyForPath", t, func() {
		Convey("a plain dotted path maps through unchanged", func() {
			key, insideList := historyKeyForPath("database.host")
			So(key, ShouldEqual, "database.host")
			So(insideList, ShouldBeFalse)
		})

		Convey("an empty path maps to the empty key", func() {
			key, insideList := historyKeyForPath("")
			So(key, ShouldEqual, "")
			So(insideList, ShouldBeFalse)
		})

		Convey("a path into a list stops at the list and reports it", func() {
			key, insideList := historyKeyForPath("jobs.[0].name")
			So(key, ShouldEqual, "jobs")
			So(insideList, ShouldBeTrue)
		})
	})
}
