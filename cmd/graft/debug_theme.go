package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/goccy/go-yaml"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/internal/utils/termbg"
)

// themeEnvVar is the environment variable --theme's value falls back to
// when the flag is not given (flag > env > file > default, see
// newRootCmd/PersistentPreRunE/resolveThemeTier). Named GRAFT_THEME
// rather than GRAFT_UI_THEME: the mechanical GRAFT_<SECTION>_<FIELD>
// convention (internal/config/env.go) serves config-file-backed
// subsystem knobs, and the theme's own file tier is a standalone
// ui.theme reader (resolveThemeFileValue) independent of that package,
// so the env var follows prior art's short form instead (BAT_THEME).
// This is the one deliberate exception to that convention, which is why
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

// themeConfigSearchPaths returns the three documented config-file
// locations, in search order: ./graft.yaml (current directory),
// ~/.graft/config.yaml (user config directory, unexpanded - see
// expandThemeConfigPath), then /etc/graft/config.yaml, the last only on
// a system that has an /etc directory at all. This mirrors
// internal/config's getSearchPaths() exactly (docs/reference/config.md),
// duplicated here rather than imported: resolveThemeFileValue is a
// standalone reader that never depends on internal/config, so it does
// not activate that package's other 16 Config fields as a side effect
// (see decision 5, plans/colorizing-backlog-closeout.md).
func themeConfigSearchPaths() []string {
	paths := []string{"./graft.yaml", "~/.graft/config.yaml"}
	if _, err := os.Stat("/etc"); err == nil {
		paths = append(paths, "/etc/graft/config.yaml")
	}
	return paths
}

// expandThemeConfigPath expands a leading "~" to the user's home
// directory, leaving every other path (including "" and already-absolute
// or relative paths) unchanged. Mirrors internal/config's expandPath,
// duplicated for the same reason as themeConfigSearchPaths.
func expandThemeConfigPath(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}

// themeFileConfig decodes only the ui.theme key out of a config-file
// document, ignoring every other key present. This is the entire reason
// resolveThemeFileValue does not reuse internal/config.Load: that
// package's Load/mergeFileOntoConfig/ApplyEnv/Validate machinery is
// built around its full 5-section, 16-field Config struct as one unit,
// with no "load just one key" entry point - wiring it in here would
// silently activate the other 15 fields (2 of them, parallel.* and
// cache.l2_*, have observable effects today) for every invocation near a
// discovered file, a separate, larger decision this reader is scoped to
// avoid by construction (decision 5).
type themeFileConfig struct {
	UI struct {
		Theme string `yaml:"theme"`
	} `yaml:"ui"`
}

// resolveThemeFileValue searches searchPaths in order and reads ui.theme
// from the first file that exists there - "first existing file wins",
// matching internal/config.LoadWithSearch's own search-order contract,
// not "first file that happens to set ui.theme": a later path is never
// consulted once an earlier one is found on disk, even if that file is
// unreadable, fails to parse, or simply omits ui.theme.
//
// It never returns a hard error: a missing path, a permission-denied
// read, or malformed YAML all resolve to ("", false, "") - "no file
// value found" - since this tier must never abort an unrelated graft
// command (matching resolveThemeTier's env-tier soft-fail precedent).
// The one case that does produce a non-empty warn is a file that is
// found, parses, and sets ui.theme to a value isValidThemeName rejects:
// that returns ("", false, "<warning>"), the same soft-fail-and-warn
// shape resolveThemeTier already gives an invalid GRAFT_THEME value.
func resolveThemeFileValue(searchPaths []string) (value string, found bool, warn string) {
	for _, path := range searchPaths {
		expanded, err := expandThemeConfigPath(path)
		if err != nil {
			continue
		}
		if _, statErr := os.Stat(expanded); statErr != nil {
			continue // no file at this candidate path; try the next one
		}

		// The first existing candidate stops the search here,
		// regardless of what happens next (decision: "first existing
		// file wins", not "first file with a usable ui.theme").
		data, readErr := os.ReadFile(filepath.Clean(expanded))
		if readErr != nil {
			return "", false, ""
		}
		var cfg themeFileConfig
		if yaml.Unmarshal(data, &cfg) != nil {
			return "", false, ""
		}
		if cfg.UI.Theme == "" {
			return "", false, ""
		}
		if !isValidThemeName(cfg.UI.Theme) {
			return "", false, fmt.Sprintf(
				"Invalid ui.theme value in %s: %q. Must be one of: %s. Using default.",
				expanded, cfg.UI.Theme, knownThemeNamesJoined())
		}
		return cfg.UI.Theme, true, ""
	}
	return "", false, ""
}

