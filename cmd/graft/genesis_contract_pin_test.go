package main

import (
	"testing"
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
