package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// debugColorizeTestFiles mirrors TestDebugREPL's own fixture set
// (debug_repl_test.go), reused here so the color-on assertions below
// exercise real load/step/continue/eval output rather than a
// purpose-built minimal fixture.
var debugColorizeTestFiles = []string{
	"../../assets/history/base.yml",
	"../../assets/history/env.yml",
	"../../assets/history/secrets.yml",
}

// colorOnUI forces color on with the dark theme, independent of the
// writerIsTTY seam - a bytes.Buffer is never a terminal, so an explicit
// ColorOverride is the only way to get a color-on session in a test
// (see resolveDebugStyler).
var colorOnUI = debugUIOptions{ColorOverride: ptrBool(true), Theme: "dark"}

// runDebugSessionWithUI is runDebugSessionWithOpts (debug_repl_test.go)
// with an explicit debugUIOptions, for the color-on rendering tests
// below.
func runDebugSessionWithUI(files []string, opts *mergeOpts, ui debugUIOptions, script string) (stdout string, rc int) {
	var out bytes.Buffer
	rc = handleDebug(files, opts, strings.NewReader(script), &out, ui)
	return out.String(), rc
}

// styled renders text in role's dark-theme style, so the expectations
// below are built from the theme table (per the Test Plan) rather than
// hardcoded escape codes - a palette tweak updates one place.
func styled(role debugRole, text string) string {
	return debugThemeDark.styles[role].Apply(text)
}

func TestDebugColorOnBanner(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "quit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleBanner, "Welcome to the Graft Debugger")) {
		t.Errorf("banner first line not styled roleBanner:\n%s", out)
	}
	if !strings.Contains(out, styled(roleMuted, "Type 'help' for available commands.")) {
		t.Errorf("banner second line not styled roleMuted:\n%s", out)
	}
}

func TestDebugPromptBuilder(t *testing.T) {
	t.Run("color off renders the bare literal prompt unchanged", func(t *testing.T) {
		sess := &debugSession{styler: debugStyler{}}
		if got := debugPromptString(sess); got != "graft> " {
			t.Errorf("debugPromptString() = %q, want %q", got, "graft> ")
		}
	})

	t.Run("color on wraps only graft> in the reserved prompt style, trailing space unstyled", func(t *testing.T) {
		sess := &debugSession{styler: debugStyler{enabled: true, theme: debugThemeDark}}
		want := debugThemeDark.styles[rolePrompt].Apply("graft>") + " "
		if got := debugPromptString(sess); got != want {
			t.Errorf("debugPromptString() = %q, want %q", got, want)
		}
	})
}

func TestDebugColorOnPromptInSession(t *testing.T) {
	// The test harness's `in` is a strings.Reader, never a terminal, so
	// handleDebug always selects the scanner path here (debugLineReader's
	// terminal check needs both ends to be a *os.File) - which is exactly
	// the path the Test Plan calls out: "a piped session with --color on
	// does select the scanner path with an enabled styler, so the scanner
	// prompt must be styled text too".
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "quit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	want := styled(rolePrompt, "graft>") + " "
	if !strings.Contains(out, want) {
		t.Errorf("scanner-path prompt not styled with the reserved prompt role:\n%s", out)
	}
}

func TestDebugColorOnProgress(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "load\nstep\ncontinue\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleHeading, "Loaded 3 documents:")) {
		t.Errorf("load heading not styled roleHeading:\n%s", out)
	}
	if !strings.Contains(out, styled(roleCounter, "[0]")) {
		t.Errorf("load index not styled roleCounter:\n%s", out)
	}
	if !strings.Contains(out, styled(roleFile, "../../assets/history/base.yml")) {
		t.Errorf("load file name not styled roleFile:\n%s", out)
	}
	if !strings.Contains(out, styled(roleMuted, "(2 keys)")) {
		t.Errorf("load key count not styled roleMuted:\n%s", out)
	}
	if !strings.Contains(out, styled(roleCounter, "[1/3]")) {
		t.Errorf("step counter not styled roleCounter:\n%s", out)
	}
	if !strings.Contains(out, styled(roleFile, "../../assets/history/env.yml")) {
		t.Errorf("step file name not styled roleFile:\n%s", out)
	}
	if !strings.Contains(out, styled(roleSuccess, "Evaluation complete.")) {
		t.Errorf("evaluation complete not styled roleSuccess:\n%s", out)
	}
}

