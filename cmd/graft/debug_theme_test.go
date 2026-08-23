package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// allDebugThemes is every built-in theme, for tests that walk them all.
func allDebugThemes() []*debugTheme {
	return []*debugTheme{debugThemeDark, debugThemeLight, debugThemeMono}
}

// themeAllowsPlainRole reports whether theme deliberately leaves role
// unstyled (the empty ansi.Style), the two documented exceptions
// (mono's roleSuccess and roleYAMLLiteral). Every other role, in every
// theme, must resolve to a non-empty style, or the completeness test
// below would pass vacuously for a role nobody actually assigned.
func themeAllowsPlainRole(theme *debugTheme, role debugRole) bool {
	return theme == debugThemeMono && (role == roleSuccess || role == roleYAMLLiteral)
}

// TestDebugThemesDefineEveryRole walks all debugRoleCount roles in
// every built-in theme and fails if a role was left at the zero Style
// without being one of the two documented deliberate exceptions - the
// signal that a role was added to the enum but never given a color in
// one of the tables.
func TestDebugThemesDefineEveryRole(t *testing.T) {
	for _, theme := range allDebugThemes() {
		theme := theme
		t.Run(theme.name, func(t *testing.T) {
			for role := debugRole(0); role < debugRoleCount; role++ {
				style := theme.styles[role]
				if style == "" && !themeAllowsPlainRole(theme, role) {
					t.Errorf("theme %s: role %d has no style and is not a documented plain exception", theme.name, role)
				}
			}
		})
	}
}

// TestDebugThemePromptStyleIsReservedToPrompt locks decision 3: bold
// magenta ("1;35") appears in no role but rolePrompt across the color
// themes, and reverse video ("7") appears in no role but rolePrompt in
// mono. Without this, an output line could render identically to the
// prompt, defeating the whole point of a reserved prompt style.
func TestDebugThemePromptStyleIsReservedToPrompt(t *testing.T) {
	for _, theme := range allDebugThemes() {
		theme := theme
		t.Run(theme.name, func(t *testing.T) {
			promptStyle := theme.styles[rolePrompt]
			for role := debugRole(0); role < debugRoleCount; role++ {
				if role == rolePrompt {
					continue
				}
				if theme.styles[role] == promptStyle {
					t.Errorf("theme %s: role %d shares the prompt's style %q", theme.name, role, promptStyle)
				}
			}
		})
	}
}

func TestDebugStylerApply(t *testing.T) {
	t.Run("disabled styler returns text unchanged", func(t *testing.T) {
		st := debugStyler{}
		if got := st.apply(roleError, "boom"); got != "boom" {
			t.Errorf("apply() = %q, want unchanged text", got)
		}
	})

	t.Run("enabled styler with no theme returns text unchanged", func(t *testing.T) {
		st := debugStyler{enabled: true}
		if got := st.apply(roleError, "boom"); got != "boom" {
			t.Errorf("apply() = %q, want unchanged text", got)
		}
	})

	t.Run("enabled styler with a theme applies that role's own style", func(t *testing.T) {
		st := debugStyler{enabled: true, theme: debugThemeDark}
		cases := []struct {
			role debugRole
			text string
		}{
			{roleError, "Merge failed"},
			{roleSuccess, "Merge complete"},
			{roleWarn, "No documents loaded"},
			{rolePath, "database.host"},
			{roleYAMLKey, "database"},
		}
		for _, c := range cases {
			want := debugThemeDark.styles[c.role].Apply(c.text)
			if got := st.apply(c.role, c.text); got != want {
				t.Errorf("apply(role %d, %q) = %q, want %q", c.role, c.text, got, want)
			}
		}
	})
}

func TestResolveDebugTheme(t *testing.T) {
	tests := []struct {
		name string
		want *debugTheme
	}{
		{"", debugThemeDark},
		{"auto", debugThemeDark},
		{"dark", debugThemeDark},
		{"light", debugThemeLight},
		{"mono", debugThemeMono},
		{"bogus", debugThemeDark},
	}
	for _, tt := range tests {
		if got := resolveDebugTheme(tt.name); got != tt.want {
			t.Errorf("resolveDebugTheme(%q) = %s, want %s", tt.name, got.name, tt.want.name)
		}
	}
}

func ptrBool(b bool) *bool { return &b }

