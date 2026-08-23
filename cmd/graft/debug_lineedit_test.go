package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ergochat/readline"
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

		Convey("completes the theme key alongside the vault.* keys for config", func() {
			So(completionsFor(c, "config th"), ShouldResemble, []string{"theme "})
			So(completionsFor(c, "config "), ShouldResemble, []string{"theme ", "vault.addr ", "vault.namespace ", "vault.token "})
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
			r := newDebugLineReader(strings.NewReader("inspect meta\n"), &out, "graft> ", nil, debugStyler{})
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

// TestDebugReadlineConfigInstallsPainter locks Phase 5's construction-time
// wiring: newDebugLineReader's readline path must carry a Painter built
// from the session's own styler (debugInputPainter), not the library's
// identity default. Tested against debugReadlineConfig directly - the
// seam newDebugLineReader itself builds its readline.Config from - so
// this needs no real terminal or readline instance.
func TestDebugReadlineConfigInstallsPainter(t *testing.T) {
	st := debugStyler{enabled: true, theme: debugThemeDark}
	cfg := debugReadlineConfig(strings.NewReader(""), io.Discard, "graft> ", nil, "", st)

	if cfg.Painter == nil {
		t.Fatal("debugReadlineConfig() Config.Painter is nil, want debugInputPainter(st)")
	}
	got := cfg.Painter([]rune("foo"), 3)
	want := st.apply(roleInput, "foo")
	if string(got) != want {
		t.Errorf("Config.Painter([]rune(%q), 3) = %q, want %q", "foo", string(got), want)
	}
}

// TestDebugReadlineConfigPainterIsIdentityWhenColorOff proves the wiring
// carries a disabled styler through faithfully: the constructed config
// still gets a non-nil Painter (debugInputPainter never returns nil), but
// that painter is a strict identity, matching every other "color off
// means zero escape bytes" call site in the debugger.
func TestDebugReadlineConfigPainterIsIdentityWhenColorOff(t *testing.T) {
	cfg := debugReadlineConfig(strings.NewReader(""), io.Discard, "graft> ", nil, "", debugStyler{})
	if cfg.Painter == nil {
		t.Fatal("debugReadlineConfig() Config.Painter is nil, want an identity painter, not none")
	}
	line := []rune("foo")
	got := cfg.Painter(line, 3)
	if !reflect.DeepEqual(got, line) {
		t.Errorf("Config.Painter(disabled)(%v) = %v, want unchanged", line, got)
	}
}

// TestReadlineLineReaderSetPainter proves SetPainter round-trips through
// GetConfig/mutate/SetConfig, the same pattern SetPrompt already uses
// internally, and leaves every other field - Prompt here, standing in
// for the rest - untouched. This is exercised against a real
// readline.Instance: readline.NewFromConfig works with any io.Reader for
// Stdin (the library only needs a real terminal for raw-mode/ANSI setup,
// which it gates on the test process's actual stdin being a tty - almost
// never true under `go test` - not on the Stdin field's concrete type),
// so no pty is needed here.
func TestReadlineLineReaderSetPainter(t *testing.T) {
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt: "graft> ",
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
	})
	if err != nil {
		t.Fatalf("readline.NewFromConfig() error = %v", err)
	}
	r := &readlineLineReader{rl: rl, out: io.Discard}
	defer func() { _ = r.Close() }()

	st := debugStyler{enabled: true, theme: debugThemeDark}
	r.SetPainter(debugInputPainter(st))

	cfg := rl.GetConfig()
	if cfg.Painter == nil {
		t.Fatal("SetPainter did not install a Painter on the live config")
	}
	got := cfg.Painter([]rune("foo"), 3)
	want := st.apply(roleInput, "foo")
	if string(got) != want {
		t.Errorf("installed painter([]rune(%q), 3) = %q, want %q", "foo", string(got), want)
	}
	if cfg.Prompt != "graft> " {
		t.Errorf("SetPainter() changed Prompt to %q, want unchanged %q", cfg.Prompt, "graft> ")
	}
}

// TestScannerLineReaderSetPainterIsNoop proves the scanner path's
// SetPainter compiles into a genuine no-op and never panics: it is
// called unconditionally from cmdConfigTheme whichever reader is active
// (gated only on the styler being enabled, not on reader type), so the
// scanner path must tolerate the call without changing any of its own
// state.
func TestScannerLineReaderSetPainterIsNoop(t *testing.T) {
	var out bytes.Buffer
	r := &scannerLineReader{scanner: bufio.NewScanner(strings.NewReader("")), out: &out, prompt: "graft> "}

	st := debugStyler{enabled: true, theme: debugThemeDark}
	r.SetPainter(debugInputPainter(st))

	if r.prompt != "graft> " {
		t.Errorf("SetPainter() changed scannerLineReader state: prompt = %q, want unchanged %q", r.prompt, "graft> ")
	}
}
