package main

import (
	"bytes"
	"io"
	"os"
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
