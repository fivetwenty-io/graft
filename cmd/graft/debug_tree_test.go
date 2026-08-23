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
