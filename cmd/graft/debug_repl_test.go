package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/log"
)

// runDebugSession runs `graft debug` against files with script (one REPL
// command per line) as stdin, and returns stdout and the process exit code
// handleDebug would report.
func runDebugSession(files []string, script string) (stdout string, rc int) {
	return runDebugSessionWithOpts(files, &mergeOpts{}, script)
}

// runDebugSessionWithOpts is runDebugSession with caller-supplied mergeOpts
// (e.g. EnableGoPatch/FallbackAppend), for tests that need those flags
// threaded through the session's own merge calls (F14).
func runDebugSessionWithOpts(files []string, opts *mergeOpts, script string) (stdout string, rc int) {
	var out bytes.Buffer
	rc = handleDebug(files, opts, strings.NewReader(script), &out, debugUIOptions{})
	return out.String(), rc
}

// TestDebugREPL locks `graft debug` (docs/user-guide/cli/debug.md) against
// assets/history/{base,env,secrets}.yml - the same fixture B2's merge
// --history tests use, chosen there to exercise every history.Phase, which
// is equally useful here: base.yml/env.yml give `step`/`continue` a real
// LOAD-then-MERGE transition to show, and secrets.yml's `(( grab
// meta.version ))` gives `defer`/`eval` a real unevaluated operator to work
// with.
func TestDebugREPL(t *testing.T) {
	files := []string{"../../assets/history/base.yml", "../../assets/history/env.yml", "../../assets/history/secrets.yml"}
	// deferredHistoryFiles carries an unfilled (( param )) whose evaluation
	// always fails, with no external service involved, so it exercises how
	// `history` behaves when the document holds an operator the session
	// cannot resolve.
	deferredHistoryFiles := []string{"../../assets/debug/deferred-history-base.yml", "../../assets/debug/deferred-history-override.yml"}

	Convey("graft debug", t, func() {
		Convey("load reports every file with its own top-level key count", func() {
			out, rc := runDebugSession(files, "load\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Welcome to the Graft Debugger")
			So(out, ShouldContainSubstring, "Loaded 3 documents:\n")
			So(out, ShouldContainSubstring, "[0] ../../assets/history/base.yml (2 keys)\n")
			So(out, ShouldContainSubstring, "[1] ../../assets/history/env.yml (2 keys)\n")
			So(out, ShouldContainSubstring, "[2] ../../assets/history/secrets.yml (1 key)\n")
		})

		Convey("step merges exactly one more file and reports what changed", func() {
			out, rc := runDebugSession(files, "load\nstep\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "[1/3] Merging ../../assets/history/env.yml...\n")
			So(out, ShouldContainSubstring, "database.host: localhost → db.prod.example.com")
			So(out, ShouldContainSubstring, "database.pool_size: 10 → 50")
			// Only one step ran: secrets.yml (step 2) hasn't merged yet.
			So(out, ShouldNotContainSubstring, "secrets.yml...")
		})

		Convey("step before load is a no-op with a helpful message", func() {
			out, rc := runDebugSession(files, "step\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "No documents loaded. Run 'load' first.")
		})

		Convey("continue runs every remaining step, including evaluation", func() {
			out, rc := runDebugSession(files, "load\ncontinue\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "[1/3] Merging ../../assets/history/env.yml...")
			So(out, ShouldContainSubstring, "[2/3] Merging ../../assets/history/secrets.yml...")
			So(out, ShouldContainSubstring, "database.password: <none> → (( grab meta.version ))")
			So(out, ShouldContainSubstring, "[3/3] Evaluating operators...")
			So(out, ShouldContainSubstring, "Evaluation complete.")
		})

		Convey("continue stops at a breakpoint instead of running to completion", func() {
			out, rc := runDebugSession(files, "load\nbreak database.pool_size\ncontinue\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Breakpoint set on database.pool_size")
			So(out, ShouldContainSubstring, "Breakpoint hit: database.pool_size\n  Current: 50\n")
			// Stopped after step 1: evaluation never ran.
			So(out, ShouldNotContainSubstring, "Evaluating operators")
			So(out, ShouldNotContainSubstring, "Evaluation complete")
		})

		Convey("break/breaks/unbreak manage the breakpoint set", func() {
			out, rc := runDebugSession(files, strings.Join([]string{
				"break database.host",
				"break database.password",
				"breaks",
				"unbreak database.host",
				"breaks",
				"quit",
			}, "\n")+"\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Breakpoint set on database.host\n")
			So(out, ShouldContainSubstring, "Breakpoint set on database.password\n")
			So(out, ShouldContainSubstring, "Breakpoints:\n  - database.host\n  - database.password\n")
			So(out, ShouldContainSubstring, "Breakpoint removed\n")
			So(out, ShouldContainSubstring, "Breakpoints:\n  - database.password\n")
		})

		Convey("unbreak on a path with no breakpoint reports that instead of erroring", func() {
			out, rc := runDebugSession(files, "unbreak nope.path\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "No breakpoint on nope.path\n")
		})

		Convey("breaks with none set says so", func() {
			out, rc := runDebugSession(files, "breaks\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "No breakpoints set.\n")
		})

		Convey("inspect shows the current value at a path, reflecting session progress", func() {
			out, rc := runDebugSession(files, "load\ninspect database.host\nstep\ninspect database.host\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "localhost\n")
			So(out, ShouldContainSubstring, "db.prod.example.com\n")
		})

		Convey("inspect on a path that doesn't exist reports that", func() {
			out, rc := runDebugSession(files, "load\ninspect no.such.path\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Path not found: no.such.path\n")
		})

		Convey("history shows the same per-file entries merge --history would for that path", func() {
			out, rc := runDebugSession(files, "load\nhistory database.pool_size\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "database.pool_size:\n")
			So(out, ShouldContainSubstring, "10")
			So(out, ShouldContainSubstring, "50")
			So(out, ShouldContainSubstring, "Final")
		})

		Convey("history without defer still reports an unresolvable operator's error", func() {
			out, rc := runDebugSession(deferredHistoryFiles, "load\nhistory meta.replicas\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Error computing history")
			So(out, ShouldContainSubstring, "please set meta.environment")
		})

		Convey("history honors the session's deferred paths, as step and continue do", func() {
			out, rc := runDebugSession(deferredHistoryFiles, strings.Join([]string{
				"load",
				"defer meta.environment",
				"history meta.replicas",
				"quit",
				"",
			}, "\n"))
			So(rc, ShouldEqual, 0)
			// The deferred param no longer aborts the whole recompute, so an
			// unrelated path reports its real per-file entries.
			So(out, ShouldNotContainSubstring, "Error computing history")
			So(out, ShouldContainSubstring, "meta.replicas:\n")
			So(out, ShouldContainSubstring, "2")
			So(out, ShouldContainSubstring, "6")
			So(out, ShouldContainSubstring, "Final")
		})

		Convey("history on a path with no recorded history says so", func() {
			out, rc := runDebugSession(files, "load\nhistory no.such.path\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "No history recorded for path: no.such.path\n")
		})

		Convey("defer leaves a path's operator unevaluated through continue, and eval force-resolves it", func() {
			out, rc := runDebugSession(files, strings.Join([]string{
				"load",
				"defer database.password",
				"continue",
				"inspect database.password",
				"eval database.password",
				"inspect database.password",
				"quit",
			}, "\n")+"\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Marked database.password for deferred evaluation\n")
			So(out, ShouldContainSubstring, "Evaluation complete.")

			// After continue: still the raw operator text (deferred).
			_, afterContinue, found := strings.Cut(out, "Evaluation complete.")
			So(found, ShouldBeTrue)
			So(afterContinue, ShouldContainSubstring, `(( grab meta.version ))`)

			So(out, ShouldContainSubstring, "Evaluating: (( grab meta.version ))\n")
			So(out, ShouldContainSubstring, "Result: \"1.0\"\n")

			// The final inspect (after eval) shows the resolved value,
			// proving eval splices its result back into the session tree
			// rather than only printing it. Slice past the "Result:" line
			// itself, or the assertion matches that line and passes even
			// when the splice never happens.
			resultLine := "Result: \"1.0\"\n"
			afterEval := out[strings.LastIndex(out, resultLine)+len(resultLine):]
			So(afterEval, ShouldContainSubstring, "\"1.0\"")
			So(afterEval, ShouldNotContainSubstring, "(( grab meta.version ))")
		})

		Convey("a REPL line too long for the scanner is reported, not silently dropped", func() {
			var out bytes.Buffer
			var stderr string
			originalPrintStdErrf := log.PrintStdErrf
			log.PrintStdErrf = func(format string, args ...interface{}) {
				stderr += fmt.Sprintf(format, args...)
			}
			defer func() { log.PrintStdErrf = originalPrintStdErrf }()

			// bufio.Scanner's default buffer is 64KiB; a longer line makes
			// Scan return false exactly as a clean EOF does.
			script := "inspect " + strings.Repeat("x", 70000) + "\nload\nquit\n"
			rc := handleDebug([]string{"../../assets/history/base.yml"}, &mergeOpts{}, strings.NewReader(script), &out, debugUIOptions{})

			So(rc, ShouldEqual, 2)
			So(stderr, ShouldContainSubstring, "Error reading debugger input")
			// The commands after the over-long line never ran; the point is
			// that this is reported rather than looking like a normal exit.
			So(out.String(), ShouldNotContainSubstring, "Loaded 1 document")
		})

		Convey("config with no args lists known keys; config <key> reads one; config <key> <value> sets it for the session", func() {
			restore := os.Getenv("VAULT_ADDR")
			defer func() { _ = os.Setenv("VAULT_ADDR", restore) }()
			_ = os.Unsetenv("VAULT_ADDR")

			out, rc := runDebugSession(files, strings.Join([]string{
				"config",
				"config vault.addr",
				"config vault.addr https://vault-dev.example.com",
				"config vault.addr",
				"quit",
			}, "\n")+"\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "vault.addr: (not set)\n")
			So(out, ShouldContainSubstring, "vault.token: (not set)\n")
			So(out, ShouldContainSubstring, "vault.namespace: (not set)\n")
			So(out, ShouldContainSubstring, "Current: (not set)\n")
			So(out, ShouldContainSubstring, "Updated vault.addr\n")
			So(out, ShouldContainSubstring, "Current: https://vault-dev.example.com\n")
			So(os.Getenv("VAULT_ADDR"), ShouldEqual, "https://vault-dev.example.com")
		})

		Convey("config on an unknown key reports the known keys instead of silently no-oping", func() {
			out, rc := runDebugSession(files, "config bogus.key\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Unknown config key: bogus.key")
			So(out, ShouldContainSubstring, "vault.addr")
		})

		Convey("output prints the current document as YAML", func() {
			out, rc := runDebugSession(files, "load\ncontinue\noutput\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "database:\n")
			So(out, ShouldContainSubstring, "host: db.prod.example.com\n")
			So(out, ShouldContainSubstring, `password: "1.0"`)
		})

		Convey("history and output agree on an operator (( prune )) marker: both show it removed", func() {
			pruneFiles := []string{"../../assets/history/prune-marker.yml", "../../assets/history/prune-override.yml"}
			out, rc := runDebugSession(pruneFiles, "load\ncontinue\noutput\nhistory secret\nquit\n")
			So(rc, ShouldEqual, 0)

			// output: an operator (( prune )) is unconditional, applied by
			// the engine itself independent of any CLI flag, so it is gone
			// from the tree by the final step even though output otherwise
			// shows the pre-(--prune-flag) document. Scope this assertion to
			// the output section only (before the later 'history' command's
			// own "secret:" header line).
			historyHeaderStart := strings.Index(out, "graft> secret:")
			So(historyHeaderStart, ShouldBeGreaterThanOrEqualTo, 0)
			outputSection := out[:historyHeaderStart]
			So(outputSection, ShouldContainSubstring, "database:\n  host: db.prod.example.com\n")
			So(outputSection, ShouldNotContainSubstring, "secret:")

			// history: the same removal, reported as a proper entry rather
			// than a bare absence.
			So(out, ShouldContainSubstring, "secret:\n")
			So(out, ShouldContainSubstring, "→ (( prune ))\n")
			So(out, ShouldContainSubstring, "Final              → <pruned>\n")
		})

		Convey("history and output agree under the --prune flag: both stay on the pre-prune tree while stepping", func() {
			out, rc := runDebugSessionWithOpts(files, &mergeOpts{Prune: []string{"database.port"}},
				"load\ncontinue\noutput\nhistory database.port\nquit\n")
			So(rc, ShouldEqual, 0)
			// --prune is a CLI flag, not an operator marker: output never
			// applies it, so database.port survives to 'output'.
			So(out, ShouldContainSubstring, "port: 5432\n")
			// history must show the same survival, not a spurious removal.
			So(out, ShouldContainSubstring, "database.port:\n")
			So(out, ShouldNotContainSubstring, "<pruned>")
			So(out, ShouldContainSubstring, "Final              → 5432")
		})

		Convey("prune-report before the merge completes tells the user to finish stepping first", func() {
			out, rc := runDebugSessionWithOpts(files, &mergeOpts{Prune: []string{"database.port"}},
				"load\nprune-report\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Merge not complete yet.")
		})

		Convey("prune-report with no --prune/--cherry-pick flags says so", func() {
			out, rc := runDebugSession(files, "load\ncontinue\nprune-report\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "No --prune/--cherry-pick flags were given for this session.\n")
		})

		Convey("prune-report, once the merge completes, reports what --prune would remove without touching output", func() {
			out, rc := runDebugSessionWithOpts(files, &mergeOpts{Prune: []string{"database.port"}},
				"load\ncontinue\noutput\nprune-report\nquit\n")
			So(rc, ShouldEqual, 0)
			// output is unaffected by the flag.
			So(out, ShouldContainSubstring, "port: 5432\n")
			// prune-report names the path the flag would remove.
			So(out, ShouldContainSubstring, "Paths --prune/--cherry-pick would remove")
			So(out, ShouldContainSubstring, "  - database.port\n")
		})

		Convey("autodefer defers the root chain only, and output/export/history agree afterward", func() {
			cascadeFiles := []string{"../../assets/skip-defer/transitive-grab.yml"}
			out, rc := runDebugSession(cascadeFiles, strings.Join([]string{
				"load",
				"autodefer",
				"output",
				"history meta.password",
				"quit",
			}, "\n")+"\n")
			So(rc, ShouldEqual, 0)

			// Summary: exactly the root vault failure, not its grab
			// dependent - the cascade is not double-reported.
			So(out, ShouldContainSubstring, "Autodefer: 1 key deferred:\n")
			So(out, ShouldContainSubstring, "deferred $.meta.password: ")
			So(out, ShouldContainSubstring, "Vault client initialization")
			So(out, ShouldNotContainSubstring, "database.password")

			// output: both the root and its grab dependent still carry the
			// deferred expression, not a resolved value or an error.
			historyHeaderStart := strings.Index(out, "graft> meta.password:")
			So(historyHeaderStart, ShouldBeGreaterThanOrEqualTo, 0)
			outputSection := out[:historyHeaderStart]
			So(outputSection, ShouldContainSubstring, `password: (( vault "secret/db:password" ))`)
			count := strings.Count(outputSection, `(( vault "secret/db:password" ))`)
			So(count, ShouldEqual, 2) // meta.password and database.password (the grab copy)

			// history: agrees, Final is the same deferred expression, not
			// <pruned> and not an error.
			So(out, ShouldContainSubstring, "meta.password:\n")
			So(out, ShouldContainSubstring, `Final              → (( vault "secret/db:password" ))`)
			So(out, ShouldNotContainSubstring, "<pruned>")
		})

		Convey("autodefer with nothing failing is a no-op with a clear message", func() {
			out, rc := runDebugSession(files, "load\ncontinue\nautodefer\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Autodefer: no failing operators - nothing to defer.\n")
		})

		Convey("autodefer composes with a prior manual defer: an already-deferred path is protected, not re-attempted or re-reported", func() {
			cascadeFiles := []string{"../../assets/skip-defer/transitive-grab.yml"}
			out, rc := runDebugSession(cascadeFiles, strings.Join([]string{
				"load",
				"defer meta.password",
				"autodefer",
				"inspect",
				"quit",
			}, "\n")+"\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Marked meta.password for deferred evaluation\n")
			// Nothing new failed: the manual defer already protected the
			// only failing path, so autodefer's own loop finds no new
			// failure to report.
			So(out, ShouldContainSubstring, "Autodefer: no failing operators - nothing to defer.\n")
			So(out, ShouldNotContainSubstring, "Autodefer: 1 key deferred")
			// The manual defer entry survives untouched (no reason, since
			// autodefer never re-attributed it).
			So(out, ShouldContainSubstring, "Deferred 1 path:\n  - meta.password\n")
		})

		Convey("autodefer on a genuine cycle is a hard failure, keeping the original error and leaving the session untouched", func() {
			cycleFiles := []string{"../../assets/skip-defer/cycle.yml"}
			out, rc := runDebugSession(cycleFiles, strings.Join([]string{
				"load",
				"autodefer",
				"inspect",
				"quit",
			}, "\n")+"\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Autodefer failed")
			So(out, ShouldContainSubstring, "cycle detected")
			// The session's tree is unchanged (still the raw, unevaluated
			// merge) and nothing was recorded as deferred.
			So(out, ShouldContainSubstring, "a: (( grab b ))")
			So(out, ShouldContainSubstring, "b: (( grab a ))")
			So(out, ShouldNotContainSubstring, "Deferred")
		})

		Convey("inspect lists deferred paths with reasons where known, manual and autodefer alike", func() {
			cascadeFiles := []string{"../../assets/skip-defer/transitive-grab.yml"}
			out, rc := runDebugSession(cascadeFiles, strings.Join([]string{
				"load",
				"defer database.password",
				"autodefer",
				"inspect",
				"quit",
			}, "\n")+"\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Deferred 2 paths:\n")
			// Manual defer: no reason.
			So(out, ShouldContainSubstring, "  - database.password\n")
			// Autodefer: root-cause reason attached.
			So(out, ShouldContainSubstring, "  - meta.password: ")
			So(out, ShouldContainSubstring, "Vault client initialization")
		})

		Convey("help autodefer describes the command; autodefer completes as a command name", func() {
			out, rc := runDebugSession(files, "help autodefer\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "autodefer\n\nRuns the same defer-on-error retry loop")

			c := &debugCompleter{sess: &debugSession{loaded: true, tree: map[string]interface{}{}, breakpoints: map[string]bool{}, deferred: map[string]string{}}}
			So(completionsFor(c, "autodef"), ShouldResemble, []string{"autodefer "})
			So(completionsFor(c, "autodefer "), ShouldBeEmpty) // takes no argument
		})

		Convey("diff shows changes from the first loaded file to the current state", func() {
			out, rc := runDebugSession(files, "load\ncontinue\ndiff\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Changes from ../../assets/history/base.yml:\n")
			So(out, ShouldContainSubstring, "database.host: localhost → db.prod.example.com")
			So(out, ShouldContainSubstring, `database.password: <none> → "1.0"`)
		})

		Convey("export writes the current document to a YAML file", func() {
			dir := t.TempDir()
			target := dir + "/out.yml"
			out, rc := runDebugSession(files, fmt.Sprintf("load\ncontinue\nexport %s\nquit\n", target))
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Exported to "+target+"\n")

			data, err := os.ReadFile(target)
			So(err, ShouldBeNil)
			So(string(data), ShouldContainSubstring, "host: db.prod.example.com")
		})

		Convey("export writes JSON when the target ends in .json", func() {
			dir := t.TempDir()
			target := dir + "/out.json"
			_, rc := runDebugSession(files, fmt.Sprintf("load\ncontinue\nexport %s\nquit\n", target))
			So(rc, ShouldEqual, 0)

			data, err := os.ReadFile(target)
			So(err, ShouldBeNil)
			So(string(data), ShouldContainSubstring, `"host"`)
			So(string(data), ShouldContainSubstring, `"db.prod.example.com"`)
		})

		Convey("help lists every command; help <command> shows that command's detail", func() {
			out, rc := runDebugSession(files, "help\nhelp break\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Available commands:\n")
			So(out, ShouldContainSubstring, "  load ")
			So(out, ShouldContainSubstring, "  quit ")
			So(out, ShouldContainSubstring, "break <path>\n\nSets a breakpoint on a path.")
		})

		Convey("help on an unknown command says so", func() {
			out, rc := runDebugSession(files, "help bogus\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, `No help available for "bogus".`)
		})

		Convey("an unrecognized command is reported, not silently ignored", func() {
			out, rc := runDebugSession(files, "frobnicate\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Unknown command: frobnicate. Type 'help' for available commands.\n")
		})

		Convey("quit exits cleanly", func() {
			out, rc := runDebugSession(files, "quit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "graft> ")
		})

		Convey("exit is a synonym for quit", func() {
			out, rc := runDebugSession(files, "exit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "graft> ")
		})

		Convey("EOF on stdin (no quit) ends the session cleanly", func() {
			out, rc := runDebugSession(files, "load\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "Loaded 3 documents")
		})

		Convey("blank lines are ignored, not treated as unknown commands", func() {
			out, rc := runDebugSession(files, "\n\nload\n\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldNotContainSubstring, "Unknown command")
		})
	})

	Convey("graft debug with no files is a usage error, not a stdin-read error", t, func() {
		var out bytes.Buffer
		var stderr string
		log.PrintStdErrf = func(format string, args ...interface{}) {
			stderr += fmt.Sprintf(format, args...)
		}
		rc := handleDebug(nil, &mergeOpts{}, strings.NewReader(""), &out, debugUIOptions{})
		So(rc, ShouldEqual, 1)
		So(out.String(), ShouldEqual, "")
		So(stderr, ShouldContainSubstring, "graft debug requires at least one file")
	})

	Convey("graft merge --interactive is a working alias for graft debug", t, func() {
		var stderr string
		log.PrintStdErrf = func(format string, args ...interface{}) {
			stderr += fmt.Sprintf(format, args...)
		}
		rc := 256
		exit = func(code int) { rc = code }
		usage = func() {
			stderr = "usage was called"
			exit(1)
		}

		// The debug REPL's RunE wiring writes straight to the real
		// os.Stdout (not through the printStdOutf indirection the rest of
		// the CLI uses), since an interactive prompt needs to write
		// unbuffered without a test-only seam in the way. Capture it by
		// swapping os.Stdout itself, the same technique setStdinFromFile
		// uses for stdin.
		script := t.TempDir() + "/script.txt"
		if err := os.WriteFile(script, []byte("load\nquit\n"), 0o600); err != nil {
			t.Fatalf("writing script: %v", err)
		}
		restoreStdin := setStdinFromFile(t, script)
		defer restoreStdin()

		stdoutPath := t.TempDir() + "/stdout.txt"
		stdoutFile, err := os.Create(stdoutPath)
		if err != nil {
			t.Fatalf("creating stdout capture file: %v", err)
		}
		originalStdout := os.Stdout
		os.Stdout = stdoutFile
		defer func() { os.Stdout = originalStdout }()

		os.Args = []string{"graft", "merge", "--interactive", "../../assets/history/base.yml", "../../assets/history/env.yml"}
		stderr = ""
		main()
		_ = stdoutFile.Close()
		os.Stdout = originalStdout

		captured, readErr := os.ReadFile(stdoutPath)
		So(readErr, ShouldBeNil)
		stdout := string(captured)

		So(stderr, ShouldEqual, "")
		So(rc, ShouldEqual, 0)
		So(stdout, ShouldContainSubstring, "Welcome to the Graft Debugger")
		So(stdout, ShouldContainSubstring, "Loaded 2 documents:")
	})

	// F14 regression: the debug session's own merge calls (load's base
	// document, each step's raw merge, diff's recomputed base) must honor
	// opts.EnableGoPatch/FallbackAppend instead of silently discarding them
	// via a fresh &mergeOpts{SkipEval: true} that carries neither field.
	Convey("graft debug honors --go-patch in the session's own merges (F14)", t, func() {
		opts := &mergeOpts{EnableGoPatch: true}
		goPatchFiles := []string{"../../assets/history/base.yml", "../../assets/vaultinfo/go-patch.yml"}

		out, rc := runDebugSessionWithOpts(goPatchFiles, opts, "load\nstep\noutput\nquit\n")
		So(rc, ShouldEqual, 0)
		So(out, ShouldNotContainSubstring, "Root of YAML document is not a hash/map")
		So(out, ShouldNotContainSubstring, "Merge failed")
		// go-patch.yml's op replaces/creates new_key with a (still
		// unevaluated, since this is the raw pre-eval step) vault operator
		// expression.
		So(out, ShouldContainSubstring, `new_key: (( vault "secret/blork:blork" ))`)
	})

	Convey("graft debug without --go-patch fails on an array-rooted document, the same as without the flag anywhere else", t, func() {
		goPatchFiles := []string{"../../assets/history/base.yml", "../../assets/vaultinfo/go-patch.yml"}

		out, rc := runDebugSession(goPatchFiles, "load\nstep\nquit\n")
		So(rc, ShouldEqual, 0) // the REPL itself always exits 0; the merge error is printed inline
		So(out, ShouldContainSubstring, "Merge failed")
	})

	Convey("graft debug --go-patch is a registered CLI flag, not a usage error", t, func() {
		var stderr string
		log.PrintStdErrf = func(format string, args ...interface{}) {
			stderr += fmt.Sprintf(format, args...)
		}
		rc := 256
		exit = func(code int) { rc = code }
		usage = func() {
			stderr = "usage was called"
			exit(1)
		}

		script := t.TempDir() + "/script.txt"
		if err := os.WriteFile(script, []byte("load\nstep\noutput\nquit\n"), 0o600); err != nil {
			t.Fatalf("writing script: %v", err)
		}
		restoreStdin := setStdinFromFile(t, script)
		defer restoreStdin()

		stdoutPath := t.TempDir() + "/stdout.txt"
		stdoutFile, err := os.Create(stdoutPath)
		if err != nil {
			t.Fatalf("creating stdout capture file: %v", err)
		}
		originalStdout := os.Stdout
		os.Stdout = stdoutFile
		defer func() { os.Stdout = originalStdout }()

		os.Args = []string{"graft", "debug", "--go-patch", "../../assets/history/base.yml", "../../assets/vaultinfo/go-patch.yml"}
		stderr = ""
		main()
		_ = stdoutFile.Close()
		os.Stdout = originalStdout

		captured, readErr := os.ReadFile(stdoutPath)
		So(readErr, ShouldBeNil)
		stdout := string(captured)

		So(stderr, ShouldNotEqual, "usage was called")
		So(rc, ShouldEqual, 0)
		So(stdout, ShouldContainSubstring, `new_key: (( vault "secret/blork:blork" ))`)
	})

	Convey("graft debug --prune/--cherry-pick are registered CLI flags, not usage errors", t, func() {
		var stderr string
		log.PrintStdErrf = func(format string, args ...interface{}) {
			stderr += fmt.Sprintf(format, args...)
		}
		rc := 256
		exit = func(code int) { rc = code }
		usage = func() {
			stderr = "usage was called"
			exit(1)
		}

		script := t.TempDir() + "/script.txt"
		if err := os.WriteFile(script, []byte("load\ncontinue\nprune-report\nquit\n"), 0o600); err != nil {
			t.Fatalf("writing script: %v", err)
		}
		restoreStdin := setStdinFromFile(t, script)
		defer restoreStdin()

		stdoutPath := t.TempDir() + "/stdout.txt"
		stdoutFile, err := os.Create(stdoutPath)
		if err != nil {
			t.Fatalf("creating stdout capture file: %v", err)
		}
		originalStdout := os.Stdout
		os.Stdout = stdoutFile
		defer func() { os.Stdout = originalStdout }()

		os.Args = []string{"graft", "debug", "--prune", "database.port",
			"../../assets/history/base.yml", "../../assets/history/env.yml"}
		stderr = ""
		main()
		_ = stdoutFile.Close()
		os.Stdout = originalStdout

		captured, readErr := os.ReadFile(stdoutPath)
		So(readErr, ShouldBeNil)
		stdout := string(captured)

		So(stderr, ShouldNotEqual, "usage was called")
		So(rc, ShouldEqual, 0)
		So(stdout, ShouldContainSubstring, "Paths --prune/--cherry-pick would remove")
		So(stdout, ShouldContainSubstring, "  - database.port\n")
	})

	// F19 regression: cmdContinue must stop after the first failing step,
	// not spin forever re-running and re-failing it. Both cases run
	// handleDebug in a goroutine with a hard timeout, rather than calling
	// it inline, so that if the fix regresses the test itself fails fast
	// (via the timeout branch) instead of hanging the whole test binary.
	Convey("continue stops after a merge failure instead of looping forever (F19)", t, func() {
		// go-patch.yml is array-rooted; without --go-patch (the default
		// here), merging it as plain YAML fails at the second step.
		goPatchFiles := []string{"../../assets/history/base.yml", "../../assets/vaultinfo/go-patch.yml"}

		type result struct {
			out string
			rc  int
		}
		done := make(chan result, 1)
		go func() {
			var out bytes.Buffer
			rc := handleDebug(goPatchFiles, &mergeOpts{}, strings.NewReader("load\ncontinue\nquit\n"), &out, debugUIOptions{})
			done <- result{out.String(), rc}
		}()

		select {
		case res := <-done:
			So(res.rc, ShouldEqual, 0)
			So(strings.Count(res.out, "Merge failed"), ShouldEqual, 1)
			So(len(res.out), ShouldBeLessThan, 10000)
		case <-time.After(5 * time.Second):
			t.Error("continue did not return within 5s after a merge failure - looks like the infinite-loop regression (F19)")
		}
	})

	Convey("continue stops after an evaluation failure instead of looping forever (F19)", t, func() {
		evalFailureFiles := []string{"../../assets/debug/eval-failure.yml"}

		type result struct {
			out string
			rc  int
		}
		done := make(chan result, 1)
		go func() {
			var out bytes.Buffer
			rc := handleDebug(evalFailureFiles, &mergeOpts{}, strings.NewReader("load\ncontinue\nquit\n"), &out, debugUIOptions{})
			done <- result{out.String(), rc}
		}()

		select {
		case res := <-done:
			So(res.rc, ShouldEqual, 0)
			So(strings.Count(res.out, "Evaluation failed"), ShouldEqual, 1)
			So(len(res.out), ShouldBeLessThan, 10000)
		case <-time.After(5 * time.Second):
			t.Error("continue did not return within 5s after an evaluation failure - looks like the infinite-loop regression (F19)")
		}
	})

	Convey("plain step is unaffected by F19's fix: one 'step' command reports one failure and returns to the prompt", t, func() {
		goPatchFiles := []string{"../../assets/history/base.yml", "../../assets/vaultinfo/go-patch.yml"}
		out, rc := runDebugSession(goPatchFiles, "load\nstep\nquit\n")
		So(rc, ShouldEqual, 0)
		So(strings.Count(out, "Merge failed"), ShouldEqual, 1)
	})

	// Decision 13 (plans/debugger-colorizing.md): engine errors bake their
	// color in via ansi.Sprintf/Errorf at the moment Error() is called,
	// against whatever the package-global ansi.Color state is then - so an
	// error built while color happens to be on carries live escape codes
	// inside err.Error() itself, independent of what the debugger does with
	// it afterward. That is today's redirect leak: `graft debug ... >
	// out.txt` with stderr still a terminal bakes those escapes into the
	// file. Forcing ansi.Color(true) here (deferred restore, following
	// debug_tree_test.go's prevColor/Reset convention) is essential: the
	// suite-wide init() in main_test.go disables color, so an unforced
	// version of this test would pass vacuously without ever exercising
	// the leak.
	//
	// This locks only the half of decision 13 this phase delivers: the
	// error-derived argument at each of the 18 stdout error sites is
	// escape-free once stripped. The literal "@R{...}" labels at those
	// same sites (e.g. "Merge failed", "Evaluation failed") still render
	// through that same global flag until the role migration replaces
	// them with the per-session styler, so this test does not assert the
	// full output is free of every escape byte - only that none of the
	// bytes baked into the underlying error survive. The full "zero \x1b
	// bytes anywhere in the output" guarantee is a later regression test.
	Convey("engine-baked escapes inside error text do not survive into debugger output under forced color", t, func() {
		prevColor := ansi.IsColorEnabled()
		ansi.Color(true)
		Reset(func() { ansi.Color(prevColor) })

		Convey("a merge failure keeps its message but loses the error's own baked-in color", func() {
			goPatchFiles := []string{"../../assets/history/base.yml", "../../assets/vaultinfo/go-patch.yml"}
			out, rc := runDebugSession(goPatchFiles, "load\nstep\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "../../assets/vaultinfo/go-patch.yml: root of YAML document is not a hash/map")
			So(out, ShouldNotContainSubstring, "\x1b[35m../../assets/vaultinfo/go-patch.yml\x1b[0m")
			So(out, ShouldNotContainSubstring, "\x1b[31mroot of YAML document is not a hash/map\x1b[0m")
		})

		Convey("an evaluation failure keeps its message but loses the error's own baked-in color", func() {
			evalFailureFiles := []string{"../../assets/debug/eval-failure.yml"}
			out, rc := runDebugSession(evalFailureFiles, "load\ncontinue\nquit\n")
			So(rc, ShouldEqual, 0)
			So(out, ShouldContainSubstring, "1 error(s) detected:\n - $.bad: unable to resolve `nonexistent.path`: `$.nonexistent` could not be found in the datastructure")
			So(out, ShouldNotContainSubstring, "\x1b[31m1\x1b[0m error(s) detected:")
			So(out, ShouldNotContainSubstring, "\x1b[36m$.nonexistent\x1b[0m")
			So(out, ShouldNotContainSubstring, "\x1b[31m`\x1b[0m")
		})
	})

	// A debugSession resolves its styler once, at construction, against
	// its own out writer via the writerIsTTY seam (never against
	// stderr - that is the package-global ansi flag's job). Faking
	// writerIsTTY true, the way --no-color's ColorOverride=false must
	// still win outright, proves the override beats a terminal that
	// would otherwise auto-enable color; the companion true/nil case
	// proves the same faked terminal actually drives the auto-mode
	// answer, not a stuck constant.
	Convey("a debug session resolves its styler against its own writer, honoring an explicit override over a faked terminal", t, func() {
		prevWriterIsTTY := writerIsTTY
		writerIsTTY = func(io.Writer) bool { return true }
		defer func() { writerIsTTY = prevWriterIsTTY }()

		noColor := false
		var out bytes.Buffer
		sess, err := newDebugSession(files, &mergeOpts{}, &out, debugUIOptions{ColorOverride: &noColor})
		So(err, ShouldBeNil)
		So(sess.styler.enabled, ShouldBeFalse)

		color := true
		sessColorOn, err := newDebugSession(files, &mergeOpts{}, &out, debugUIOptions{ColorOverride: &color})
		So(err, ShouldBeNil)
		So(sessColorOn.styler.enabled, ShouldBeTrue)
	})
}

// TestDebugNoEscapeFullOutput locks the full guarantee of decision 13
// (plans/debugger-colorizing.md): now that the 18 error sites render
// their labels through the per-session styler instead of the 18 sites'
// own "@R{...}" markup (this phase's migration), a scripted session's
// entire output carries zero \x1b bytes, not just the error-derived
// argument the narrower test above already covers. Forcing
// ansi.Color(true) (deferred restore, same convention) still matters:
// before this phase, those labels rendered through that same global
// flag, so an unforced run - or one against pre-migration code - would
// either pass vacuously or leak the labels' own escapes, never proving
// the guarantee this phase delivers. Every session below stays
// color-off throughout regardless of the forced global: a bytes.Buffer
// is never a terminal and none of these sessions pass a ColorOverride,
// so s.style is identity at every call site - forcing the global only
// matters for engine errors, which build their text via
// ansi.Sprintf/Errorf against that same global at construction time.
func TestDebugNoEscapeFullOutput(t *testing.T) {
	prevColor := ansi.IsColorEnabled()
	ansi.Color(true)
	defer ansi.Color(prevColor)

	assertNoEscapes := func(t *testing.T, label, out string) {
		t.Helper()
		if strings.ContainsRune(out, '\x1b') {
			t.Errorf("%s: output contains an escape byte:\n%q", label, out)
		}
	}

	t.Run("a full command tour across every category carries no escapes", func(t *testing.T) {
		dir := t.TempDir()
		target := dir + "/tour-export.yml"
		script := strings.Join([]string{
			"load",
			"break database.pool_size",
			"continue", // hits the breakpoint mid-merge
			"breaks",
			"unbreak database.pool_size",
			"breaks",
			"continue", // runs the remaining merge step plus evaluation to completion
			"diff",
			"defer database.password",
			"inspect",
			"autodefer",
			"eval database.password",
			"config",
			"config vault.token",
			"config bogus.key",
			"prune-report",
			"export " + target,
			"help",
			"help bogus",
			"frobnicate",
			"quit",
		}, "\n") + "\n"
		out, rc := runDebugSession(debugColorizeTestFiles, script)
		if rc != 0 {
			t.Fatalf("rc = %d, want 0:\n%s", rc, out)
		}
		assertNoEscapes(t, "full command tour", out)
	})

	t.Run("guard messages issued before load carry no escapes", func(t *testing.T) {
		out, rc := runDebugSession(debugColorizeTestFiles, strings.Join([]string{
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
			t.Fatalf("rc = %d, want 0:\n%s", rc, out)
		}
		assertNoEscapes(t, "guard messages", out)
	})

	t.Run("a merge failure carries no escapes", func(t *testing.T) {
		goPatchFiles := []string{"../../assets/history/base.yml", "../../assets/vaultinfo/go-patch.yml"}
		out, rc := runDebugSession(goPatchFiles, "load\nstep\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0:\n%s", rc, out)
		}
		if !strings.Contains(out, "Merge failed") {
			t.Fatalf("test setup: expected a merge failure in the output:\n%s", out)
		}
		assertNoEscapes(t, "merge failure", out)
	})

	t.Run("an evaluation failure carries no escapes", func(t *testing.T) {
		evalFailureFiles := []string{"../../assets/debug/eval-failure.yml"}
		out, rc := runDebugSession(evalFailureFiles, "load\ncontinue\nquit\n")
		if rc != 0 {
			t.Fatalf("rc = %d, want 0:\n%s", rc, out)
		}
		if !strings.Contains(out, "Evaluation failed") {
			t.Fatalf("test setup: expected an evaluation failure in the output:\n%s", out)
		}
		assertNoEscapes(t, "evaluation failure", out)
	})
}
