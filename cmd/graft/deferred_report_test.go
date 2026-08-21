package main

import (
	"strings"
	"testing"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
)

// This file tests `graft merge --defer-on-error`/`--adaptive` and
// `--report-deferred` end to end through main() (plans/dennis-feedback-
// gaps.md Item 2). adaptive_merge_test.go covers runAdaptiveMerge's own
// mechanics directly; these tests cover the CLI flag wiring, exit codes,
// and the four --report-deferred placements' exact rendered output.

// TestMergeDeferOnErrorDefersAndExits3 pins the default (--report-
// deferred=beginning) end-to-end shape: the cascade fixture (root vault
// failure + a grab dependent) merges successfully with the deferred
// expression intact, a summary comment block at the top, and exit 3.
func TestMergeDeferOnErrorDefersAndExits3(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--defer-on-error", "../../assets/skip-defer/transitive-grab.yml"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3, stderr: %s", rc, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	want := "---\n" +
		"# graft: 1 key deferred\n" +
		"# graft: deferred $.meta.password: Error during Vault client initialization: failed to determine Vault URL / token, and the $REDACT environment variable is not set\n" +
		"database:\n" +
		"  password: (( vault \"secret/db:password\" ))\n" +
		"meta:\n" +
		"  password: (( vault \"secret/db:password\" ))\n\n"
	if stdout != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", stdout, want)
	}
}

// TestMergeAdaptiveIsAnAliasForDeferOnError confirms --adaptive produces
// byte-identical output to --defer-on-error for the same input.
func TestMergeAdaptiveIsAnAliasForDeferOnError(t *testing.T) {
	deferOut, deferErr, deferRC := runMerge(t, []string{"merge", "--defer-on-error", "../../assets/skip-defer/transitive-grab.yml"})
	adaptiveOut, adaptiveErr, adaptiveRC := runMerge(t, []string{"merge", "--adaptive", "../../assets/skip-defer/transitive-grab.yml"})

	if deferRC != adaptiveRC {
		t.Fatalf("rc: --defer-on-error=%d, --adaptive=%d, want equal", deferRC, adaptiveRC)
	}
	if deferErr != adaptiveErr {
		t.Fatalf("stderr differs: --defer-on-error=%q, --adaptive=%q", deferErr, adaptiveErr)
	}
	if deferOut != adaptiveOut {
		t.Fatalf("stdout differs:\n--defer-on-error: %q\n--adaptive:       %q", deferOut, adaptiveOut)
	}
}

// TestMergeReportDeferredInlinePlacement pins the "inline" placement:
// the comment sits directly above the one key that actually failed
// (meta.password) - not above database.password, which never itself
// produced a *PathError (it just copied meta.password's deferred text
// via grab; see runAdaptiveMerge's doc comment on attribution).
func TestMergeReportDeferredInlinePlacement(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--defer-on-error", "--report-deferred=inline", "../../assets/skip-defer/transitive-grab.yml"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3, stderr: %s", rc, stderr)
	}
	want := "---\n" +
		"database:\n" +
		"  password: (( vault \"secret/db:password\" ))\n" +
		"meta:\n" +
		"  # graft: deferred: Error during Vault client initialization: failed to determine Vault URL / token, and the $REDACT environment variable is not set\n" +
		"  password: (( vault \"secret/db:password\" ))\n\n"
	if stdout != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", stdout, want)
	}
}

// TestMergeReportDeferredEndPlacement pins the "end" placement: the same
// summary block as "beginning", appended after the document instead.
func TestMergeReportDeferredEndPlacement(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--defer-on-error", "--report-deferred=end", "../../assets/skip-defer/transitive-grab.yml"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3, stderr: %s", rc, stderr)
	}
	want := "---\n" +
		"database:\n" +
		"  password: (( vault \"secret/db:password\" ))\n" +
		"meta:\n" +
		"  password: (( vault \"secret/db:password\" ))\n" +
		"# graft: 1 key deferred\n" +
		"# graft: deferred $.meta.password: Error during Vault client initialization: failed to determine Vault URL / token, and the $REDACT environment variable is not set\n\n"
	if stdout != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", stdout, want)
	}
}

// TestMergeReportDeferredNonePlacement pins the "none" placement:
// exit 3 (Dennis's "silenced" option - the caller can still tell a
// partial merge happened) with no comments at all, output otherwise
// identical to the plain document.
func TestMergeReportDeferredNonePlacement(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--defer-on-error", "--report-deferred=none", "../../assets/skip-defer/transitive-grab.yml"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3, stderr: %s", rc, stderr)
	}
	want := "---\n" +
		"database:\n" +
		"  password: (( vault \"secret/db:password\" ))\n" +
		"meta:\n" +
		"  password: (( vault \"secret/db:password\" ))\n\n"
	if stdout != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", stdout, want)
	}
	if strings.Contains(stdout, "graft:") {
		t.Fatalf("--report-deferred=none must not emit any comment, got:\n%s", stdout)
	}
}

// TestMergeReportDeferredInvalidValue pins a clear usage error (exit 1)
// for an unrecognized --report-deferred value - independent of whether
// anything would even defer.
func TestMergeReportDeferredInvalidValue(t *testing.T) {
	_, stderr, rc := runMerge(t, []string{"merge", "--report-deferred=bogus", "../../assets/merge/first.yml"})
	if rc != 1 {
		t.Fatalf("rc = %d, want 1, stderr: %s", rc, stderr)
	}
	if !strings.Contains(stderr, `invalid --report-deferred value "bogus"`) {
		t.Fatalf("stderr = %q, want it to name the bad value", stderr)
	}
}

