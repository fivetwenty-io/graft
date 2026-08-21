package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/log"
)

// This file is the Phase-0 index for R3/R3b (see
// ~/.agents/plans/graft-library-api-plan.md, "Wave E2 versus C4 and C7" and
// the R3/R3b risk-table rows): a single place enumerating every string the
// Genesis compatibility contract (docs/spruce/genesis-compat-contract.md)
// constrains, and where each one is pinned by a go-test-visible assertion.
// Deliberately scoped to contract-visible strings only, not every Error()
// body in the repo, so Wave E2 (error-code annotations) and C4/C7 (sentinel
// errors, BackendError) can both land without fighting this file — see
// genesis-compat-contract.md:53-111 and the plan's R3/R3b rows for the
// scoping rationale.
//
// Existing coverage surveyed before adding anything here (none of it is
// duplicated below):
//
//   - stderr shape " - $.<path>: <msg>" (contract:53-63): pinned by
//     TestGenesisAdaptiveMergeErrorFormat, this package
//     (genesis_error_format_test.go), against the same regex genesis's
//     _adaptive_merge uses. Also pinned at the library level, GRAFT_ERROR_CODES
//     unset, by TestMultiErrorDefaultFormatUnchanged and
//     TestMultiErrorDefaultFormatUnchangedWithFalsyEnv,
//     pkg/graft/errors_test.go.
//   - "secret <key> not found" (contract:65-69): pinned hermetically (fake
//     vaultbackend.Reader, no live Vault) by
//     TestVaultOperatorNotFoundErrorText,
//     pkg/graft/operators/op_vault_parity_test.go, and again by
//     pkg/graft/operators/op_vault_errorcode_test.go.
//   - "invalid argument <value>; must be in the form path/to/secret:key"
//     (contract:76-83): pinned byte-for-byte, hermetically, by
//     TestVaultOperatorKeyParseErrorText,
//     pkg/graft/operators/op_vault_parity_test.go.
//   - one-JSON-object-per-line framing (contract:85-91): pinned at the
//     library level by TestJSONifyFilesMultiDoc, pkg/graft/json_test.go, and
//     at the CLI level by the "json command emits one JSON object per line
//     for multi-doc input" case in TestMain, this package (main_test.go).
//   - exit codes 0/1/2 (contract:102-111): pinned per-command across
//     TestMain, this package (main_test.go) -- e.g. the "diff command"
//     sub-tests assert 0 (identical inputs), 1 (differences), and 2 (load
//     error) explicitly; merge and json sub-tests assert the same 0/2
//     split for their own success/error cases.
//   - "-v"/"--version" shape and the genesis minimum-version gate
//     (contract:10-27): pinned by the "Should output version" sub-tests in
//     TestMain, this package (main_test.go), including the
//     genesisVersionRegex match and a semverAtLeast(..., minGenesisVersion)
//     check.
//
// The one gap that survey found: every existing GRAFT_ERROR_CODES=1
// assertion either checks MultiError.Error() directly (library level,
// pkg/graft/errors_test.go) or never sets the env var at all (CLI level,
// genesis_error_format_test.go). Nothing had driven a real merge failure
// through main() with GRAFT_ERROR_CODES=1 set and confirmed the stderr line
// genesis's regex actually scrapes is untouched. TestGenesisContractErrorCodesOptInPreservesScrapedShape
// below closes that gap; it is the CLI-level twin of
// TestMultiErrorOptInStillMatchesGenesisPathRegex (pkg/graft/errors_test.go),
// and the reconciliation point R3b calls for: this must stay green whether
// C4/C7's Is/Unwrap additions land, or Wave E2 widens which errors carry a
// code, as long as neither touches the " - $.path:" prefix or the
// "secret ... not found" / "invalid argument ..." wording.

