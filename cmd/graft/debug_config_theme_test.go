package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

// styledMono renders text in role's mono-theme style, mirroring
// debug_colorize_test.go's dark-theme styled helper, for tests that must
// prove a switch actually changed which theme's codes appear.
func styledMono(role debugRole, text string) string {
	return debugThemeMono.styles[role].Apply(text)
}

// TestScannerLineReaderSetPrompt is the scanner-path unit test the plan
// calls out by name: SetPrompt must change what the *next* ReadLine call
// prints, not the line already in flight.
func TestScannerLineReaderSetPrompt(t *testing.T) {
	var out bytes.Buffer
	r := &scannerLineReader{scanner: bufio.NewScanner(strings.NewReader("first\nsecond\n")), out: &out, prompt: "graft> "}

	if _, err := r.ReadLine(); err != nil {
		t.Fatalf("ReadLine() 1st call: %v", err)
	}
	r.SetPrompt("mono> ")
	if _, err := r.ReadLine(); err != nil {
		t.Fatalf("ReadLine() 2nd call: %v", err)
	}

	want := "graft> mono> "
	if out.String() != want {
		t.Errorf("prompts printed = %q, want %q", out.String(), want)
	}
}

// TestDebugConfigThemeListingRow locks the plan's `config` listing
// addition: a `theme: <name>` row, "dark (auto)" when the session's
// selection is auto (today always resolving to dark - see
// resolveDebugTheme) and the bare name when pinned. All three cases
// keep the row unstyled, matching decision 12.
func TestDebugConfigThemeListingRow(t *testing.T) {
	t.Run("explicit theme selection shows the bare name", func(t *testing.T) {
		out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "config\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0:\n%s", rc, out)
		}
		if !strings.Contains(out, "theme: dark\n") {
			t.Errorf("listing missing plain 'theme: dark' row:\n%s", out)
		}
	})

	t.Run("auto selection shows the resolved palette plus (auto)", func(t *testing.T) {
		autoUI := debugUIOptions{ColorOverride: ptrBool(true), Theme: themeNameAuto}
		out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, autoUI, "config\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0:\n%s", rc, out)
		}
		if !strings.Contains(out, "theme: dark (auto)\n") {
			t.Errorf("listing missing 'theme: dark (auto)' row:\n%s", out)
		}
	})

	t.Run("a zero-value debugUIOptions (color off) still reports the auto default", func(t *testing.T) {
		out, rc := runDebugSession(debugColorizeTestFiles, "config\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0:\n%s", rc, out)
		}
		if !strings.Contains(out, "theme: dark (auto)\n") {
			t.Errorf("listing missing 'theme: dark (auto)' row with color off:\n%s", out)
		}
		if strings.ContainsRune(out, '\x1b') {
			t.Errorf("color-off listing carries an escape byte:\n%q", out)
		}
	})
}

// TestDebugConfigThemeGetArm locks `config theme` (no value): it prints
// the same "Current: <name>" shape every other single-key read does,
// unstyled per decision 12.
func TestDebugConfigThemeGetArm(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "config theme\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0:\n%s", rc, out)
	}
	if !strings.Contains(out, "Current: dark\n") {
		t.Errorf("get arm missing plain 'Current: dark' line:\n%s", out)
	}
}

// TestDebugConfigThemeSetArmSwitchesStyling proves `config theme <name>`
// changes which theme's codes later output actually carries, not just
// the session's recorded preference: a role whose mono rendering
// differs from dark's (roleWarn) must show mono's code after the
// switch, and dark's code for that same message must not reappear.
func TestDebugConfigThemeSetArmSwitchesStyling(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, strings.Join([]string{
		"config theme mono",
		"break", // no path: prints the roleWarn usage line
		"quit",
	}, "\n")+"\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0:\n%s", rc, out)
	}

	// The confirmation renders in the theme just switched to (mono),
	// whose roleSuccess is deliberately plain (debugThemeMono), so the
	// meaningful assertion here is that it appears at all, not that it
	// carries an escape code - see TestDebugConfigThemeSetArmRestylesPrompt
	// and the invalid-name test below for the escape-bearing assertions.
	if !strings.Contains(out, "Theme set to mono\n") {
		t.Errorf("missing switch confirmation:\n%s", out)
	}

	monoUsage := styledMono(roleWarn, "Usage: break <path>")
	darkUsage := styled(roleWarn, "Usage: break <path>")
	if !strings.Contains(out, monoUsage) {
		t.Errorf("post-switch output not carrying mono's roleWarn code:\n%s", out)
	}
	if strings.Contains(out, darkUsage) {
		t.Errorf("post-switch output still carries dark's roleWarn code:\n%s", out)
	}
}

