package main

import (
	"reflect"
	"testing"
)

// TestDebugInputPainterAppliesRoleInput proves debugInputPainter wraps
// whatever runes it is given in roleInput's style, under both call
// shapes readline actually uses: the whole buffer (refresh/history/
// backspace path) and just the newly typed suffix (the append fast
// path taken when the cursor sits at the end of the line). See
// plans/colorizing-backlog-closeout.md decision 9: a stateless wrap is
// correct under both shapes with no extra branching, since the library
// never reads the painter's return value for cursor/width math.
func TestDebugInputPainterAppliesRoleInput(t *testing.T) {
	for _, theme := range allDebugThemes() {
		theme := theme
		t.Run(theme.name, func(t *testing.T) {
			st := debugStyler{enabled: true, theme: theme}
			painter := debugInputPainter(st)

			t.Run("whole buffer shape", func(t *testing.T) {
				got := painter([]rune("foo"), 3)
				want := st.apply(roleInput, "foo")
				if string(got) != want {
					t.Errorf("painter([]rune(%q), 3) = %q, want %q", "foo", string(got), want)
				}
			})

			t.Run("append-suffix shape", func(t *testing.T) {
				got := painter([]rune("f"), 1)
				want := st.apply(roleInput, "f")
				if string(got) != want {
					t.Errorf("painter([]rune(%q), 1) = %q, want %q", "f", string(got), want)
				}
			})

			t.Run("visible runes preserved exactly", func(t *testing.T) {
				got := painter([]rune("foo"), 3)
				gotStr := string(got)
				visible := stripSGR(gotStr)
				if visible != "foo" {
					t.Errorf("painter output %q strips to %q, want the original visible text %q", gotStr, visible, "foo")
				}
			})
		})
	}
}

// TestDebugInputPainterDisabledIsIdentity proves a disabled styler (or
// one with no theme resolved) emits zero escape bytes: the returned
// slice must equal the input slice exactly, under both call shapes,
// matching every other styled call site's "color off means no escapes"
// guarantee (decision 10).
func TestDebugInputPainterDisabledIsIdentity(t *testing.T) {
	cases := []struct {
		name string
		st   debugStyler
	}{
		{"disabled styler", debugStyler{}},
		{"enabled styler with no theme", debugStyler{enabled: true}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			painter := debugInputPainter(c.st)

			whole := []rune("foo")
			if got := painter(whole, 3); !reflect.DeepEqual(got, whole) {
				t.Errorf("painter(whole buffer) = %v, want unchanged %v", got, whole)
			}

			suffix := []rune("f")
			if got := painter(suffix, 1); !reflect.DeepEqual(got, suffix) {
				t.Errorf("painter(suffix) = %v, want unchanged %v", got, suffix)
			}
		})
	}
}

// TestDebugInputPainterEmptyLineIsIdentity proves an empty buffer is
// returned unchanged even with an enabled, themed styler: wrapping
// nothing in an SGR-and-reset pair would emit escape bytes bracketing
// no visible text, which is pure noise a redraw would still have to
// transmit for no visual effect.
func TestDebugInputPainterEmptyLineIsIdentity(t *testing.T) {
	for _, theme := range allDebugThemes() {
		theme := theme
		t.Run(theme.name, func(t *testing.T) {
			st := debugStyler{enabled: true, theme: theme}
			painter := debugInputPainter(st)

			empty := []rune{}
			got := painter(empty, 0)
			if len(got) != 0 {
				t.Errorf("painter(empty line) = %v (len %d), want empty", got, len(got))
			}
		})
	}
}

// TestDebugInputPainterTerminatingNewlineIsIdentity proves the painter
// leaves a lone terminating newline unstyled. readline calls the painter
// with ['\n'] on Enter (operation.go's refresh path -> runebuf.go's
// print, per the readline library's own line-accept handling), so
// without this guard the painted stream would end
// "...<SGR>T<reset><SGR>\n<reset>" - the reset landing after the line
// break, wrapping a rune with no visible effect (a bare newline renders
// no glyph for an SGR code to color) but incorrect all the same: the
// escape bytes are still there, and the reset that should immediately
// follow the last visible character now trails an extra rune later.
func TestDebugInputPainterTerminatingNewlineIsIdentity(t *testing.T) {
	for _, theme := range allDebugThemes() {
		theme := theme
		t.Run(theme.name, func(t *testing.T) {
			st := debugStyler{enabled: true, theme: theme}
			painter := debugInputPainter(st)

			line := []rune("\n")
			got := painter(line, 1)
			if !reflect.DeepEqual(got, line) {
				t.Errorf("painter([]rune(%q), 1) = %v, want unchanged %v (a lone terminating newline must never be painted)", "\n", got, line)
			}
		})
	}
}

// TestDebugInputPainterStatelessAcrossCalls proves the closure carries
// no state between invocations: calling it with the append-suffix shape
// and then the whole-buffer shape (or any other order/mix) never lets
// one call's input leak into another's output.
func TestDebugInputPainterStatelessAcrossCalls(t *testing.T) {
	st := debugStyler{enabled: true, theme: debugThemeDark}
	painter := debugInputPainter(st)

	first := painter([]rune("f"), 1)
	if want := st.apply(roleInput, "f"); string(first) != want {
		t.Fatalf("first call = %q, want %q", string(first), want)
	}

	second := painter([]rune("fo"), 2)
	if want := st.apply(roleInput, "fo"); string(second) != want {
		t.Fatalf("second call = %q, want %q", string(second), want)
	}

	third := painter([]rune("foo"), 3)
	if want := st.apply(roleInput, "foo"); string(third) != want {
		t.Fatalf("third call = %q, want %q", string(third), want)
	}
}

// TestDebugThemeInputStyleIsReservedFromPrompt locks the same invariant
// TestDebugThemePromptStyleIsReservedToPrompt already checks generically
// for every role, spelled out for roleInput specifically since it is the
// newest role added and the one most likely to accidentally collide
// (both roleInput and rolePrompt render text at the same visual
// location, the command line).
func TestDebugThemeInputStyleIsReservedFromPrompt(t *testing.T) {
	for _, theme := range allDebugThemes() {
		theme := theme
		t.Run(theme.name, func(t *testing.T) {
			if theme.styles[roleInput] == theme.styles[rolePrompt] {
				t.Errorf("theme %s: roleInput shares rolePrompt's style %q", theme.name, theme.styles[rolePrompt])
			}
		})
	}
}

// stripSGR removes every "\x1b[...m" SGR sequence from s, leaving only
// the visible runes, so a test can assert the painter never adds,
// removes, or reorders a visible rune - it may only wrap them.
func stripSGR(s string) string {
	var b []rune
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && runes[j] != 'm' {
				j++
			}
			i = j // skip past the terminating 'm'
			continue
		}
		b = append(b, runes[i])
	}
	return string(b)
}
