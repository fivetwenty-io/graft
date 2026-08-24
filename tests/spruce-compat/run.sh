#!/usr/bin/env bash
# Golden-output parity harness: runs both `spruce` and `graft` over fixture
# inputs covering each of genesis's 16 documented spruce invocation
# patterns and compares results per the semantic-parity bar this harness
# implements: exit codes must match exactly;
# stdout is compared semantically (YAML/JSON re-parsed and structurally
# diffed, byte-identical noted where it holds); stderr is compared only on
# the surfaces genesis actually scrapes (` - $.path: msg` line format,
# `secret X not found` substring).
#
# Baseline configuration: graft's default (no special env/flags) IS the
# parallel-enabled baseline by default — this harness invokes
# graft with no concurrency overrides so every PASS here validates that
# default.
#
# Usage:
#   bash tests/spruce-compat/run.sh
#
# Env overrides:
#   GRAFT_BIN   - path to a prebuilt graft binary (default: build fresh)
#   SPRUCE_BIN  - path to a prebuilt spruce binary (default: PATH lookup,
#                 then build from SPRUCE_REPO)
#   SPRUCE_REPO - path to a spruce source checkout used to build spruce
#                 when it is not already on PATH or given via SPRUCE_BIN
#                 (default: sibling `../spruce` of this repo)
#
# Exit code: 0 if every non-skipped pattern passed (or the whole harness
# skipped gracefully because spruce is unavailable); 1 if any pattern
# failed. Must run under bash — genesis's own shell for spawning subprocess
# invocations — not sh/dash.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FIX="$SCRIPT_DIR/fixtures"

# shellcheck source=lib/harness.sh
source "$SCRIPT_DIR/lib/harness.sh"
# shellcheck source=lib/spruce-bin.sh
source "$SCRIPT_DIR/lib/spruce-bin.sh"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/spruce-compat.XXXXXX")"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

# --- binary resolution -------------------------------------------------

resolve_graft() {
  if [ -n "${GRAFT_BIN:-}" ] && [ -x "${GRAFT_BIN:-}" ]; then
    return 0
  fi
  local out="$TMP/graft"
  if ! (cd "$REPO_ROOT" && go build -o "$out" ./cmd/graft) >"$TMP/graft-build.log" 2>&1; then
    echo "FATAL: failed to build graft from $REPO_ROOT/cmd/graft:" >&2
    cat "$TMP/graft-build.log" >&2
    exit 1
  fi
  GRAFT_BIN="$out"
}

resolve_spruce() {
  # Shared with the other runners (lib/spruce-bin.sh): $SPRUCE_BIN, then
  # a `spruce` on PATH, then a build from $SPRUCE_REPO. A graft installed
  # under a spruce name is rejected at every step, since comparing graft
  # with itself is not the parity signal this harness reports.
  local found
  found="$(spruce_bin_resolve "$TMP" "$REPO_ROOT")" || return 1
  SPRUCE_BIN="$found"
  return 0
}

resolve_graft
if ! resolve_spruce; then
  echo "SKIP: spruce binary not found on PATH and not buildable from SPRUCE_REPO (${SPRUCE_REPO:-$REPO_ROOT/../spruce})."
  echo "SKIP: spruce/graft parity harness cannot run without a spruce binary to compare against. Set SPRUCE_BIN or SPRUCE_REPO to enable it."
  exit 0
fi

echo "graft:  $GRAFT_BIN ($("$GRAFT_BIN" -v 2>&1))"
echo "spruce: $SPRUCE_BIN ($("$SPRUCE_BIN" -v 2>&1))"
echo

# ========================================================================
# Pattern 1 — `spruce diff <a> <b>`
# Genesis wraps this in a pty (fake_tty) so spruce colorizes; this harness
# runs both tools in piped (non-tty) mode, which is the mode both tools
# use when stdout is not a terminal. That is the portable, deterministic
# case to assert here. The pty/ANSI-colorized case is NOT exercised by
# this harness (would require a real pty allocation in CI) — noted as a
# documented gap, not silently skipped.
# ========================================================================
pattern_01_diff() {
  local name="pattern 1: spruce diff <a> <b> (piped, no-ANSI)"
  run_tool "$SPRUCE_BIN" "$TMP/p1.s.out" "$TMP/p1.s.err" NONE -- diff "$FIX/diff-a.yml" "$FIX/diff-b.yml"
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p1.g.out" "$TMP/p1.g.err" NONE -- diff "$FIX/diff-a.yml" "$FIX/diff-b.yml"
  local g_rc=$RC

  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity raw "$TMP/p1.s.out" "$TMP/p1.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
  report_skip "pattern 1b: spruce diff under a pty (ANSI coloring)" \
    "not exercised — requires a pty allocation (script(1)/openpty) not attempted by this harness; genesis's fake_tty wrapper is the real consumer of this mode"
}