// TestDebugConfigThemeSetArmRestylesPrompt proves the live prompt
// restyle (SetPrompt, Phase 9) reaches the scanner path: a bytes.Buffer
// session always selects the scanner (neither end is a real terminal),
// which is exactly the path the plan calls out as needing the same
// restyle a readline session gets.
func TestDebugConfigThemeSetArmRestylesPrompt(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "config theme mono\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0:\n%s", rc, out)
	}

	darkPrompt := styled(rolePrompt, "graft>")
	monoPrompt := styledMono(rolePrompt, "graft>")

	firstDark := strings.Index(out, darkPrompt)
	firstMono := strings.Index(out, monoPrompt)
	if firstDark < 0 {
		t.Fatalf("initial dark-styled prompt never appeared:\n%q", out)
	}
	if firstMono < 0 {
		t.Fatalf("mono-styled prompt never appeared after the switch:\n%q", out)
	}
	if firstMono <= firstDark {
		t.Errorf("mono prompt (at %d) did not follow the initial dark prompt (at %d):\n%q", firstMono, firstDark, out)
	}
}

// TestDebugConfigThemeInvalidNameLeavesThemeUnchanged locks the "no
// state change" half of an unknown `config theme <name>`: the error
// text matches the plan's wording exactly, and later output still
// carries the theme active before the failed attempt.
func TestDebugConfigThemeInvalidNameLeavesThemeUnchanged(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, strings.Join([]string{
		"config theme bogus",
		"break", // no path: prints the roleWarn usage line
		"quit",
	}, "\n")+"\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0:\n%s", rc, out)
	}

	wantErr := styled(roleError, "Unknown theme: bogus. Known themes: auto, dark, light, mono.")
	if !strings.Contains(out, wantErr) {
		t.Errorf("missing roleError unknown-theme message:\n%s", out)
	}

	// Theme is unchanged (still dark, from colorOnUI): the guard message
	// after the failed attempt still carries dark's roleWarn code.
	stillDark := styled(roleWarn, "Usage: break <path>")
	if !strings.Contains(out, stillDark) {
		t.Errorf("theme changed despite an invalid name:\n%s", out)
	}
}

// TestDebugConfigThemeWithColorDisabled locks the plan's disabled-color
// behavior: the choice is still recorded (a later `config theme` read
// reflects it) and a one-line notice explains why nothing visibly
// changed, with the whole session still carrying zero escape bytes.
func TestDebugConfigThemeWithColorDisabled(t *testing.T) {
	out, rc := runDebugSession(debugColorizeTestFiles, strings.Join([]string{
		"config theme mono",
		"config theme",
		"quit",
	}, "\n")+"\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0:\n%s", rc, out)
	}
	if !strings.Contains(out, "Theme set to mono\n") {
		t.Errorf("missing plain switch confirmation:\n%s", out)
	}
	if !strings.Contains(out, debugColorDisabledNotice+"\n") {
		t.Errorf("missing color-disabled notice:\n%s", out)
	}
	if !strings.Contains(out, "Current: mono\n") {
		t.Errorf("the recorded choice was not reflected by a later read:\n%s", out)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("color-off session carries an escape byte:\n%q", out)
	}
}

// TestDebugColorOnConfigThemeStaysPlain extends the credential-guard
// posture (decision 12, TestDebugColorOnConfigStaysPlain) to the new
// theme rows: with color on, every "theme:"/"Current:" line stays
// unstyled, the same rule every other config value line follows. This
// is an additional test alongside the existing guard, not a
// replacement for it.
func TestDebugColorOnConfigThemeStaysPlain(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, strings.Join([]string{
		"config",
		"config theme",
		"config theme mono",
		"quit",
	}, "\n")+"\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0:\n%s", rc, out)
	}
	for _, line := range strings.Split(out, "\n") {
		plain := strings.HasPrefix(line, "theme:") || strings.HasPrefix(line, "Current:")
		if plain && strings.ContainsRune(line, '\x1b') {
			t.Errorf("theme value line contains an escape byte: %q", line)
		}
	}
}