// TestGenesisContractErrorCodesOptInPreservesScrapedShape drives a real
// merge failure through main() with GRAFT_ERROR_CODES=1 set and confirms
// genesis's _adaptive_merge regex (genesisAdaptiveMergeErrorRx, declared in
// genesis_error_format_test.go, this package) still matches every stderr
// line, capturing the same path it captures with the env var unset. Wave
// E2 is free to change what comes after "$.<path>: " (it adds a "[Ecode] "
// prefix to the message segment); it may never change the "$.<path>: "
// prefix itself, since that is what genesis's regex, and Genesis's
// vault-not-found substring check within it, key off. See
// genesis-compat-contract.md:53-69 and plan rows R3/R3b.
func TestGenesisContractErrorCodesOptInPreservesScrapedShape(t *testing.T) {
	t.Setenv("GRAFT_ERROR_CODES", "1")

	stderr, rc := runGraftCapturingOutput(t, []string{"merge", "../../assets/errors/multi.yml"})

	if rc != 2 {
		t.Fatalf("rc = %d, want 2", rc)
	}

	lines := adaptiveMergeErrorLines(stderr)
	if len(lines) != 1 {
		t.Fatalf("adaptiveMergeErrorLines(stderr) = %v, want exactly 1 line", lines)
	}

	m := genesisAdaptiveMergeErrorRx.FindStringSubmatch(lines[0])
	if m == nil {
		t.Fatalf("stderr line %q does not match genesis's _adaptive_merge regex with GRAFT_ERROR_CODES=1 set", lines[0])
	}

	path, message := m[1], m[2]
	if path != "an-error" {
		t.Fatalf("captured path = %q, want %q (GRAFT_ERROR_CODES must not move the path capture)", path, "an-error")
	}

	// The opted-in message segment gains an "[Ecode] " prefix (E204 =
	// CodeParamRequired, see pkg/graft/errors.go), but the regex above
	// already proved the "$.<path>: " prefix ahead of it is untouched; this
	// assertion confirms the code annotation is genuinely active for this
	// run, not silently no-op, so the "preserves the shape" claim above is
	// exercising a real opted-in code path rather than an accidental no-op.
	const wantMessage = "[E204] missing param!"
	if message != wantMessage {
		t.Fatalf("captured message = %q, want %q", message, wantMessage)
	}

	if stderr == "" {
		t.Fatalf("expected non-empty stderr")
	}
}

// TestGenesisContractSkipEvalJSONPipelineSurvivesMergeDashDashDash pins
// contract pattern 10 (genesis-compat-contract.md:60): `graft merge
// --multi-doc --go-patch --skip-eval files... | graft json`, used to
// build the unevaluated tree genesis looks values up in for
// deferred-operator rewriting. `graft merge` output now leads with a
// "---\n" document-start marker (renderMergedTree, cmd/graft/main.go);
// this drives both commands through main() in sequence, piping the
// first's captured stdout into the second's stdin exactly as the shell
// pipe does, and confirms `graft json` still parses it - the leading
// "---\n" is standard YAML (a document-start marker), not new content,
// so it must not need any special handling downstream.
func TestGenesisContractSkipEvalJSONPipelineSurvivesMergeDashDashDash(t *testing.T) {
	prevPrintStdOutf := printStdOutf
	prevPrintStdErrf := log.PrintStdErrf
	prevExit := exit
	prevUsage := usage
	prevArgs := os.Args
	prevStdin := os.Stdin
	defer func() {
		printStdOutf = prevPrintStdOutf
		log.PrintStdErrf = prevPrintStdErrf
		exit = prevExit
		usage = prevUsage
		os.Args = prevArgs
		os.Stdin = prevStdin
	}()

	var stdout, stderr string
	rc := 256
	printStdOutf = func(format string, args ...interface{}) {
		stdout += fmt.Sprintf(format, args...)
	}
	log.PrintStdErrf = func(format string, args ...interface{}) {
		stderr += fmt.Sprintf(format, args...)
	}
	exit = func(code int) { rc = code }
	usage = func() { exit(1) }

	os.Args = []string{"graft", "merge", "--skip-eval", "../../assets/merge/first.yml"}
	main()
	if rc != 0 {
		t.Fatalf("merge --skip-eval rc = %d, stderr: %s", rc, stderr)
	}
	if !strings.HasPrefix(stdout, "---\n") {
		t.Fatalf("merge --skip-eval stdout does not lead with \"---\\n\" (test premise broken): %q", stdout)
	}
	mergedOutput := stdout

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	go func() {
		defer func() { _ = w.Close() }()
		_, _ = w.WriteString(mergedOutput)
	}()
	os.Stdin = r

	stdout, stderr, rc = "", "", 256
	os.Args = []string{"graft", "json"}
	main()
	if rc != 0 {
		t.Fatalf("json rc = %d, stderr: %s", rc, stderr)
	}
	if stdout == "" {
		t.Fatal("json produced no output")
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("json output did not parse as JSON: %v\noutput: %s", err, stdout)
	}
	if _, ok := decoded["array_append"]; !ok {
		t.Fatalf("decoded JSON missing expected key %q (from assets/merge/first.yml): %v", "array_append", decoded)
	}
}
