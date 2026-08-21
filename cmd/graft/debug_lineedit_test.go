package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// completionsFor runs c.Do over the whole of line (cursor at the end, the
// only position a tab press can reach in practice) and re-attaches each
// returned suffix to the prefix it completes, so assertions read as whole
// words rather than as the suffix fragments readline works in.
func completionsFor(c *debugCompleter, line string) []string {
	runes := []rune(line)
	suffixes, length := c.Do(runes, len(runes))
	prefix := ""
	if length > 0 {
		prefix = string(runes[len(runes)-length:])
	}
	out := make([]string, 0, len(suffixes))
	for _, s := range suffixes {
		out = append(out, prefix+string(s))
	}
	return out
}

// completerFixture is a session with a small loaded tree, standing in for a
// real merge so completion has concrete keys to offer.
func completerFixture() *debugCompleter {
	return &debugCompleter{sess: &debugSession{
		loaded: true,
		tree: map[string]interface{}{
			"meta": map[string]interface{}{
				"environment": "production",
				"replicas":    6,
			},
			"properties": map[string]interface{}{
				"api": map[string]interface{}{
					"timeout": 30,
				},
			},
			"jobs": []interface{}{
				map[string]interface{}{"name": "api"},
				map[string]interface{}{"name": "worker"},
			},
		},
		breakpoints: map[string]bool{"meta.replicas": true},
		deferred:    map[string]string{},
	}}
}

func TestDebugCompleter(t *testing.T) {
	Convey("the debug REPL's tab completion", t, func() {
		c := completerFixture()

		Convey("completes command names on the first word", func() {
			So(completionsFor(c, ""), ShouldContain, "inspect ")
			So(completionsFor(c, ""), ShouldContain, "quit ")
			So(completionsFor(c, "ins"), ShouldResemble, []string{"inspect "})
			So(completionsFor(c, "brea"), ShouldResemble, []string{"break ", "breaks "})
		})

		Convey("completes top-level document paths for path commands", func() {
			So(completionsFor(c, "inspect "), ShouldResemble, []string{"jobs", "meta", "properties"})
			So(completionsFor(c, "inspect me"), ShouldResemble, []string{"meta"})
			So(completionsFor(c, "history prop"), ShouldResemble, []string{"properties"})
		})

		Convey("completes nested paths one level at a time", func() {
			So(completionsFor(c, "inspect meta."), ShouldResemble, []string{"meta.environment ", "meta.replicas "})
			So(completionsFor(c, "inspect meta.rep"), ShouldResemble, []string{"meta.replicas "})
			So(completionsFor(c, "break properties.api."), ShouldResemble, []string{"properties.api.timeout "})
		})

		Convey("offers list indices for a sequence", func() {
			So(completionsFor(c, "inspect jobs."), ShouldResemble, []string{"jobs.[0]", "jobs.[1]"})
		})

		Convey("leaves a trailing space only on paths with nothing below them", func() {
			// "meta" has children, so completing it must not end the word.
			So(completionsFor(c, "inspect met"), ShouldResemble, []string{"meta"})
			// "meta.replicas" is a scalar, so the word is finished.
			So(completionsFor(c, "inspect meta.replic"), ShouldResemble, []string{"meta.replicas "})
		})

		Convey("offers nothing for a path that does not resolve", func() {
			So(completionsFor(c, "inspect nope.nothing."), ShouldBeEmpty)
		})

		Convey("offers nothing before load, when there is no tree yet", func() {
			empty := &debugCompleter{sess: &debugSession{breakpoints: map[string]bool{}}}
			So(completionsFor(empty, "inspect "), ShouldBeEmpty)
			// Command completion still works with no document loaded.
			So(completionsFor(empty, "loa"), ShouldResemble, []string{"load "})
		})

		Convey("completes only paths that actually have a breakpoint for unbreak", func() {
			So(completionsFor(c, "unbreak "), ShouldResemble, []string{"meta.replicas "})
		})

		Convey("completes known keys for config", func() {
			So(completionsFor(c, "config va"), ShouldResemble, []string{"vault.addr ", "vault.namespace ", "vault.token "})
			// A config value is free-form, so there is nothing to offer.
			So(completionsFor(c, "config vault.addr http"), ShouldBeEmpty)
		})

		Convey("completes command names for help", func() {
			So(completionsFor(c, "help ins"), ShouldResemble, []string{"inspect "})
		})

		Convey("completes filenames for export", func() {
			dir, err := os.MkdirTemp("", "graft-completion")
			So(err, ShouldBeNil)
			defer func() { _ = os.RemoveAll(dir) }()
			So(os.WriteFile(filepath.Join(dir, "rendered.yml"), []byte("---\n"), 0o600), ShouldBeNil)
			So(os.Mkdir(filepath.Join(dir, "renderings"), 0o700), ShouldBeNil)

			base := filepath.Join(dir, "render")
			So(completionsFor(c, "export "+base), ShouldResemble, []string{
				filepath.Join(dir, "rendered.yml") + " ",
				// A directory keeps its separator so the next Tab descends.
				filepath.Join(dir, "renderings") + string(os.PathSeparator),
			})
		})

		Convey("offers nothing for commands that take no argument", func() {
			So(completionsFor(c, "output "), ShouldBeEmpty)
			So(completionsFor(c, "breaks any"), ShouldBeEmpty)
		})
	})
}

func TestDebugHistoryFiltering(t *testing.T) {
	Convey("REPL history persistence", t, func() {
		Convey("keeps ordinary commands", func() {
			So(debugHistoryWorthSaving("inspect meta.replicas"), ShouldBeTrue)
			So(debugHistoryWorthSaving("config vault.addr https://vault.example.com"), ShouldBeTrue)
		})

		Convey("drops blank lines", func() {
			So(debugHistoryWorthSaving("   "), ShouldBeFalse)
		})

		Convey("drops any line that sets a secret config value", func() {
			So(debugHistoryWorthSaving("config vault.token s.SuperSecret"), ShouldBeFalse)
			So(debugHistoryWorthSaving("  config   vault.token   s.SuperSecret  "), ShouldBeFalse)
		})

		Convey("keeps a bare read of a secret key, which reveals nothing", func() {
			So(debugHistoryWorthSaving("config vault.token"), ShouldBeTrue)
		})
	})
}

func TestDebugLineReaderSelection(t *testing.T) {
	Convey("the REPL's input source", t, func() {
		Convey("falls back to the plain scanner when stdin is not a terminal", func() {
			var out bytes.Buffer
			r := newDebugLineReader(strings.NewReader("inspect meta\n"), &out, "graft> ", nil)
			defer func() { _ = r.Close() }()

			line, err := r.ReadLine()
			So(err, ShouldBeNil)
			So(line, ShouldEqual, "inspect meta")
			So(out.String(), ShouldEqual, "graft> ")

			_, err = r.ReadLine()
			So(err, ShouldEqual, io.EOF)
		})
	})
}