func TestDebugColorOnMergeCompleteGuards(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI,
		"load\ncontinue\nstep\ncontinue\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleSuccess, "Merge complete. Nothing more to step.")) {
		t.Errorf("step-after-complete guard not styled roleSuccess:\n%s", out)
	}
	if !strings.Contains(out, styled(roleSuccess, "Merge complete. Nothing more to run.")) {
		t.Errorf("continue-after-complete guard not styled roleSuccess:\n%s", out)
	}
}

func TestDebugColorOnChangeLines(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "load\nstep\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(rolePath, "database.host")) {
		t.Errorf("change path not styled rolePath:\n%s", out)
	}
	if !strings.Contains(out, styled(roleValueOld, "localhost")) {
		t.Errorf("change old value not styled roleValueOld:\n%s", out)
	}
	if !strings.Contains(out, styled(roleMuted, "→")) {
		t.Errorf("change arrow not styled roleMuted:\n%s", out)
	}
	if !strings.Contains(out, styled(roleValueNew, "db.prod.example.com")) {
		t.Errorf("change new value not styled roleValueNew:\n%s", out)
	}
}

func TestDebugColorOnDiff(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "load\ncontinue\ndiff\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleHeading, "Changes from")) {
		t.Errorf("diff heading not styled roleHeading:\n%s", out)
	}
	if !strings.Contains(out, styled(roleFile, "../../assets/history/base.yml")) {
		t.Errorf("diff base file not styled roleFile:\n%s", out)
	}
	// database.password goes from <none> to a value: the <none> side
	// must render roleMuted, never roleValueOld, per Category E.
	if !strings.Contains(out, styled(roleMuted, "<none>")) {
		t.Errorf("<none> not styled roleMuted:\n%s", out)
	}
	if strings.Contains(out, styled(roleValueOld, "<none>")) {
		t.Errorf("<none> must never render in roleValueOld:\n%s", out)
	}
}

func TestDebugColorOnBreakpoints(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, strings.Join([]string{
		"load",
		"break database.pool_size",
		"continue",
		"breaks",
		"unbreak database.pool_size",
		"breaks",
		"quit",
	}, "\n")+"\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleSuccess, "Breakpoint set on database.pool_size")) {
		t.Errorf("break confirmation not styled roleSuccess:\n%s", out)
	}
	if !strings.Contains(out, styled(roleBreak, "Breakpoint hit:")) {
		t.Errorf("breakpoint-hit label not styled roleBreak:\n%s", out)
	}
	if !strings.Contains(out, styled(rolePath, "database.pool_size")) {
		t.Errorf("breakpoint-hit path not styled rolePath:\n%s", out)
	}
	if !strings.Contains(out, styled(roleValueNew, "50")) {
		t.Errorf("breakpoint Current value not styled roleValueNew:\n%s", out)
	}
	if !strings.Contains(out, styled(roleHeading, "Breakpoints:")) {
		t.Errorf("breakpoints heading not styled roleHeading:\n%s", out)
	}
	if !strings.Contains(out, styled(roleSuccess, "Breakpoint removed")) {
		t.Errorf("unbreak confirmation not styled roleSuccess:\n%s", out)
	}
	if !strings.Contains(out, styled(roleMuted, "No breakpoints set.")) {
		t.Errorf("empty-breakpoints message not styled roleMuted:\n%s", out)
	}
}

func TestDebugColorOnDeferred(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, strings.Join([]string{
		"load",
		"defer database.password",
		"inspect",
		"autodefer",
		"quit",
	}, "\n")+"\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleSuccess, "Marked database.password for deferred evaluation")) {
		t.Errorf("defer confirmation not styled roleSuccess:\n%s", out)
	}
	if !strings.Contains(out, styled(roleHeading, "Deferred 1 path:")) {
		t.Errorf("deferred heading not styled roleHeading:\n%s", out)
	}
	if !strings.Contains(out, styled(rolePath, "database.password")) {
		t.Errorf("deferred path not styled rolePath:\n%s", out)
	}
	if !strings.Contains(out, styled(roleMuted, "Autodefer: no failing operators - nothing to defer.")) {
		t.Errorf("autodefer no-op message not styled roleMuted:\n%s", out)
	}
}