// normalizeThemeName returns name unchanged, except for the empty
// string, which becomes themeNameAuto: a zero-value debugUIOptions
// (every existing test's default before this phase) carries Theme ==
// "", and an unset --theme/GRAFT_THEME resolves to "auto" too (see
// resolveThemeTier, main.go), so a session built either way reports the
// same starting theme from `config`/`config theme` (debugSession.themeName).
func normalizeThemeName(name string) string {
	if name == "" {
		return themeNameAuto
	}
	return name
}

// debugColorDisabledNotice is the one-line explanation `config theme
// <name>` prints when the session's color is resolved off: the choice
// is still recorded (a later `config theme` read reflects it, and it
// takes effect immediately if color were ever turned on mid-session,
// which nothing in this release does), but nothing about visible output
// changes, since enablement itself is a startup-only decision (see
// resolveDebugStyler). Without this, switching themes with color off
// looks like a silent no-op.
const debugColorDisabledNotice = "Color is disabled for this session; the theme choice is recorded but has no visible effect."

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
	// or "mono". An unrecognized name and "" both fall back to
	// debugThemeDark, same as "dark" itself; "auto" resolves against
	// DetectedBackground (see resolveDebugThemeFor).
	Theme string
	// DetectedBackground is the terminal background handleDebug already
	// detected before constructing the session (see
	// withDetectedBackground/termbg.Detect), consulted only when Theme
	// resolves to "auto". Its zero value, termbg.Unknown, is exactly
	// right for every caller that never ran detection - an explicit
	// theme, color disabled, a non-terminal writer, or any existing
	// test - so "auto" then falls back to dark, matching
	// resolveDebugTheme's own pre-detection behavior.
	DetectedBackground termbg.Background
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
	return debugStyler{theme: resolveDebugThemeFor(ui.Theme, ui.DetectedBackground), enabled: true}
}

// resolveDebugTheme maps a theme name to its table, treating "auto" the
// same as "dark": callers with a detected background to consult should
// use resolveDebugThemeFor instead. Kept as its own function (rather
// than resolveDebugThemeFor(name, termbg.Unknown)) because callers that
// have no background info at all - and never will, such as a color-off
// session's currentThemeDisplay fallback - read more plainly calling
// this than passing a zero value for a parameter that plays no part in
// their case.
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

// resolveDebugThemeFor resolves name into its theme table exactly as
// resolveDebugTheme does, except for "auto" (and "", which
// normalizeThemeName treats the same way): there, it consults bg -
// termbg.Light resolves the light theme, and termbg.Dark and the zero
// value termbg.Unknown (reported by every session that skipped
// detection: color off, a non-terminal writer, or an explicit theme)
// both resolve dark, the documented fallback. Both the initial
// resolution (resolveDebugStyler, against the background handleDebug
// already detected) and a later `config theme auto`
// (debugSession.cmdConfigTheme, against the session's own cached
// detectedBackground) go through this one function so they can never
// disagree about what "auto" means right now.
func resolveDebugThemeFor(name string, bg termbg.Background) *debugTheme {
	if normalizeThemeName(name) == themeNameAuto {
		if bg == termbg.Light {
			return debugThemeLight
		}
		return debugThemeDark
	}
	return resolveDebugTheme(name)
}

// withDetectedBackground runs termbg.Detect once, before the session
// (and its banner) exist, and returns ui with DetectedBackground filled
// in. It is a no-op copy of ui for every case detection cannot, or need
// not, run: the theme does not resolve to "auto", color would resolve
// off (ansi.ResolveColor, the same precedence resolveDebugStyler itself
// uses, so the two decisions can never disagree), or in/out are not
// both a real *os.File terminal - a bytes.Buffer/strings.Reader test
// session, a piped script, or anything termbg.Detect's own isatty guard
// would report Unknown for anyway. Called once, from handleDebug,
// before newDebugSession and before newDebugLineReader constructs the
// readline instance, so no readline redraw can interleave with the
// query (see plans/debugger-colorizing.md, Background Auto-Detection).
func withDetectedBackground(ui debugUIOptions, in io.Reader, out io.Writer) debugUIOptions {
	if normalizeThemeName(ui.Theme) != themeNameAuto {
		return ui
	}
	if !ansi.ResolveColor(ui.ColorOverride, writerIsTTY(out)) {
		return ui
	}
	inFile, ok := in.(*os.File)
	if !ok {
		return ui
	}
	outFile, ok := out.(*os.File)
	if !ok {
		return ui
	}
	ui.DetectedBackground = termbg.Detect(inFile, outFile)
	return ui
}
