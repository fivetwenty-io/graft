package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
	"github.com/fivetwenty-io/graft/log"
)

// This file tests plans/dennis-feedback-gaps.md's Item 3: the
// --skip-vault/--skip-aws/--skip-nats merge flags, and their "defer,
// not REDACTED" semantics (op_skip_defer.go, pkg/graft/operators), plus
// Item 2's exit-code-3 contract for a merge that deferred anything
// (adaptive_merge_test.go and deferred_report_test.go cover Item 2's own
// --defer-on-error/--report-deferred machinery directly).
// TestVaultInfoResolveReportsConcretePaths (vaultinfo_resolve_test.go,
// this package) already exercises vaultinfo's own redact-mode use of
// WithSkipVault; these tests are the merge-flag side.
//
// Every test below that actually defers something passes
// --report-deferred=none, isolating the defer-mechanics assertions
// (which path deferred, transitive grab, per-backend independence,
// round-trip) from the comment-report format itself, covered separately
// in deferred_report_test.go.

// runMerge invokes main() with the given CLI args and returns the
// captured stdout/stderr/exit code, restoring the previous test hooks
// and os.Args afterward. Mirrors runVaultinfo (vaultinfo_resolve_test.go)
// for the merge command.
func runMerge(t *testing.T, args []string) (stdout, stderr string, rc int) {
	t.Helper()

	prevPrintStdOutf := printStdOutf
	prevPrintStdErrf := log.PrintStdErrf
	prevExit := exit
	prevUsage := usage
	prevArgs := os.Args
	defer func() {
		printStdOutf = prevPrintStdOutf
		log.PrintStdErrf = prevPrintStdErrf
		exit = prevExit
		usage = prevUsage
		os.Args = prevArgs
	}()

	printStdOutf = func(format string, fmtArgs ...interface{}) {
		stdout += fmt.Sprintf(format, fmtArgs...)
	}
	log.PrintStdErrf = func(format string, fmtArgs ...interface{}) {
		stderr += fmt.Sprintf(format, fmtArgs...)
	}
	rc = 256 // sentinel: unset if exit is never called
	exit = func(code int) { rc = code }
	usage = func() { exit(1) }

	os.Args = append([]string{"graft"}, args...)
	main()
	return stdout, stderr, rc
}

// TestMergeSkipVaultFlag pins the core Item 3 requirement: --skip-vault
// with no Vault reachable leaves the (( vault ... )) expression intact
// in the output and exits 3 (a successful partial merge - Item 2's
// exit-code contract, since something was deferred - not an error).
func TestMergeSkipVaultFlag(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--skip-vault", "--report-deferred=none", "../../assets/skip-defer/vault.yml"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3 (successful partial merge), stderr: %s", rc, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	want := "---\ndatabase:\n  password: (( vault \"secret/db:password\" ))\n\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

// TestMergeSkipAwsFlag and TestMergeSkipNatsFlag mirror
// TestMergeSkipVaultFlag for the other two backends, each against a
// fixture containing only its own operator.
func TestMergeSkipAwsFlag(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--skip-aws", "--report-deferred=none", "../../assets/skip-defer/all-three-backends.yml", "--skip-vault", "--skip-nats"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3, stderr: %s", rc, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `aws_param: (( awsparam "/config/app/setting" ))`) {
		t.Fatalf("stdout missing deferred awsparam expression: %q", stdout)
	}
	if !strings.Contains(stdout, `aws_secret: (( awssecret "prod/database/password" ))`) {
		t.Fatalf("stdout missing deferred awssecret expression: %q", stdout)
	}
}

func TestMergeSkipNatsFlag(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--skip-vault", "--skip-aws", "--skip-nats", "--report-deferred=none", "../../assets/skip-defer/all-three-backends.yml"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3, stderr: %s", rc, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `nats_value: (( nats "kv:mybucket/mykey" ))`) {
		t.Fatalf("stdout missing deferred nats expression: %q", stdout)
	}
}

// TestMergeSkipAllThreeBackendsCompose confirms the three flags are
// composable in a single invocation (Item 3's "one flag per backend,
// composable" requirement): every operator in a fixture using all three
// backends defers, and the merge still succeeds (partially).
func TestMergeSkipAllThreeBackendsCompose(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--skip-vault", "--skip-aws", "--skip-nats", "--report-deferred=none", "../../assets/skip-defer/all-three-backends.yml"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3, stderr: %s", rc, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		`vault_value: (( vault "secret/db:password" ))`,
		`aws_param: (( awsparam "/config/app/setting" ))`,
		`aws_secret: (( awssecret "prod/database/password" ))`,
		`nats_value: (( nats "kv:mybucket/mykey" ))`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %s", want, stdout)
		}
	}
}