func TestDebugColorOnAutodeferSummary(t *testing.T) {
	cascadeFiles := []string{"../../assets/skip-defer/transitive-grab.yml"}
	out, rc := runDebugSessionWithUI(cascadeFiles, &mergeOpts{}, colorOnUI, "load\nautodefer\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleHeading, "Autodefer: 1 key deferred:")) {
		t.Errorf("autodefer summary heading not styled roleHeading:\n%s", out)
	}
	if !strings.Contains(out, styled(rolePath, "$.meta.password")) {
		t.Errorf("autodefer deferred path not styled rolePath:\n%s", out)
	}
}

func TestDebugColorOnEval(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI,
		"load\ndefer database.password\ncontinue\neval database.password\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleHeading, "Evaluating:")) {
		t.Errorf("Evaluating: label not styled roleHeading:\n%s", out)
	}
	if !strings.Contains(out, styled(roleSuccess, "Result:")) {
		t.Errorf("Result: label not styled roleSuccess:\n%s", out)
	}
}

// TestDebugColorOnConfigStaysPlain locks decision 12: with color on, no
// arm of `config` styles a value - the bare listing, the single-key
// "Current:" line, or the credential key itself - because vault.token's
// value is a live credential. The whole rendered line, not a substring,
// is asserted escape-free, per the Test Plan's credential-guard case.
func TestDebugColorOnConfigStaysPlain(t *testing.T) {
	restoreToken, hadToken := os.LookupEnv("VAULT_TOKEN")
	restoreAddr, hadAddr := os.LookupEnv("VAULT_ADDR")
	defer func() {
		if hadToken {
			_ = os.Setenv("VAULT_TOKEN", restoreToken)
		} else {
			_ = os.Unsetenv("VAULT_TOKEN")
		}
		// The script below runs `config vault.addr ...`, which sets
		// VAULT_ADDR for the rest of the process (cmdConfig's default
		// arm calls os.Setenv directly) - restore it, or a later test
		// in this same binary that expects VAULT_ADDR unset (and so a
		// client-initialization failure, not a real DNS lookup) would
		// flake against whatever network the test host has.
		if hadAddr {
			_ = os.Setenv("VAULT_ADDR", restoreAddr)
		} else {
			_ = os.Unsetenv("VAULT_ADDR")
		}
	}()
	_ = os.Setenv("VAULT_TOKEN", "s.super-secret-token")

	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, strings.Join([]string{
		"config",
		"config vault.token",
		"config vault.addr https://vault-dev.example.com",
		"config bogus.key",
		"quit",
	}, "\n")+"\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}

	for _, line := range strings.Split(out, "\n") {
		plain := strings.HasPrefix(line, "vault.addr:") ||
			strings.HasPrefix(line, "vault.token:") ||
			strings.HasPrefix(line, "vault.namespace:") ||
			strings.HasPrefix(line, "Current:")
		if plain && strings.ContainsRune(line, '\x1b') {
			t.Errorf("config value line contains an escape byte: %q", line)
		}
	}
	if !strings.Contains(out, "s.super-secret-token") {
		t.Fatalf("test setup: the credential never appeared in output at all:\n%s", out)
	}
	if !strings.Contains(out, styled(roleSuccess, "Updated vault.addr")) {
		t.Errorf("Updated confirmation not styled roleSuccess:\n%s", out)
	}
	if !strings.Contains(out, styled(roleWarn, "Unknown config key: bogus.key. Known keys: vault.addr, vault.token, vault.namespace")) {
		t.Errorf("unknown config key message not styled roleWarn:\n%s", out)
	}
}

func TestDebugColorOnPruneReport(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{Prune: []string{"database.port"}}, colorOnUI,
		"load\nprune-report\ncontinue\nprune-report\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleWarn, "Merge not complete yet. Run 'continue' (or enough 'step's) before 'prune-report'.")) {
		t.Errorf("not-complete-yet message not styled roleWarn:\n%s", out)
	}
	if !strings.Contains(out, styled(roleHeading, "Paths --prune/--cherry-pick would remove (not applied to 'output'/'export'/'history'):")) {
		t.Errorf("prune-report heading not styled roleHeading:\n%s", out)
	}
	if !strings.Contains(out, styled(rolePath, "database.port")) {
		t.Errorf("prune-report path not styled rolePath:\n%s", out)
	}
}

func TestDebugColorOnPruneReportNothingToRemove(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "load\ncontinue\nprune-report\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleMuted, "No --prune/--cherry-pick flags were given for this session.")) {
		t.Errorf("no-flags message not styled roleMuted:\n%s", out)
	}
}