# ========================================================================
# Pattern 2 — `spruce json < file` (stdin redirect)
# ========================================================================
pattern_02_json_stdin_redirect() {
  local name="pattern 2: spruce json < file"
  run_tool "$SPRUCE_BIN" "$TMP/p2.s.out" "$TMP/p2.s.err" "$FIX/base.yml" -- json
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p2.g.out" "$TMP/p2.g.err" "$FIX/base.yml" -- json
  local g_rc=$RC

  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity json "$TMP/p2.s.out" "$TMP/p2.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 3 — `spruce merge --skip-eval <json-tmpfile>` (genesis's
# save_to_yaml_file's JSON-tmpfile-to-YAML path; the perl `))`-rejoin
# post-processing is a genesis-side hack applied AFTER this call and is
# out of scope for this harness — it only needs graft's raw stdout to
# match spruce's raw stdout here).
# ========================================================================
pattern_03_skip_eval_json_input() {
  local name="pattern 3: spruce merge --skip-eval <json-tmpfile>"
  run_tool "$SPRUCE_BIN" "$TMP/p3.s.out" "$TMP/p3.s.err" NONE -- merge --skip-eval "$FIX/base.json"
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p3.g.out" "$TMP/p3.g.err" NONE -- merge --skip-eval "$FIX/base.json"
  local g_rc=$RC

  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity yaml "$TMP/p3.s.out" "$TMP/p3.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 4 — `spruce merge --skip-eval <file>` (YAML input)
# ========================================================================
pattern_04_skip_eval_yaml_input() {
  local name="pattern 4: spruce merge --skip-eval <file>"
  run_tool "$SPRUCE_BIN" "$TMP/p4.s.out" "$TMP/p4.s.err" NONE -- merge --skip-eval "$FIX/base.yml"
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p4.g.out" "$TMP/p4.g.err" NONE -- merge --skip-eval "$FIX/base.yml"
  local g_rc=$RC

  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity yaml "$TMP/p4.s.out" "$TMP/p4.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 5 — `spruce merge --go-patch <files...>` (full
# eval, go-patch enabled, no multi-doc/skip-eval — that combination is
# covered separately by patterns 9/10/11/15).
# ========================================================================
pattern_05_merge_gopatch() {
  local name="pattern 5: spruce merge --go-patch <base> <patch-ops>"
  run_tool "$SPRUCE_BIN" "$TMP/p5.s.out" "$TMP/p5.s.err" NONE -- merge --go-patch "$FIX/gopatch-base.yml" "$FIX/gopatch-ops.yml"
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p5.g.out" "$TMP/p5.g.err" NONE -- merge --go-patch "$FIX/gopatch-base.yml" "$FIX/gopatch-ops.yml"
  local g_rc=$RC

  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity yaml "$TMP/p5.s.out" "$TMP/p5.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 6 — `spruce merge --skip-eval <files> > fout` with
# `--cherry-pick`/`--prune`. Two sub-cases: cherry-pick a
# top-level key, and prune a top-level key. Output redirection to a file
# is semantically identical to capturing stdout, which is what this
# harness already does for every pattern.
# ========================================================================
pattern_06_skip_eval_cherry_and_prune() {
  local name_cp="pattern 6a: spruce merge --skip-eval --cherry-pick releases <files> > fout"
  run_tool "$SPRUCE_BIN" "$TMP/p6cp.s.out" "$TMP/p6cp.s.err" NONE -- merge --skip-eval --cherry-pick releases "$FIX/cherry-a.yml" "$FIX/cherry-b.yml"
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p6cp.g.out" "$TMP/p6cp.g.err" NONE -- merge --skip-eval --cherry-pick releases "$FIX/cherry-a.yml" "$FIX/cherry-b.yml"
  local g_rc=$RC
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name_cp" "exit code mismatch: spruce=$s_rc graft=$g_rc"
  elif assert_stdout_parity yaml "$TMP/p6cp.s.out" "$TMP/p6cp.g.out"; then
    report_pass "$name_cp" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name_cp" "exit=$s_rc (matched); $DETAIL_OUT"
  fi

  local name_pr="pattern 6b: spruce merge --skip-eval --prune secret_stuff <file> > fout"
  run_tool "$SPRUCE_BIN" "$TMP/p6pr.s.out" "$TMP/p6pr.s.err" NONE -- merge --skip-eval --prune secret_stuff "$FIX/prune-source.yml"
  s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p6pr.g.out" "$TMP/p6pr.g.err" NONE -- merge --skip-eval --prune secret_stuff "$FIX/prune-source.yml"
  g_rc=$RC
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name_pr" "exit code mismatch: spruce=$s_rc graft=$g_rc"
  elif assert_stdout_parity yaml "$TMP/p6pr.s.out" "$TMP/p6pr.g.out"; then
    report_pass "$name_pr" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name_pr" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 7 — `spruce json <file> | jq '."key"' | spruce merge --skip-eval`
# (jq-filtered subset re-merged). Runs the SAME tool on
# both ends of the pipe for a fair comparison; jq is the fixed, shared
# middle stage.
# ========================================================================
pattern_07_json_jq_merge() {
  local name="pattern 7: spruce json | jq '.properties' | spruce merge --skip-eval -"

  ( set -o pipefail
    "$SPRUCE_BIN" json "$FIX/jq-source.yml" </dev/null 2>"$TMP/p7.s.mid.err" \
      | jq '.properties' 2>"$TMP/p7.s.jq.err" \
      | "$SPRUCE_BIN" merge --skip-eval - >"$TMP/p7.s.out" 2>"$TMP/p7.s.err"
  )
  local s_rc=$?

  ( set -o pipefail
    "$GRAFT_BIN" json "$FIX/jq-source.yml" </dev/null 2>"$TMP/p7.g.mid.err" \
      | jq '.properties' 2>"$TMP/p7.g.jq.err" \
      | "$GRAFT_BIN" merge --skip-eval - >"$TMP/p7.g.out" 2>"$TMP/p7.g.err"
  )
  local g_rc=$?

  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity yaml "$TMP/p7.s.out" "$TMP/p7.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 8 — `set -o pipefail; spruce vaultinfo <file> | spruce json`
# Two sub-cases: (a) success path shape/content, (b) a
# vaultinfo failure (missing file) must propagate a non-zero pipeline
# exit code under pipefail even though the downstream `json` stage would
# itself exit 0 on empty stdin — this is the exact masking bug genesis's
# own comment warns about (ManifestProvider.pm:420-426).
# ========================================================================
pattern_08_vaultinfo_json_pipefail() {
  local name_ok="pattern 8a: set -o pipefail; spruce vaultinfo <file> | spruce json (success path)"
  ( set -o pipefail
    "$SPRUCE_BIN" vaultinfo "$FIX/vault-source.yml" </dev/null 2>"$TMP/p8ok.s.mid.err" | "$SPRUCE_BIN" json >"$TMP/p8ok.s.out" 2>"$TMP/p8ok.s.err"
  )
  local s_rc=$?
  ( set -o pipefail
    "$GRAFT_BIN" vaultinfo "$FIX/vault-source.yml" </dev/null 2>"$TMP/p8ok.g.mid.err" | "$GRAFT_BIN" json >"$TMP/p8ok.g.out" 2>"$TMP/p8ok.g.err"
  )
  local g_rc=$?
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name_ok" "exit code mismatch: spruce=$s_rc graft=$g_rc"
  elif assert_stdout_parity json "$TMP/p8ok.s.out" "$TMP/p8ok.g.out"; then
    report_pass "$name_ok" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name_ok" "exit=$s_rc (matched); $DETAIL_OUT"
  fi

  local name_fail="pattern 8b: pipefail propagates vaultinfo failure through | json"
  ( set -o pipefail
    "$SPRUCE_BIN" vaultinfo "$FIX/does-not-exist.yml" </dev/null 2>"$TMP/p8fail.s.mid.err" | "$SPRUCE_BIN" json >"$TMP/p8fail.s.out" 2>"$TMP/p8fail.s.err"
  )
  s_rc=$?
  ( set -o pipefail
    "$GRAFT_BIN" vaultinfo "$FIX/does-not-exist.yml" </dev/null 2>"$TMP/p8fail.g.mid.err" | "$GRAFT_BIN" json >"$TMP/p8fail.g.out" 2>"$TMP/p8fail.g.err"
  )
  g_rc=$?
  if [ "$s_rc" -eq 0 ] || [ "$g_rc" -eq 0 ]; then
    report_fail "$name_fail" "pipefail did not propagate a failure as expected: spruce_rc=$s_rc graft_rc=$g_rc (both must be non-zero)"
  elif ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name_fail" "both non-zero (pipefail worked) but codes differ: spruce=$s_rc graft=$g_rc"
  else
    report_pass "$name_fail" "both propagate non-zero exit=$s_rc under pipefail, matching genesis's pipefail requirement"
  fi
}

# ========================================================================
# Pattern 9 — `spruce merge --multi-doc --go-patch <files>` (adaptive/
# adaptive/no skip-eval). Three sub-cases:
#   9a: success path on a valid multi-doc fixture
#   9b: an error path exercising the adaptive-merge stderr line format
#   9c: a vault-op fixture merged under REDACT=1 (no live vault backend
#       available in this environment — REDACT lets both tools resolve
#       without one)
# ========================================================================
pattern_09_multidoc_gopatch_adaptive() {
  local name_ok="pattern 9a: spruce merge --multi-doc --go-patch <multidoc file>"
  run_tool "$SPRUCE_BIN" "$TMP/p9ok.s.out" "$TMP/p9ok.s.err" NONE -- merge --multi-doc --go-patch "$FIX/multidoc.yml"
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p9ok.g.out" "$TMP/p9ok.g.err" NONE -- merge --multi-doc --go-patch "$FIX/multidoc.yml"
  local g_rc=$RC
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name_ok" "exit code mismatch: spruce=$s_rc graft=$g_rc"
  elif assert_stdout_parity yaml "$TMP/p9ok.s.out" "$TMP/p9ok.g.out"; then
    report_pass "$name_ok" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name_ok" "exit=$s_rc (matched); $DETAIL_OUT"
  fi

  local name_err="pattern 9b: adaptive-merge error format on stderr (unresolved param/grab/concat)"
  run_tool "$SPRUCE_BIN" "$TMP/p9err.s.out" "$TMP/p9err.s.err" NONE -- merge --multi-doc --go-patch "$FIX/error-source.yml"
  s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p9err.g.out" "$TMP/p9err.g.err" NONE -- merge --multi-doc --go-patch "$FIX/error-source.yml"
  g_rc=$RC
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name_err" "exit code mismatch: spruce=$s_rc graft=$g_rc"
  elif assert_adaptive_stderr_parity "$TMP/p9err.s.err" "$TMP/p9err.g.err"; then
    report_pass "$name_err" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name_err" "exit=$s_rc (matched); $DETAIL_OUT"
  fi

  local name_redact="pattern 9c: vault-op fixture merged under REDACT=1 (no live backend)"
  ( export REDACT=1
    run_tool "$SPRUCE_BIN" "$TMP/p9rd.s.out" "$TMP/p9rd.s.err" NONE -- merge --multi-doc --go-patch "$FIX/vault-source.yml"
    echo "$RC" >"$TMP/p9rd.s.rc"
    run_tool "$GRAFT_BIN" "$TMP/p9rd.g.out" "$TMP/p9rd.g.err" NONE -- merge --multi-doc --go-patch "$FIX/vault-source.yml"
    echo "$RC" >"$TMP/p9rd.g.rc"
  )
  s_rc="$(cat "$TMP/p9rd.s.rc")"
  g_rc="$(cat "$TMP/p9rd.g.rc")"
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name_redact" "exit code mismatch: spruce=$s_rc graft=$g_rc"
  elif ! grep -q 'REDACTED' "$TMP/p9rd.s.out" || ! grep -q 'REDACTED' "$TMP/p9rd.g.out"; then
    report_fail "$name_redact" "expected REDACTED literal in both outputs without a live vault backend (REDACT=1)"
  elif assert_stdout_parity yaml "$TMP/p9rd.s.out" "$TMP/p9rd.g.out"; then
    report_pass "$name_redact" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name_redact" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 10 — `spruce merge --multi-doc --go-patch --skip-eval <files> |
# spruce json` (unevaluated-tree JSON for deferred-operator
# lookups).
# ========================================================================
pattern_10_multidoc_gopatch_skipeval_json() {
  local name="pattern 10: spruce merge --multi-doc --go-patch --skip-eval <files> | spruce json"
  ( set -o pipefail
    "$SPRUCE_BIN" merge --multi-doc --go-patch --skip-eval "$FIX/multidoc.yml" </dev/null 2>"$TMP/p10.s.mid.err" | "$SPRUCE_BIN" json >"$TMP/p10.s.out" 2>"$TMP/p10.s.err"
  )
  local s_rc=$?
  ( set -o pipefail
    "$GRAFT_BIN" merge --multi-doc --go-patch --skip-eval "$FIX/multidoc.yml" </dev/null 2>"$TMP/p10.g.mid.err" | "$GRAFT_BIN" json >"$TMP/p10.g.out" 2>"$TMP/p10.g.err"
  )
  local g_rc=$?
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity json "$TMP/p10.s.out" "$TMP/p10.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 11 — `spruce merge --go-patch --multi-doc <files> | spruce json`
# (kit metadata JSON — full eval, no skip-eval, piped to
# json instead of raw stdout).
# ========================================================================
pattern_11_gopatch_multidoc_json() {
  local name="pattern 11: spruce merge --go-patch --multi-doc <files> | spruce json"
  ( set -o pipefail
    "$SPRUCE_BIN" merge --go-patch --multi-doc "$FIX/multidoc.yml" </dev/null 2>"$TMP/p11.s.mid.err" | "$SPRUCE_BIN" json >"$TMP/p11.s.out" 2>"$TMP/p11.s.err"
  )
  local s_rc=$?
  ( set -o pipefail
    "$GRAFT_BIN" merge --go-patch --multi-doc "$FIX/multidoc.yml" </dev/null 2>"$TMP/p11.g.mid.err" | "$GRAFT_BIN" json >"$TMP/p11.g.out" 2>"$TMP/p11.g.err"
  )
  local g_rc=$?
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity json "$TMP/p11.s.out" "$TMP/p11.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 12 — `cat <file> | spruce merge --skip-eval -` (stdin `-`
# stdin `-` sentinel via a JSON tmpfile in genesis's real usage; exercised
# here with the same base.json fixture as pattern 3, but through a pipe
# and the `-` sentinel rather than a positional file argument).
# ========================================================================
pattern_12_stdin_dash_skipeval() {
  local name="pattern 12: cat file | spruce merge --skip-eval -"
  ( set -o pipefail
    cat "$FIX/base.json" | "$SPRUCE_BIN" merge --skip-eval - >"$TMP/p12.s.out" 2>"$TMP/p12.s.err"
  )
  local s_rc=$?
  ( set -o pipefail
    cat "$FIX/base.json" | "$GRAFT_BIN" merge --skip-eval - >"$TMP/p12.g.out" 2>"$TMP/p12.g.err"
  )
  local g_rc=$?
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity yaml "$TMP/p12.s.out" "$TMP/p12.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 13 — `spruce merge --skip-eval --go-patch -m --cherry-pick
# releases <files>` (confirms `-m` short-flag support
# alongside long flags and --cherry-pick).
# ========================================================================
pattern_13_short_m_cherrypick() {
  local name="pattern 13: spruce merge --skip-eval --go-patch -m --cherry-pick releases <files>"
  run_tool "$SPRUCE_BIN" "$TMP/p13.s.out" "$TMP/p13.s.err" NONE -- merge --skip-eval --go-patch -m --cherry-pick releases "$FIX/cherry-a.yml" "$FIX/cherry-b.yml"
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p13.g.out" "$TMP/p13.g.err" NONE -- merge --skip-eval --go-patch -m --cherry-pick releases "$FIX/cherry-a.yml" "$FIX/cherry-b.yml"
  local g_rc=$RC
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity yaml "$TMP/p13.s.out" "$TMP/p13.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 14 — `echo "$fin" | spruce merge "$@" -` with `--prune`
# pattern 14, manifest content piped via echo rather than a file, stdin
# `-` sentinel, full eval — no skip-eval).
# ========================================================================
pattern_14_echo_stdin_prune() {
  local name="pattern 14: echo \"\$fin\" | spruce merge --prune secret_stuff -"
  local fin
  fin="$(cat "$FIX/prune-source.yml")"
  ( set -o pipefail
    echo "$fin" | "$SPRUCE_BIN" merge --prune secret_stuff - >"$TMP/p14.s.out" 2>"$TMP/p14.s.err"
  )
  local s_rc=$?
  ( set -o pipefail
    echo "$fin" | "$GRAFT_BIN" merge --prune secret_stuff - >"$TMP/p14.g.out" 2>"$TMP/p14.g.err"
  )
  local g_rc=$?
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity yaml "$TMP/p14.s.out" "$TMP/p14.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 15 — `cat <file> | spruce merge --skip-eval --go-patch
# --multi-doc | spruce json` (the 3-way go-patch +
# multi-doc + skip-eval combination).
#
# Two sub-cases, since `--multi-doc` merge on a SINGLE input source
# collapses same-file documents into one merged result (later doc wins
# on conflicting keys — confirmed identical on both tools by hand-probe
# before writing this harness, disjoint-key case included), so the
# pipeline's own `json` stage naturally emits exactly one line for this
# fixture:
#   15a: parity of the full 3-way-flag pipeline's actual output (no
#        line-count assumption baked in — whatever both tools produce,
#        they must agree)
#   15b: the actual "one JSON object per line" contract genesis's
#        Env.pm:6608 `lines($out)` depends on — this belongs to `json`
#        applied directly to multi-doc YAML input, verified here
#        on the same multidoc.yml fixture without going through merge
#        first, so document separation is never collapsed.
# ========================================================================
pattern_15_threeway_multidoc_json_lines() {
  local name_a="pattern 15a: cat file | spruce merge --skip-eval --go-patch --multi-doc | spruce json"
  ( set -o pipefail
    cat "$FIX/multidoc.yml" | "$SPRUCE_BIN" merge --skip-eval --go-patch --multi-doc - 2>"$TMP/p15.s.mid.err" | "$SPRUCE_BIN" json >"$TMP/p15.s.out" 2>"$TMP/p15.s.err"
  )
  local s_rc=$?
  ( set -o pipefail
    cat "$FIX/multidoc.yml" | "$GRAFT_BIN" merge --skip-eval --go-patch --multi-doc - 2>"$TMP/p15.g.mid.err" | "$GRAFT_BIN" json >"$TMP/p15.g.out" 2>"$TMP/p15.g.err"
  )
  local g_rc=$?
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name_a" "exit code mismatch: spruce=$s_rc graft=$g_rc"
  elif assert_stdout_parity jsonlines "$TMP/p15.s.out" "$TMP/p15.g.out"; then
    report_pass "$name_a" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name_a" "exit=$s_rc (matched); $DETAIL_OUT"
  fi

  local name_b="pattern 15b: spruce json <multi-doc file> emits one JSON object per line (Env.pm:6608 lines(\$out) contract)"
  run_tool "$SPRUCE_BIN" "$TMP/p15b.s.out" "$TMP/p15b.s.err" NONE -- json "$FIX/multidoc.yml"
  s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p15b.g.out" "$TMP/p15b.g.err" NONE -- json "$FIX/multidoc.yml"
  g_rc=$RC
  local s_lines g_lines
  s_lines=$(grep -c . "$TMP/p15b.s.out" || true)
  g_lines=$(grep -c . "$TMP/p15b.g.out" || true)
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name_b" "exit code mismatch: spruce=$s_rc graft=$g_rc"
  elif [ "$s_lines" != "2" ] || [ "$g_lines" != "2" ]; then
    report_fail "$name_b" "expected 2 JSON lines (one per multi-doc document, no merge involved): spruce got $s_lines, graft got $g_lines"
  elif assert_stdout_parity jsonlines "$TMP/p15b.s.out" "$TMP/p15b.g.out"; then
    report_pass "$name_b" "exit=$s_rc; 2 JSON lines each; $DETAIL_OUT"
  else
    report_fail "$name_b" "exit=$s_rc (matched), line count matched (2 each); $DETAIL_OUT"
  fi
}

# ========================================================================
# Pattern 16 — `Genesis::Hook::spruce_merge`: `spruce merge <opts> <files>`
# with kit-hook-supplied files. Hash args are serialized
# to a YAML tempfile before the call in genesis; simulated here with a
# plain fixture file standing in for that tempfile, merged against the
# base fixture with full evaluation (no special flags — genesis's hook
# wrapper passes through whatever opts/files the kit hook author chose).
# ========================================================================
pattern_16_hook_merge() {
  local name="pattern 16: spruce merge <hook-tempfile> <base-file> (Genesis::Hook::spruce_merge)"
  run_tool "$SPRUCE_BIN" "$TMP/p16.s.out" "$TMP/p16.s.err" NONE -- merge "$FIX/hook-source.yml" "$FIX/base.yml"
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/p16.g.out" "$TMP/p16.g.err" NONE -- merge "$FIX/hook-source.yml" "$FIX/base.yml"
  local g_rc=$RC
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity yaml "$TMP/p16.s.out" "$TMP/p16.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Auxiliary: additional --multi-doc / array-merge coverage requested
# independently of the 16-pattern checklist (base+override key-merge
# across grab/concat/array-name-merge, the dominant genesis operators
# the dominant operators in real genesis kit/deployment usage).
# ========================================================================
pattern_aux_base_override_merge() {
  local name="aux: full-eval merge of base.yml + override.yml (grab/concat/array key-merge)"
  run_tool "$SPRUCE_BIN" "$TMP/paux.s.out" "$TMP/paux.s.err" NONE -- merge "$FIX/base.yml" "$FIX/override.yml"
  local s_rc=$RC
  run_tool "$GRAFT_BIN" "$TMP/paux.g.out" "$TMP/paux.g.err" NONE -- merge "$FIX/base.yml" "$FIX/override.yml"
  local g_rc=$RC
  if ! assert_exit_parity "$s_rc" "$g_rc"; then
    report_fail "$name" "exit code mismatch: spruce=$s_rc graft=$g_rc"
    return
  fi
  if assert_stdout_parity yaml "$TMP/paux.s.out" "$TMP/paux.g.out"; then
    report_pass "$name" "exit=$s_rc; $DETAIL_OUT"
  else
    report_fail "$name" "exit=$s_rc (matched); $DETAIL_OUT"
  fi
}

# ========================================================================
# Documented, explicit skip: genesis's "secret X not found" stderr
# contract (vault_paths()'s /^secret (.*) not found/ match)
# requires an actual failed lookup against a live Vault backend to
# produce. No live vault is available in this environment; REDACT=1
# short-circuits before any backend call is made, so it cannot exercise
# this specific error text. Documented as a limitation rather than
# silently omitted.
# ========================================================================
report_secret_not_found_skip() {
  report_skip "stderr contract: \"secret X not found\" substring" \
    "requires a live Vault backend returning a real not-found error; none available in this environment. REDACT=1 (used for pattern 9c) short-circuits before any backend call and cannot produce this text. A backend-free unit-level contract test that pins this exact string is expected to live alongside the vault operator's own tests."
}

# --- run everything ------------------------------------------------------

pattern_01_diff
pattern_02_json_stdin_redirect
pattern_03_skip_eval_json_input
pattern_04_skip_eval_yaml_input
pattern_05_merge_gopatch
pattern_06_skip_eval_cherry_and_prune
pattern_07_json_jq_merge
pattern_08_vaultinfo_json_pipefail
pattern_09_multidoc_gopatch_adaptive
pattern_10_multidoc_gopatch_skipeval_json
pattern_11_gopatch_multidoc_json
pattern_12_stdin_dash_skipeval
pattern_13_short_m_cherrypick
pattern_14_echo_stdin_prune
pattern_15_threeway_multidoc_json_lines
pattern_16_hook_merge
pattern_aux_base_override_merge
report_secret_not_found_skip

echo
echo "============================================================"
echo "spruce/graft parity: $PASS_COUNT passed, $FAIL_COUNT failed, $SKIP_COUNT skipped"
if [ "$FAIL_COUNT" -gt 0 ]; then
  echo "Failed patterns:"
  for p in "${FAILED_PATTERNS[@]}"; do
    echo "  - $p"
  done
  exit 1
fi
exit 0
