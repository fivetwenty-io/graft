package main

import (
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// themeEnvVar is the environment variable --theme's value falls back to
// when the flag is not given (flag > env > default, see
// newRootCmd/PersistentPreRunE). Named GRAFT_THEME rather than
// GRAFT_UI_THEME: the mechanical GRAFT_<SECTION>_<FIELD> convention
// (internal/config/env.go) serves config-file-backed subsystem knobs,
// and the theme has no config-file tier this release, so it follows
// prior art's short form instead (BAT_THEME). This is the one
// deliberate exception to that convention, which is why
// themeEnvVarMisspelling exists: it warns anyone who reaches for the
// mechanical name instead.
const themeEnvVar = "GRAFT_THEME"

// themeEnvVarMisspelling is the mechanical-convention name a user might
// reasonably reach for instead of themeEnvVar. When it is set and
// themeEnvVar is not, PersistentPreRunE warns once on stderr so the
// mistake is not silently ignored (see docs/reference/environment-variables.md).
const themeEnvVarMisspelling = "GRAFT_UI_THEME"

// Theme name spellings, defined once so --theme/GRAFT_THEME parsing,
// each debugTheme's name field, and resolveDebugTheme's dispatch never
// drift from one another.
const (
	themeNameAuto  = "auto"
	themeNameDark  = "dark"
	themeNameLight = "light"
	themeNameMono  = "mono"
)

// knownThemeNames are every --theme/GRAFT_THEME value graft recognizes.
// "auto" is the default: background auto-detection, falling back to
// dark until full detection lands (see resolveDebugTheme).
var knownThemeNames = []string{themeNameAuto, themeNameDark, themeNameLight, themeNameMono}

// isValidThemeName reports whether name is one of knownThemeNames.
func isValidThemeName(name string) bool {
	for _, n := range knownThemeNames {
		if name == n {
			return true
		}
	}
	return false
}

// knownThemeNamesJoined renders knownThemeNames for error and warning
// text: "auto, dark, light, mono".
func knownThemeNamesJoined() string {
	return strings.Join(knownThemeNames, ", ")
}

// debugRole names what a piece of `graft debug` REPL output is - a
// path, a success message, a YAML key - never what color it gets. A
// debugTheme is the only thing that maps a role to a rendering; the
// call sites that print session output name the role, not the color.
type debugRole int

const (
	// rolePrompt is the "graft>" prompt. It is contrast-reserved: no
	// other role in any theme renders with the same style (see
	// TestDebugThemePromptStyleIsReservedToPrompt), so no line of
	// session output can be mistaken for the command line.
	rolePrompt debugRole = iota
	roleBanner
	roleHeading
	roleCounter
	roleSuccess
	roleWarn
	roleError
	roleBreak
	rolePath
	roleFile
	roleValueOld
	roleValueNew
	roleMuted
	roleCommand
	roleYAMLKey
	roleYAMLLiteral
	roleYAMLAnchor
	roleYAMLComment

	// debugRoleCount is the number of roles above, used to size each
	// theme's style table and to walk every role in tests.
	debugRoleCount
)

// debugTheme maps every debugRole to the SGR parameter string
// (ansi.Style) it renders with in this theme. The zero Style ("")
// renders as plain text; a theme uses that deliberately for a role
// whose meaning already reads plainly in words (see debugThemeMono's
// roleSuccess/roleYAMLLiteral).
type debugTheme struct {
	name   string
	styles [debugRoleCount]ansi.Style
}

// debugThemeDark is the default fallback theme: legible on the dark
// terminal backgrounds most developer terminals use.
var debugThemeDark = &debugTheme{
	name: themeNameDark,
	styles: [debugRoleCount]ansi.Style{
		rolePrompt:      "1;35", // bold magenta
		roleBanner:      "1",    // bold
		roleHeading:     "1",    // bold
		roleCounter:     "2",    // dim
		roleSuccess:     "32",   // green
		roleWarn:        "33",   // yellow
		roleError:       "1;31", // bold red
		roleBreak:       "1;33", // bold yellow
		rolePath:        "36",   // cyan
		roleFile:        "32",   // green
		roleValueOld:    "31",   // red
		roleValueNew:    "32",   // green
		roleMuted:       "2",    // dim
		roleCommand:     "1",    // bold
		roleYAMLKey:     "36",   // cyan
		roleYAMLLiteral: "33",   // yellow
		roleYAMLAnchor:  "2",    // dim
		roleYAMLComment: "2",    // dim
	},
}

// debugThemeLight retires yellow and cyan from body text (both are
// weak on a white background) and swaps the prompt-adjacent roles to
// blue; every role not listed here renders exactly as debugThemeDark.
var debugThemeLight = &debugTheme{
	name: themeNameLight,
	styles: [debugRoleCount]ansi.Style{
		rolePrompt:      "1;35", // bold magenta, same as dark
		roleBanner:      "1",
		roleHeading:     "1",
		roleCounter:     "2",
		roleSuccess:     "32",
		roleWarn:        "31", // red (dark's yellow is weak on white)
		roleError:       "1;31",
		roleBreak:       "1;34", // bold blue (dark's yellow is weak on white)
		rolePath:        "34",   // blue (dark's cyan is weak on white)
		roleFile:        "32",
		roleValueOld:    "31",
		roleValueNew:    "32",
		roleMuted:       "2",
		roleCommand:     "1",
		roleYAMLKey:     "34", // blue, matching rolePath above
		roleYAMLLiteral: "36", // cyan renders dark teal on light palettes
		roleYAMLAnchor:  "2",
		roleYAMLComment: "2",
	},
}

// debugThemeMono uses weight and decoration only, no color codes at
// all, for monochrome terminals and colorblind users. rolePrompt's
// reverse video is this theme's contrast reservation, mirroring bold
// magenta in dark/light. roleSuccess and roleYAMLLiteral are
// deliberately plain: a confirmation and a literal both already carry
// their meaning in the words, and mono has to leave something plain
// somewhere for the reservation to mean anything.
var debugThemeMono = &debugTheme{
	name: themeNameMono,
	styles: [debugRoleCount]ansi.Style{
		rolePrompt:      "7", // reverse video
		roleBanner:      "1", // bold
		roleHeading:     "1",
		roleCounter:     "2", // dim
		roleSuccess:     "",  // plain
		roleWarn:        "1",
		roleError:       "1",
		roleBreak:       "1",
		rolePath:        "4", // underline
		roleFile:        "4",
		roleValueOld:    "2",
		roleValueNew:    "1",
		roleMuted:       "2",
		roleCommand:     "1",
		roleYAMLKey:     "1",
		roleYAMLLiteral: "", // plain
		roleYAMLAnchor:  "2",
		roleYAMLComment: "2",
	},
}

// debugStyler renders debugSession output in one resolved theme, or
// leaves it untouched when disabled - the zero value, which is what
// every bytes.Buffer-driven test gets (see resolveDebugStyler).
type debugStyler struct {
	theme   *debugTheme
	enabled bool
}

// apply renders s in role's style, or returns s unchanged when the
// styler is disabled or has no theme resolved.
func (st debugStyler) apply(r debugRole, s string) string {
	if !st.enabled || st.theme == nil {
		return s
	}
	return st.theme.styles[r].Apply(s)
}

// debugUIOptions carries the presentation choices `graft debug` and
// `graft merge --interactive` resolve before their session starts.
// This is deliberately not a field on mergeOpts: mergeOpts feeds the
// persistent merge cache's key (mergeOutputCacheKey), and the debugger
// neither reads nor writes that cache nor ever touches the
// package-global ansi state, so nothing presentation-only belongs on
// it.
type debugUIOptions struct {
	// ColorOverride is --color/--no-color's resolved value: nil means
	// auto (resolve against the session's own out writer - see
	// resolveDebugStyler), non-nil forces color on or off outright
	// regardless of whether out is a terminal.
	ColorOverride *bool
	// Theme is the resolved theme name: "", "auto", "dark", "light",
	// or "mono". An unrecognized name, "auto", and "" all currently
	// resolve to debugThemeDark; background auto-detection (picking
	// dark or light for "auto") is not wired in yet.
	Theme string
}

// writerIsTTY reports whether out is a terminal, so resolveDebugStyler
// can decide auto-mode color against the debugger's own out writer
// instead of stderr (which is what the package-global ansi flag
// resolves against - see PersistentPreRunE). A function var, matching
// isStderrTTY/isStdoutTTY (main.go) and debugInputIsInteractive
// (debug_lineedit.go), so tests can fake a terminal without a real
// pty.
var writerIsTTY = func(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}

// resolveDebugStyler resolves ui into the debugStyler a debugSession
// uses for its own output. Enablement follows ansi.ResolveColor's
// precedence (explicit override, else NO_COLOR/TERM, else out being a
// terminal) - a bytes.Buffer is never a terminal, so every existing
// plain-buffer test gets a disabled (identity) styler regardless of
// the package-global ansi state or the test environment. When color
// is off, no theme is resolved at all: an "auto" theme with color off
// must never perform background-detection I/O.
func resolveDebugStyler(ui debugUIOptions, out io.Writer) debugStyler {
	if !ansi.ResolveColor(ui.ColorOverride, writerIsTTY(out)) {
		return debugStyler{}
	}
	return debugStyler{theme: resolveDebugTheme(ui.Theme), enabled: true}
}

// resolveDebugTheme maps a theme name to its table. "auto" is not yet
// backed by background detection (a later phase wires that in), so it
// falls back to dark, the documented default, same as "", themeNameDark
// itself, and any name resolveThemeTier did not already reject at the
// point of use (--theme's own invalid-flag error, or GRAFT_THEME's
// invalid-value warning; see resolveThemeTier in main.go).
func resolveDebugTheme(name string) *debugTheme {
	switch name {
	case themeNameLight:
		return debugThemeLight
	case themeNameMono:
		return debugThemeMono
	default: // "", themeNameDark, themeNameAuto, and anything unrecognized
		return debugThemeDark
	}
}