func TestDebugColorOnExport(t *testing.T) {
	dir := t.TempDir()
	target := dir + "/out.yml"
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI,
		fmt.Sprintf("load\ncontinue\nexport %s\nquit\n", target))
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleSuccess, "Exported to")) {
		t.Errorf("Exported to label not styled roleSuccess:\n%s", out)
	}
	if !strings.Contains(out, styled(roleFile, target)) {
		t.Errorf("exported file path not styled roleFile:\n%s", out)
	}
}

func TestDebugColorOnHelp(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "help\nhelp bogus\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleHeading, "Available commands:")) {
		t.Errorf("help heading not styled roleHeading:\n%s", out)
	}
	if !strings.Contains(out, styled(roleCommand, fmt.Sprintf("%-15s", "load"))) {
		t.Errorf("help command name not styled roleCommand:\n%s", out)
	}
	if !strings.Contains(out, styled(roleWarn, `No help available for "bogus".`)) {
		t.Errorf("unknown-help message not styled roleWarn:\n%s", out)
	}
}

func TestDebugColorOnUnknownCommand(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, "frobnicate\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleWarn, "Unknown command: frobnicate. Type 'help' for available commands.")) {
		t.Errorf("unknown-command message not styled roleWarn:\n%s", out)
	}
}

func TestDebugColorOnGuardMessages(t *testing.T) {
	out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI, strings.Join([]string{
		"step",
		"break",
		"unbreak",
		"defer",
		"export",
		"load",
		"inspect no.such.path",
		"eval no.such.path",
		"quit",
	}, "\n")+"\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, styled(roleWarn, "No documents loaded. Run 'load' first.")) {
		t.Errorf("not-loaded guard not styled roleWarn:\n%s", out)
	}
	if !strings.Contains(out, styled(roleWarn, "Usage: break <path>")) {
		t.Errorf("break usage not styled roleWarn:\n%s", out)
	}
	if !strings.Contains(out, styled(roleWarn, "Usage: unbreak <path>")) {
		t.Errorf("unbreak usage not styled roleWarn:\n%s", out)
	}
	if !strings.Contains(out, styled(roleWarn, "Usage: defer <path>")) {
		t.Errorf("defer usage not styled roleWarn:\n%s", out)
	}
	if !strings.Contains(out, styled(roleWarn, "Usage: export <file>")) {
		t.Errorf("export usage not styled roleWarn:\n%s", out)
	}
	if !strings.Contains(out, styled(roleWarn, "Path not found: no.such.path")) {
		t.Errorf("path-not-found guard not styled roleWarn:\n%s", out)
	}
}