func TestResolveDebugStyler(t *testing.T) {
	t.Run("a bytes.Buffer resolves color off regardless of the package global", func(t *testing.T) {
		prev := ansi.IsColorEnabled()
		ansi.Color(true)
		defer ansi.Color(prev)

		var out bytes.Buffer
		st := resolveDebugStyler(debugUIOptions{}, &out)
		if st.enabled {
			t.Error("resolveDebugStyler() enabled = true against a bytes.Buffer, want false")
		}
		if got := st.apply(roleError, "boom"); got != "boom" {
			t.Errorf("apply() on a disabled styler = %q, want unchanged text", got)
		}
	})

	t.Run("explicit ColorOverride true enables color even against a non-terminal writer", func(t *testing.T) {
		var out bytes.Buffer
		st := resolveDebugStyler(debugUIOptions{ColorOverride: ptrBool(true)}, &out)
		if !st.enabled {
			t.Error("resolveDebugStyler() enabled = false with an explicit true override, want true")
		}
		if st.theme != debugThemeDark {
			t.Errorf("resolveDebugStyler() theme = %s, want dark (the default)", st.theme.name)
		}
	})

	t.Run("no-color faked TTY: explicit ColorOverride false wins even when writerIsTTY reports true", func(t *testing.T) {
		prevWriterIsTTY := writerIsTTY
		writerIsTTY = func(io.Writer) bool { return true }
		defer func() { writerIsTTY = prevWriterIsTTY }()

		var out bytes.Buffer
		st := resolveDebugStyler(debugUIOptions{ColorOverride: ptrBool(false)}, &out)
		if st.enabled {
			t.Error("resolveDebugStyler() enabled = true despite an explicit false override and a faked TTY, want false")
		}
	})

	t.Run("auto mode against a faked TTY writer resolves color on when the environment permits it", func(t *testing.T) {
		prevWriterIsTTY := writerIsTTY
		writerIsTTY = func(io.Writer) bool { return true }
		defer func() { writerIsTTY = prevWriterIsTTY }()

		t.Setenv("NO_COLOR", "")
		t.Setenv("TERM", "xterm-256color")

		var out bytes.Buffer
		st := resolveDebugStyler(debugUIOptions{}, &out)
		if !st.enabled {
			t.Error("resolveDebugStyler() enabled = false in auto mode against a faked TTY with a permissive environment, want true")
		}
	})
}

// writeThemeConfigFile writes a graft.yaml-shaped file with the given raw
// YAML content at path, creating parent directories as needed.
func writeThemeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// TestResolveThemeFileValue exercises resolveThemeFileValue directly
// against explicit search-path lists: a valid ui.theme value, an invalid
// one, a missing file, and malformed YAML, per
// plans/colorizing-backlog-closeout.md Phase 2 step 1.
func TestResolveThemeFileValue(t *testing.T) {
	t.Run("a valid ui.theme value is returned", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "graft.yaml")
		writeThemeConfigFile(t, path, "ui:\n  theme: light\n")

		value, found, warn := resolveThemeFileValue([]string{path})
		if !found || value != "light" || warn != "" {
			t.Errorf("resolveThemeFileValue() = (%q, %v, %q), want (\"light\", true, \"\")", value, found, warn)
		}
	})

	t.Run("an invalid ui.theme value warns and reports no usable value", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "graft.yaml")
		writeThemeConfigFile(t, path, "ui:\n  theme: bogus\n")

		value, found, warn := resolveThemeFileValue([]string{path})
		if found || value != "" {
			t.Errorf("resolveThemeFileValue() = (%q, %v, _), want (\"\", false, _) for an invalid value", value, found)
		}
		if warn == "" || !strings.Contains(warn, "bogus") || !strings.Contains(warn, path) {
			t.Errorf("resolveThemeFileValue() warn = %q, want it to name the bad value and the file path", warn)
		}
	})

	t.Run("no file present in any search path reports no value and no warning", func(t *testing.T) {
		dir := t.TempDir()
		value, found, warn := resolveThemeFileValue([]string{filepath.Join(dir, "does-not-exist.yaml")})
		if found || value != "" || warn != "" {
			t.Errorf("resolveThemeFileValue() = (%q, %v, %q), want (\"\", false, \"\")", value, found, warn)
		}
	})

	t.Run("malformed YAML is silently ignored, never a hard error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "graft.yaml")
		writeThemeConfigFile(t, path, "ui: [this is not: valid: yaml\n")

		value, found, warn := resolveThemeFileValue([]string{path})
		if found || value != "" || warn != "" {
			t.Errorf("resolveThemeFileValue() = (%q, %v, %q), want (\"\", false, \"\") for malformed YAML", value, found, warn)
		}
	})

	t.Run("a file present but with no ui.theme key reports no value", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "graft.yaml")
		writeThemeConfigFile(t, path, "engine:\n  strict_mode: true\n")

		value, found, warn := resolveThemeFileValue([]string{path})
		if found || value != "" || warn != "" {
			t.Errorf("resolveThemeFileValue() = (%q, %v, %q), want (\"\", false, \"\")", value, found, warn)
		}
	})

	t.Run("the first existing file in search-path order wins, even without a theme key", func(t *testing.T) {
		firstPath := filepath.Join(t.TempDir(), "first.yaml")
		writeThemeConfigFile(t, firstPath, "engine:\n  strict_mode: true\n") // exists, no ui.theme

		secondPath := filepath.Join(t.TempDir(), "second.yaml")
		writeThemeConfigFile(t, secondPath, "ui:\n  theme: light\n") // would supply a value, but comes second

		missingPath := filepath.Join(t.TempDir(), "missing.yaml") // does not exist, tried first

		value, found, warn := resolveThemeFileValue([]string{missingPath, firstPath, secondPath})
		if found || value != "" || warn != "" {
			t.Errorf("resolveThemeFileValue() = (%q, %v, %q), want (\"\", false, \"\"): first *existing* file (firstPath) wins, second.yaml must never be consulted", value, found, warn)
		}
	})

	t.Run("a missing first path falls through to the next existing one", func(t *testing.T) {
		missingPath := filepath.Join(t.TempDir(), "missing.yaml")
		presentPath := filepath.Join(t.TempDir(), "graft.yaml")
		writeThemeConfigFile(t, presentPath, "ui:\n  theme: mono\n")

		value, found, warn := resolveThemeFileValue([]string{missingPath, presentPath})
		if !found || value != "mono" || warn != "" {
			t.Errorf("resolveThemeFileValue() = (%q, %v, %q), want (\"mono\", true, \"\")", value, found, warn)
		}
	})
}