// TestMergeSkipVaultDoesNotDeferOtherOperators confirms defer mode is
// scoped to the flagged backend's own operator: a non-vault field in the
// same document (a plain (( concat )) call) still evaluates normally
// under --skip-vault, rather than every field mysteriously deferring.
func TestMergeSkipVaultDoesNotDeferOtherOperators(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--skip-vault", "--report-deferred=none", "../../assets/skip-defer/vault-and-plain.yml"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3, stderr: %s", rc, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `password: (( vault "secret/db:password" ))`) {
		t.Fatalf("stdout missing deferred vault expression: %q", stdout)
	}
	if !strings.Contains(stdout, "greeting: hello, world") {
		t.Fatalf("stdout: (( concat )) should have evaluated normally, not deferred: %q", stdout)
	}
}

// TestMergeSkipVaultGrabDefersTransitively pins Dennis's exact Item 3
// requirement: a (( grab )) of a deferred vault-backed value defers too
// (by simply copying the still-unevaluated-looking expression text), so
// the whole document round-trips instead of only the direct vault call.
func TestMergeSkipVaultGrabDefersTransitively(t *testing.T) {
	stdout, stderr, rc := runMerge(t, []string{"merge", "--skip-vault", "--report-deferred=none", "../../assets/skip-defer/transitive-grab.yml"})
	if rc != 3 {
		t.Fatalf("rc = %d, want 3, stderr: %s", rc, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	want := "---\ndatabase:\n  password: (( vault \"secret/db:password\" ))\nmeta:\n  password: (( vault \"secret/db:password\" ))\n\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

// TestMergeRedactCompatUnchangedByNewFlags pins the CRITICAL compat
// requirement: REDACT=1 (no --skip-vault flag at all) keeps its existing
// redacting behavior byte-for-byte, unaffected by the new flags' mere
// existence - including the exit code: REDACT mode redacts, it never
// defers (op_skip_defer.go's AddDeferredPath is only reached on the
// non-redact branch), so this stays a clean, exit-0 merge.
func TestMergeRedactCompatUnchangedByNewFlags(t *testing.T) {
	t.Setenv("REDACT", "1")
	stdout, stderr, rc := runMerge(t, []string{"merge", "../../assets/skip-defer/vault.yml"})
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	want := "---\ndatabase:\n  password: REDACTED\n\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

// TestMergeRedactWinsOverSkipVaultFlag confirms REDACT=1 wins outright
// even when --skip-vault is also given: the flag alone selects defer
// mode, but REDACT=1 forces redact mode regardless (graft.OperatorState.
// IsRedactMode, engine.go's evaluate) - so, like the REDACT-only case
// above, this is a clean exit-0 merge, not a partial one.
func TestMergeRedactWinsOverSkipVaultFlag(t *testing.T) {
	t.Setenv("REDACT", "1")
	stdout, stderr, rc := runMerge(t, []string{"merge", "--skip-vault", "../../assets/skip-defer/vault.yml"})
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	want := "---\ndatabase:\n  password: REDACTED\n\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q (REDACT=1 must win over --skip-vault's own defer default)", stdout, want)
	}
}

// TestMergeSkipVaultRoundTrip is the full Item 3 round-trip proof: the
// deferred output from a --skip-vault merge is fed back into a second
// `graft merge` (no --skip-vault this time, a stub Vault reader wired in
// place of a real one) and evaluates cleanly to the real secret value,
// confirming a document merged under --skip-vault can be merged again
// later once the backend is reachable.
func TestMergeSkipVaultRoundTrip(t *testing.T) {
	vaultbackend.SecretCache.Reset()
	defer vaultbackend.SecretCache.Reset()

	firstOut, firstErr, firstRC := runMerge(t, []string{"merge", "--skip-vault", "--report-deferred=none", "../../assets/vault/self-reference.yml"})
	if firstRC != 3 {
		t.Fatalf("first (deferred) merge rc = %d, want 3, stderr: %s", firstRC, firstErr)
	}
	if firstErr != "" {
		t.Fatalf("first (deferred) merge stderr = %q, want empty", firstErr)
	}
	wantFirst := "---\nmeta:\n  path: (( vault \"secret/paths:root\" ))\nvalue: (( vault \"secret/paths:\" meta.path ))\n\n"
	if firstOut != wantFirst {
		t.Fatalf("first (deferred) merge stdout = %q, want %q", firstOut, wantFirst)
	}

	deferredFile := writeDoc(t, t.TempDir(), "deferred.yml", firstOut)

	withGlobalVaultReader(selfReferencingPathReader{}, func() {
		secondOut, secondErr, secondRC := runMerge(t, []string{"merge", deferredFile})
		if secondRC != 0 {
			t.Fatalf("second (live) merge rc = %d, want 0 (nothing deferred this time), stderr: %s", secondRC, secondErr)
		}
		if secondErr != "" {
			t.Fatalf("second (live) merge stderr = %q, want empty", secondErr)
		}
		wantSecond := "---\nmeta:\n  path: child\nvalue: s3kr1t\n\n"
		if secondOut != wantSecond {
			t.Fatalf("second (live) merge stdout = %q, want %q", secondOut, wantSecond)
		}
	})
}