// TestMergeDeferOnErrorNoDeferralsIsByteIdenticalToPlainMerge pins Item
// 2's "a merge with no deferrals stays byte-identical to today"
// requirement: --defer-on-error given, but nothing fails, produces
// exactly the same bytes and exit code (0, not 3) as a plain merge of
// the same files.
func TestMergeDeferOnErrorNoDeferralsIsByteIdenticalToPlainMerge(t *testing.T) {
	plainOut, plainErr, plainRC := runMerge(t, []string{"merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"})
	adaptiveOut, adaptiveErr, adaptiveRC := runMerge(t, []string{"merge", "--defer-on-error", "../../assets/merge/first.yml", "../../assets/merge/second.yml"})

	if plainRC != 0 {
		t.Fatalf("plain merge rc = %d, want 0 (test premise broken)", plainRC)
	}
	if adaptiveRC != 0 {
		t.Fatalf("--defer-on-error rc = %d, want 0 (nothing to defer)", adaptiveRC)
	}
	if plainErr != adaptiveErr {
		t.Fatalf("stderr differs: plain=%q, --defer-on-error=%q", plainErr, adaptiveErr)
	}
	if plainOut != adaptiveOut {
		t.Fatalf("stdout differs:\nplain:          %q\n--defer-on-error: %q", plainOut, adaptiveOut)
	}
}

// TestMergeDeferOnErrorCycleIsHardFailure pins Item 2's cycle
// requirement at the CLI level: a genuine operator-dependency cycle
// under --defer-on-error still fails the merge (exit 2) and reports the
// original cycle error, exactly like a plain merge of the same cyclic
// document would - not silently "succeeding" by pretending nothing
// needed deferring.
func TestMergeDeferOnErrorCycleIsHardFailure(t *testing.T) {
	plainOut, plainErr, plainRC := runMerge(t, []string{"merge", "../../assets/skip-defer/cycle.yml"})
	adaptiveOut, adaptiveErr, adaptiveRC := runMerge(t, []string{"merge", "--defer-on-error", "../../assets/skip-defer/cycle.yml"})

	if plainRC != 2 {
		t.Fatalf("plain merge rc = %d, want 2 (test premise broken), stderr: %s", plainRC, plainErr)
	}
	if adaptiveRC != plainRC {
		t.Fatalf("--defer-on-error rc = %d, want %d (same hard failure as a plain merge)", adaptiveRC, plainRC)
	}
	if adaptiveOut != plainOut {
		t.Fatalf("stdout differs: plain=%q, --defer-on-error=%q", plainOut, adaptiveOut)
	}
	if adaptiveErr != plainErr {
		t.Fatalf("stderr differs: plain=%q, --defer-on-error=%q", plainErr, adaptiveErr)
	}
	if !strings.Contains(adaptiveErr, "cycle") {
		t.Fatalf("stderr = %q, want it to mention the cycle", adaptiveErr)
	}
}

// TestMergeDeferOnErrorCannotCombineWithHistoryFlags pins the explicit
// mutual-exclusivity guard: --defer-on-error selects a completely
// different report shape than --history/--trace-path/--show-changes/
// --changes-only, so combining them is rejected instead of silently
// picking one.
func TestMergeDeferOnErrorCannotCombineWithHistoryFlags(t *testing.T) {
	_, stderr, rc := runMerge(t, []string{"merge", "--defer-on-error", "--history", "../../assets/merge/first.yml"})
	if rc != 1 {
		t.Fatalf("rc = %d, want 1, stderr: %s", rc, stderr)
	}
	if !strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("stderr = %q, want a clear mutual-exclusivity message", stderr)
	}
}

// TestMergeDeferOnErrorRoundTrip is --defer-on-error's own round-trip
// proof (Item 2's "output with deferral comments re-parses and
// re-merges cleanly" requirement, comment placements included): the
// commented output from a --defer-on-error merge is fed back into a
// second `graft merge` (a stub Vault reader wired in, no --defer-on-error
// needed this time) and evaluates cleanly to the real secret value - the
// "# graft: ..." comments are ignored by the YAML parser, exactly as
// ordinary comments always are.
func TestMergeDeferOnErrorRoundTrip(t *testing.T) {
	vaultbackend.SecretCache.Reset()
	defer vaultbackend.SecretCache.Reset()

	firstOut, firstErr, firstRC := runMerge(t, []string{"merge", "--defer-on-error", "../../assets/vault/self-reference.yml"})
	if firstRC != 3 {
		t.Fatalf("first (deferred) merge rc = %d, want 3, stderr: %s", firstRC, firstErr)
	}
	if !strings.Contains(firstOut, "# graft:") {
		t.Fatalf("first (deferred) merge stdout missing the expected comment block: %q", firstOut)
	}

	deferredFile := writeDoc(t, t.TempDir(), "deferred.yml", firstOut)

	withGlobalVaultReader(selfReferencingPathReader{}, func() {
		secondOut, secondErr, secondRC := runMerge(t, []string{"merge", deferredFile})
		if secondRC != 0 {
			t.Fatalf("second (live) merge rc = %d, want 0, stderr: %s", secondRC, secondErr)
		}
		want := "---\nmeta:\n  path: child\nvalue: s3kr1t\n\n"
		if secondOut != want {
			t.Fatalf("second (live) merge stdout = %q, want %q", secondOut, want)
		}
	})
}
