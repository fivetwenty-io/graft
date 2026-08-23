package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/termbg"
)

func TestResolveDebugThemeFor(t *testing.T) {
	tests := []struct {
		name string
		bg   termbg.Background
		want *debugTheme
	}{
		{"", termbg.Unknown, debugThemeDark},
		{"", termbg.Light, debugThemeLight},
		{"auto", termbg.Unknown, debugThemeDark},
		{"auto", termbg.Dark, debugThemeDark},
		{"auto", termbg.Light, debugThemeLight},
		{"dark", termbg.Light, debugThemeDark},  // explicit name wins regardless of bg
		{"light", termbg.Dark, debugThemeLight}, // explicit name wins regardless of bg
		{"mono", termbg.Light, debugThemeMono},  // explicit name wins regardless of bg
		{"bogus", termbg.Light, debugThemeDark}, // unrecognized falls back to dark, same as resolveDebugTheme
	}
	for _, tt := range tests {
		if got := resolveDebugThemeFor(tt.name, tt.bg); got != tt.want {
			t.Errorf("resolveDebugThemeFor(%q, %v) = %s, want %s", tt.name, tt.bg, got.name, tt.want.name)
		}
	}
}

func TestResolveDebugStylerUsesDetectedBackgroundForAuto(t *testing.T) {
	var out bytes.Buffer

	t.Run("unknown detected background falls back to dark", func(t *testing.T) {
		st := resolveDebugStyler(debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameAuto}, &out)
		if st.theme != debugThemeDark {
			t.Errorf("theme = %s, want dark", st.theme.name)
		}
	})

	t.Run("a detected light background resolves the light theme", func(t *testing.T) {
		st := resolveDebugStyler(debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameAuto, DetectedBackground: termbg.Light}, &out)
		if st.theme != debugThemeLight {
			t.Errorf("theme = %s, want light", st.theme.name)
		}
	})

	t.Run("a detected dark background resolves the dark theme", func(t *testing.T) {
		st := resolveDebugStyler(debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameAuto, DetectedBackground: termbg.Dark}, &out)
		if st.theme != debugThemeDark {
			t.Errorf("theme = %s, want dark", st.theme.name)
		}
	})

	t.Run("an explicit theme name ignores a detected background", func(t *testing.T) {
		st := resolveDebugStyler(debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameLight, DetectedBackground: termbg.Dark}, &out)
		if st.theme != debugThemeLight {
			t.Errorf("theme = %s, want light (explicit override)", st.theme.name)
		}
	})
}

func TestWithDetectedBackground(t *testing.T) {
	t.Run("theme not auto: DetectedBackground stays Unknown", func(t *testing.T) {
		inR, inW, outR, outW := newDetectionTestPipes(t)
		defer closeDetectionTestPipes(inR, inW, outR, outW)

		ui := withDetectedBackground(debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameDark}, inR, outW)
		if ui.DetectedBackground != termbg.Unknown {
			t.Errorf("DetectedBackground = %v, want Unknown (theme is not auto)", ui.DetectedBackground)
		}
	})

	t.Run("color resolves off: DetectedBackground stays Unknown even for auto", func(t *testing.T) {
		inR, inW, outR, outW := newDetectionTestPipes(t)
		defer closeDetectionTestPipes(inR, inW, outR, outW)

		ui := withDetectedBackground(debugUIOptions{ColorOverride: ptrBool(false), Theme: themeNameAuto}, inR, outW)
		if ui.DetectedBackground != termbg.Unknown {
			t.Errorf("DetectedBackground = %v, want Unknown (color is off)", ui.DetectedBackground)
		}
	})

	t.Run("in/out are not *os.File: DetectedBackground stays Unknown", func(t *testing.T) {
		var out bytes.Buffer
		ui := withDetectedBackground(debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameAuto}, strings.NewReader("quit\n"), &out)
		if ui.DetectedBackground != termbg.Unknown {
			t.Errorf("DetectedBackground = %v, want Unknown (not real files)", ui.DetectedBackground)
		}
	})

	t.Run("auto, color on, real files: termbg.Detect is consulted (COLORFGBG resolves it)", func(t *testing.T) {
		prevIsTerminal := termbg.IsTerminal
		termbg.IsTerminal = func(*os.File) bool { return true }
		defer func() { termbg.IsTerminal = prevIsTerminal }()

		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("INSIDE_EMACS", "")
		t.Setenv("COLORFGBG", "0;15") // resolves Light, no I/O needed

		inR, inW, outR, outW := newDetectionTestPipes(t)
		defer closeDetectionTestPipes(inR, inW, outR, outW)

		ui := withDetectedBackground(debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameAuto}, inR, outW)
		if ui.DetectedBackground != termbg.Light {
			t.Errorf("DetectedBackground = %v, want Light (from COLORFGBG)", ui.DetectedBackground)
		}
	})

	t.Run("auto, color on, real files, neither end a real tty: Detect's own guard reports Unknown", func(t *testing.T) {
		// termbg.IsTerminal is not faked here, so termbg.Detect's own
		// isatty guard sees the pipes for what they are and reports
		// Unknown without touching COLORFGBG or the OSC query at all.
		t.Setenv("COLORFGBG", "0;15") // would resolve Light if consulted

		inR, inW, outR, outW := newDetectionTestPipes(t)
		defer closeDetectionTestPipes(inR, inW, outR, outW)

		ui := withDetectedBackground(debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameAuto}, inR, outW)
		if ui.DetectedBackground != termbg.Unknown {
			t.Errorf("DetectedBackground = %v, want Unknown (pipes are not ttys)", ui.DetectedBackground)
		}
	})
}

// newDetectionTestPipes opens two os.Pipe() pairs standing in for a
// terminal's two directions, for tests that need real *os.File values
// (satisfying withDetectedBackground's type assertion) without ever
// touching an actual tty.
func newDetectionTestPipes(t *testing.T) (inR, inW, outR, outW *os.File) {
	t.Helper()
	var err error
	inR, inW, err = os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	outR, outW, err = os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	return inR, inW, outR, outW
}

func closeDetectionTestPipes(files ...*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

// TestDebugConfigThemeAutoReusesCachedDetection locks the plan's `config
// theme auto` behavior: it reuses the background handleDebug already
// detected at startup (cached on the session as detectedBackground)
// rather than re-querying the terminal mid-session. Constructing the
// session directly with DetectedBackground already set (bypassing real
// detection entirely, since this is a bytes.Buffer/strings.Reader
// session) isolates exactly that reuse.
func TestDebugConfigThemeAutoReusesCachedDetection(t *testing.T) {
	lightAutoUI := debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameDark, DetectedBackground: termbg.Light}
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, lightAutoUI, strings.Join([]string{
		"config theme mono",
		"config theme auto",
		"quit",
	}, "\n")+"\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0:\n%s", rc, out)
	}

	// The switch to "auto" must resolve against the cached Light
	// detection (from construction), not fall back to dark.
	wantConfirmation := styledLight(roleSuccess, "Theme set to light (auto)")
	if !strings.Contains(out, wantConfirmation) {
		t.Errorf("missing 'Theme set to light (auto)' confirmation styled in light:\n%s", out)
	}
}

// styledLight renders text in role's light-theme style, mirroring
// debug_colorize_test.go's dark-theme styled helper.
func styledLight(role debugRole, text string) string {
	return debugThemeLight.styles[role].Apply(text)
}
