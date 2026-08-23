package termbg

import (
	"os"
	"testing"
)

func TestParseColorFGBG(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Background
	}{
		{"rxvt dark (light text on black)", "7;0", Dark},
		{"light text on white background", "0;15", Light},
		{"Konsole three-field dark", "15;default;0", Dark},
		{"Konsole three-field light", "0;default;15", Light},
		{"garbage, no separator", "banana", Unknown},
		{"empty", "", Unknown},
		{"background field out of range", "0;99", Unknown},
		{"background field non-numeric", "0;abc", Unknown},
		{"single field only", "7", Unknown},
		{"boundary dark slot 6", "0;6", Dark},
		{"boundary dark slot 8", "0;8", Dark},
		{"boundary light slot 9", "0;9", Light},
		{"boundary light slot 15", "0;15", Light},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseColorFGBG(tt.in); got != tt.want {
				t.Errorf("ParseColorFGBG(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// withIsTerminal swaps the IsTerminal seam for the duration of the
// test, restoring it afterward - the same function-var-swap pattern
// cmd/graft uses for isStderrTTY/isStdoutTTY and
// debugInputIsInteractive.
func withIsTerminal(t *testing.T, value bool) {
	t.Helper()
	prev := IsTerminal
	IsTerminal = func(*os.File) bool { return value }
	t.Cleanup(func() { IsTerminal = prev })
}

func TestDetectGuards(t *testing.T) {
	// Every guard test sets a COLORFGBG that would resolve to Dark if
	// consulted, so a Detect result of Unknown proves the guard fired
	// (and did not merely fall through to an unset COLORFGBG).
	const wouldBeDark = "15;0"

	t.Run("neither stream a terminal", func(t *testing.T) {
		withIsTerminal(t, false)
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("inside tmux", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("TERM starts with screen", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "screen-256color")
		t.Setenv("TMUX", "")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("TERM starts with tmux", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "tmux-256color")
		t.Setenv("TMUX", "")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("TERM empty", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "")
		t.Setenv("TMUX", "")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("TERM dumb", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "dumb")
		t.Setenv("TMUX", "")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("inside Emacs", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("INSIDE_EMACS", "27.1,comint")
		t.Setenv("COLORFGBG", wouldBeDark)
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})

	t.Run("every guard passes, COLORFGBG resolves dark", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("INSIDE_EMACS", "")
		t.Setenv("COLORFGBG", "15;0")
		if got := Detect(os.Stdin, os.Stdout); got != Dark {
			t.Errorf("Detect() = %v, want Dark", got)
		}
	})

	t.Run("every guard passes, COLORFGBG resolves light", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("INSIDE_EMACS", "")
		t.Setenv("COLORFGBG", "0;15")
		if got := Detect(os.Stdin, os.Stdout); got != Light {
			t.Errorf("Detect() = %v, want Light", got)
		}
	})

	t.Run("every guard passes, COLORFGBG unset", func(t *testing.T) {
		withIsTerminal(t, true)
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TMUX", "")
		t.Setenv("INSIDE_EMACS", "")
		// os.Getenv reports the same empty string for an unset variable
		// as for one explicitly set to "", so t.Setenv("", "") exercises
		// the same code path as an unset COLORFGBG while staying within
		// t.Setenv's automatic restore.
		t.Setenv("COLORFGBG", "")
		if got := Detect(os.Stdin, os.Stdout); got != Unknown {
			t.Errorf("Detect() = %v, want Unknown", got)
		}
	})
}