// TestDebugColorOnErrors locks Category P's role migration (Error-Site
// Migration, plans/debugger-colorizing.md): every stdout error site's
// literal label renders roleError through the per-session styler, the
// colon after it stays outside any styled span, and - for the
// two-argument shape - the interpolated file name gets roleFile. The
// error-derived text itself carries no role styling at all (only the
// decision-13 strip wraps it), so it never matches roleError's own
// escape sequence.
func TestDebugColorOnErrors(t *testing.T) {
	t.Run("one-argument shape: a merge failure styles only its label", func(t *testing.T) {
		goPatchFiles := []string{"../../assets/history/base.yml", "../../assets/vaultinfo/go-patch.yml"}
		out, rc := runDebugSessionWithUI(goPatchFiles, &mergeOpts{}, colorOnUI, "load\nstep\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if !strings.Contains(out, styled(roleError, "Merge failed")+": ") {
			t.Errorf("Merge failed label not styled roleError with an unstyled colon after it:\n%s", out)
		}
		if strings.Contains(out, styled(roleError, "root of YAML document is not a hash/map")) {
			t.Errorf("error-derived text must never carry roleError styling:\n%s", out)
		}
	})

	t.Run("one-argument shape: an evaluation failure styles only its label", func(t *testing.T) {
		evalFailureFiles := []string{"../../assets/debug/eval-failure.yml"}
		out, rc := runDebugSessionWithUI(evalFailureFiles, &mergeOpts{}, colorOnUI, "load\ncontinue\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if !strings.Contains(out, styled(roleError, "Evaluation failed")+": ") {
			t.Errorf("Evaluation failed label not styled roleError:\n%s", out)
		}
	})

	t.Run("one-argument shape: error computing history styles only its label", func(t *testing.T) {
		deferredHistoryFiles := []string{"../../assets/debug/deferred-history-base.yml", "../../assets/debug/deferred-history-override.yml"}
		out, rc := runDebugSessionWithUI(deferredHistoryFiles, &mergeOpts{}, colorOnUI, "load\nhistory meta.replicas\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if !strings.Contains(out, styled(roleError, "Error computing history")+": ") {
			t.Errorf("Error computing history label not styled roleError:\n%s", out)
		}
	})

	t.Run("one-argument shape: autodefer failed styles only its label", func(t *testing.T) {
		out, rc := runDebugSessionWithUI([]string{"../../assets/skip-defer/cycle.yml"}, &mergeOpts{}, colorOnUI, "load\nautodefer\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		if !strings.Contains(out, styled(roleError, "Autodefer failed")+": ") {
			t.Errorf("Autodefer failed label not styled roleError:\n%s", out)
		}
	})

	t.Run("two-argument shape: error loading styles the label roleError and the file roleFile", func(t *testing.T) {
		out, rc := runDebugSessionWithUI([]string{"../../assets/vaultinfo/go-patch.yml"}, &mergeOpts{}, colorOnUI, "load\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		want := styled(roleError, "Error loading") + " " + styled(roleFile, "../../assets/vaultinfo/go-patch.yml") + ": "
		if !strings.Contains(out, want) {
			t.Errorf("Error loading not rendered as roleError label + roleFile path:\n%s", out)
		}
	})

	t.Run("two-argument shape: error writing styles the label roleError and the file roleFile", func(t *testing.T) {
		dir := t.TempDir()
		target := dir + "/missing-subdir/out.yml"
		out, rc := runDebugSessionWithUI(debugColorizeTestFiles, &mergeOpts{}, colorOnUI,
			fmt.Sprintf("load\ncontinue\nexport %s\nquit\n", target))
		if rc != 0 {
			t.Fatalf("rc = %d, want 0", rc)
		}
		want := styled(roleError, "Error writing") + " " + styled(roleFile, target) + ": "
		if !strings.Contains(out, want) {
			t.Errorf("Error writing not rendered as roleError label + roleFile path:\n%s", out)
		}
	})
}

// paddedSourceColumn pads s to sourceColumnWidth the same way
// historyEntryLine/writeHistoryFinalLine (merge_history.go) and
// cmdHistory's own pad-then-style formatting do, so these tests build
// their expectations from the one shared width constant rather than a
// hardcoded column count.
func paddedSourceColumn(s string) string {
	return fmt.Sprintf("%-*s", sourceColumnWidth, s)
}

// TestDebugColorOnHistory locks Category H (the History Display section
// of the plan of record): the source column (both a "[N] file" entry
// and the trailing "Final" row) styles roleFile *after* padding to
// sourceColumnWidth, so escape bytes never count toward the column's
// alignment; the arrow between source and value is always roleMuted; an
// ordinary entry's value stays unstyled ("values default"); and the
// trailing Final row's real value styles roleValueNew.
func TestDebugColorOnHistory(t *testing.T) {
	files := []string{"../../assets/history/base.yml", "../../assets/history/env.yml"}
	out, rc := runDebugSessionWithUI(files, &mergeOpts{}, colorOnUI, "load\nhistory database.pool_size\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}

	if !strings.Contains(out, styled(rolePath, "database.pool_size")+":\n") {
		t.Errorf("history path header not styled rolePath:\n%s", out)
	}

	wantEntrySource := styled(roleFile, paddedSourceColumn("[0] ../../assets/history/base.yml"))
	if !strings.Contains(out, wantEntrySource) {
		t.Errorf("entry source column not styled roleFile with pad-then-style alignment:\n%s", out)
	}

	if !strings.Contains(out, styled(roleMuted, "→")) {
		t.Errorf("history arrow not styled roleMuted:\n%s", out)
	}

	// The unchanged entry value ("10") must never carry roleValueNew - only
	// the trailing Final row's value does.
	if strings.Contains(out, styled(roleValueNew, "10")) {
		t.Errorf("ordinary entry value must stay unstyled (values default):\n%s", out)
	}

	wantFinalSource := styled(roleFile, paddedSourceColumn("Final"))
	if !strings.Contains(out, wantFinalSource) {
		t.Errorf("Final row's source column not styled roleFile with pad-then-style alignment:\n%s", out)
	}
	if !strings.Contains(out, styled(roleValueNew, "50")) {
		t.Errorf("Final row's current value not styled roleValueNew:\n%s", out)
	}
}

// TestDebugColorOnHistoryPruned locks the "<pruned>" half of Category
// H: a genuinely Removed entry (history.Entry.Removed, set by an
// operator (( prune )) marker here, independent of any CLI flag) and an
// unavailable Final both style their "<pruned>" value roleMuted, and
// their source column still styles roleFile like any other row.
func TestDebugColorOnHistoryPruned(t *testing.T) {
	// No 'continue': cmdHistory always recomputes the full sequence
	// (buildMergeHistorySteps with limit -1) regardless of the session's
	// own step position, and running 'continue' first would print
	// Category E change lines that share roleValueNew/roleMuted with
	// this test's own history assertions, which would let a false
	// positive slip through if cmdHistory itself styled nothing yet.
	pruneFiles := []string{"../../assets/history/prune-marker.yml", "../../assets/history/prune-override.yml"}
	out, rc := runDebugSessionWithUI(pruneFiles, &mergeOpts{}, colorOnUI, "load\nhistory secret\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}

	// The removed entry's index is 2: [0] is the base file, [1] is the
	// override file (neither changes "secret" itself), and the operator
	// (( prune )) removal shows up as the synthetic "<evaluated>" step,
	// index 2 (see internal/history.Track).
	if !strings.Contains(out, styled(roleFile, paddedSourceColumn("[2] <pruned>"))) {
		t.Errorf("removed entry's source column not styled roleFile:\n%s", out)
	}
	if !strings.Contains(out, styled(roleMuted, "<pruned>")) {
		t.Errorf("removed entry's value not styled roleMuted:\n%s", out)
	}
	if !strings.Contains(out, styled(roleFile, paddedSourceColumn("Final"))+" "+styled(roleMuted, "→")+" "+styled(roleMuted, "<pruned>")) {
		t.Errorf("Final row for an unavailable path not styled roleFile source + roleMuted arrow + roleMuted value:\n%s", out)
	}
}

// TestDebugColorOnHistoryNoReparsing proves decision 9's "no
// rendered-line re-parsing" guarantee: cmdHistory decides styling from
// history.Entry/PathHistory's own fields (Removed, FinalOK), never by
// inspecting the formatted text, so a value that is itself the literal
// string "<pruned>" or contains a literal "→" is never mistaken for the
// real separator or a real removal.
func TestDebugColorOnHistoryNoReparsing(t *testing.T) {
	// No 'continue': see the comment in TestDebugColorOnHistoryPruned.
	// Here it matters even more, since 'continue' would print Category
	// E change lines for these same two paths, and those change lines
	// legitimately use roleValueNew for "a → b"/"<pruned>" as the *new*
	// side of a change - exactly the false positive this test exists to
	// rule out for cmdHistory's own, independently-decided styling.
	files := []string{
		"../../assets/debug/history-color-tricky-base.yml",
		"../../assets/debug/history-color-tricky-override.yml",
	}
	out, rc := runDebugSessionWithUI(files, &mergeOpts{}, colorOnUI, "load\nhistory note\nhistory label\nquit\n")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}

	// "note" goes plain -> "a → b": the literal arrow inside the value
	// must never be styled roleMuted (only the real column separator is).
	if strings.Contains(out, styled(roleMuted, "a → b")) {
		t.Errorf("a value containing a literal arrow must not be styled as the separator:\n%s", out)
	}
	if !strings.Contains(out, styled(roleValueNew, "a → b")) {
		t.Errorf("Final row's real value (which happens to contain an arrow) not styled roleValueNew:\n%s", out)
	}

	// "label" goes initial -> "<pruned>" (a real, non-removed value equal
	// to the sentinel text): the entry must stay unstyled, and Final must
	// style it roleValueNew, never roleMuted - roleMuted is reserved for a
	// genuine Removed/FinalOK-false row, not this row's text content.
	if strings.Contains(out, styled(roleMuted, "<pruned>")) {
		t.Errorf("a value that is merely the text <pruned> must not style roleMuted:\n%s", out)
	}
	if !strings.Contains(out, styled(roleValueNew, "<pruned>")) {
		t.Errorf("Final row's real value (the literal text <pruned>) not styled roleValueNew:\n%s", out)
	}
}