// TestThemeConfigSearchPaths locks the three documented search paths and
// their order (docs/reference/config.md): ./graft.yaml, then
// $HOME/.graft/config.yaml, then /etc/graft/config.yaml when /etc exists.
func TestThemeConfigSearchPaths(t *testing.T) {
	paths := themeConfigSearchPaths()
	if len(paths) < 2 {
		t.Fatalf("themeConfigSearchPaths() = %v, want at least 2 entries", paths)
	}
	if paths[0] != "./graft.yaml" {
		t.Errorf("themeConfigSearchPaths()[0] = %q, want %q", paths[0], "./graft.yaml")
	}
	if paths[1] != "~/.graft/config.yaml" {
		t.Errorf("themeConfigSearchPaths()[1] = %q, want %q", paths[1], "~/.graft/config.yaml")
	}
	if _, err := os.Stat("/etc"); err == nil {
		if len(paths) != 3 || paths[2] != "/etc/graft/config.yaml" {
			t.Errorf("themeConfigSearchPaths() = %v, want a third entry %q on a system with /etc", paths, "/etc/graft/config.yaml")
		}
	}
}

// TestResolveThemeFileValueEndToEndSearchOrder proves the full pipeline
// (themeConfigSearchPaths + resolveThemeFileValue) picks up a graft.yaml
// from the current directory ahead of $HOME/.graft/config.yaml, and falls
// back to the home-directory file when no ./graft.yaml exists - using a
// real chdir and a faked $HOME, matching this package's existing
// cwd/HOME test conventions (see chdir in examples_test.go, t.Setenv).
func TestResolveThemeFileValueEndToEndSearchOrder(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	writeThemeConfigFile(t, filepath.Join(fakeHome, ".graft", "config.yaml"), "ui:\n  theme: light\n")

	t.Run("$HOME/.graft/config.yaml is used when no ./graft.yaml exists", func(t *testing.T) {
		workDir := t.TempDir()
		restore := chdir(t, workDir)
		defer restore()

		value, found, warn := resolveThemeFileValue(themeConfigSearchPaths())
		if !found || value != "light" || warn != "" {
			t.Errorf("resolveThemeFileValue() = (%q, %v, %q), want (\"light\", true, \"\")", value, found, warn)
		}
	})

	t.Run("./graft.yaml wins over $HOME/.graft/config.yaml", func(t *testing.T) {
		workDir := t.TempDir()
		restore := chdir(t, workDir)
		defer restore()
		writeThemeConfigFile(t, filepath.Join(workDir, "graft.yaml"), "ui:\n  theme: mono\n")

		value, found, warn := resolveThemeFileValue(themeConfigSearchPaths())
		if !found || value != "mono" || warn != "" {
			t.Errorf("resolveThemeFileValue() = (%q, %v, %q), want (\"mono\", true, \"\")", value, found, warn)
		}
	})
}

// TestWriterIsTTYRealFile proves the default writerIsTTY seam actually
// consults isatty rather than always returning a constant: os.DevNull
// opened for writing is a real *os.File but never a terminal.
func TestWriterIsTTYRealFile(t *testing.T) {
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	defer func() { _ = f.Close() }()

	if writerIsTTY(f) {
		t.Errorf("writerIsTTY(%s) = true, want false", os.DevNull)
	}

	var buf bytes.Buffer
	if writerIsTTY(&buf) {
		t.Error("writerIsTTY(bytes.Buffer) = true, want false (not an *os.File at all)")
	}
}
