#!/usr/bin/env bash
#
# Bash-level test for genesis's vaultinfo pipeline pattern:
#
#   set -o pipefail; graft vaultinfo <file> | graft json
#
# Genesis (lib/Genesis/Env/ManifestProvider.pm) relies on three properties of
# this exact pipeline, each covered by one case below:
#
#   1. Under `pipefail`, a `vaultinfo` failure propagates through the pipe as
#      a non-zero pipeline exit code, even though `json` itself exits 0 on
#      empty stdin. Without `pipefail` the failure would be silently masked.
#   2. On success, `vaultinfo | json` produces JSON shaped
#      {"secrets":[{"key":...,"references":[...]}]}, which genesis decodes
#      directly.
#   3. On an unresolvable node, `vaultinfo`'s stderr lines match genesis's
#      vault_paths() regex `^\s*-\s*\$\.(\S+?):` so it can extract retry
#      paths.
#
# This script MUST run under bash (not sh/dash) because genesis always execs
# spruce/graft via `bash -c "$prog" -- "$@"` (lib/Genesis.pm:605), and
# `pipefail` semantics are bash-specific.

if [ -z "${BASH_VERSION:-}" ]; then
  echo "FAIL: this script must be run with bash, not sh/dash (got: $0 under $(readlink -f /proc/$$/exe 2>/dev/null || echo unknown))" >&2
  exit 1
fi

set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FIXTURE_DIR="$SCRIPT_DIR/vaultinfo"
WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/graft-vaultinfo-pipefail.XXXXXX")"
GRAFT_BIN="$WORKDIR/graft"

cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

FAILURES=0

pass() {
  printf 'PASS: %s\n' "$1"
}

fail() {
  printf 'FAIL: %s\n' "$1"
  FAILURES=$((FAILURES + 1))
}

for f in invalid.yml success.yml unresolvable.yml; do
  if [ ! -f "$FIXTURE_DIR/$f" ]; then
    echo "FAIL: missing fixture $FIXTURE_DIR/$f" >&2
    exit 1
  fi
done

echo "Building graft from $REPO_ROOT ..."
if ! (cd "$REPO_ROOT" && go build -o "$GRAFT_BIN" ./cmd/graft); then
  echo "FAIL: go build ./cmd/graft failed" >&2
  exit 1
fi
if [ ! -x "$GRAFT_BIN" ]; then
  echo "FAIL: graft binary not produced at $GRAFT_BIN" >&2
  exit 1
fi

# --- Case 1: failure path -- prove pipefail is load-bearing ---------------
#
# genesis's exact concern (ManifestProvider.pm comment ~420-426): without
# `pipefail`, `graft json`'s own exit 0 on empty stdin masks a failing
# `vaultinfo`. We assert both directions to prove the mechanism, not just
# the pipefail-on outcome.

FAIL_FIXTURE="$FIXTURE_DIR/invalid.yml"

(
  set -o pipefail
  "$GRAFT_BIN" vaultinfo "$FAIL_FIXTURE" 2>/dev/null | "$GRAFT_BIN" json >/dev/null 2>/dev/null
)
rc_with_pipefail=$?
if [ "$rc_with_pipefail" -ne 0 ]; then
  pass "case1a: WITH pipefail, failing vaultinfo pipeline exits non-zero (rc=$rc_with_pipefail)"
else
  fail "case1a: WITH pipefail, failing vaultinfo pipeline exited 0 (expected non-zero)"
fi

(
  set +o pipefail
  "$GRAFT_BIN" vaultinfo "$FAIL_FIXTURE" 2>/dev/null | "$GRAFT_BIN" json >/dev/null 2>/dev/null
)
rc_without_pipefail=$?
if [ "$rc_without_pipefail" -eq 0 ]; then
  pass "case1b: WITHOUT pipefail, the same pipeline masks the failure and exits 0 (rc=$rc_without_pipefail) -- confirms pipefail is load-bearing"
else
  fail "case1b: WITHOUT pipefail, pipeline unexpectedly exited non-zero (rc=$rc_without_pipefail) -- expected 0, mechanism not demonstrated"
fi

# --- Case 2: success path -- JSON shape genesis decodes -------------------

SUCCESS_FIXTURE="$FIXTURE_DIR/success.yml"
SUCCESS_JSON="$WORKDIR/success.json"

(
  set -o pipefail
  "$GRAFT_BIN" vaultinfo "$SUCCESS_FIXTURE" | "$GRAFT_BIN" json >"$SUCCESS_JSON"
)
rc_success=$?

if [ "$rc_success" -ne 0 ]; then
  fail "case2: success-path pipeline exited non-zero (rc=$rc_success), expected 0"
else
  pass "case2a: success-path pipeline exits 0"
fi

if command -v jq >/dev/null 2>&1; then
  key_count=$(jq -r '.secrets | length' "$SUCCESS_JSON" 2>/dev/null)
  first_key=$(jq -r '.secrets[0].key // empty' "$SUCCESS_JSON" 2>/dev/null)
  first_refs_len=$(jq -r '.secrets[0].references | length' "$SUCCESS_JSON" 2>/dev/null)
else
  key_count=$(python3 -c '
import json, sys
try:
    with open(sys.argv[1]) as f:
        data = json.load(f)
    print(len(data.get("secrets", [])))
except Exception:
    print(0)
' "$SUCCESS_JSON" 2>/dev/null)
  first_key=$(python3 -c '
import json, sys
try:
    with open(sys.argv[1]) as f:
        data = json.load(f)
    secrets = data.get("secrets", [])
    print(secrets[0].get("key", "") if secrets else "")
except Exception:
    print("")
' "$SUCCESS_JSON" 2>/dev/null)
  first_refs_len=$(python3 -c '
import json, sys
try:
    with open(sys.argv[1]) as f:
        data = json.load(f)
    secrets = data.get("secrets", [])
    refs = secrets[0].get("references", []) if secrets else []
    print(len(refs))
except Exception:
    print(0)
' "$SUCCESS_JSON" 2>/dev/null)
fi

if [ "${key_count:-0}" -ge 1 ] 2>/dev/null; then
  pass "case2b: output parses as JSON with at least one entry in secrets[]"
else
  fail "case2b: output did not parse as valid JSON with a non-empty secrets[] array (got: $(cat "$SUCCESS_JSON" 2>/dev/null))"
fi

if [ "$first_key" = "secret/x:y" ]; then
  pass "case2c: secrets[0].key == \"secret/x:y\""
else
  fail "case2c: secrets[0].key mismatch, got \"$first_key\", expected \"secret/x:y\""
fi

if [ "${first_refs_len:-0}" -ge 1 ] 2>/dev/null; then
  pass "case2d: secrets[0].references is non-empty"
else
  fail "case2d: secrets[0].references missing or empty"
fi

# --- Case 3: stderr-scrape path -- genesis's vault_paths() regex ----------
#
# ManifestProvider.pm vault_paths() scrapes vaultinfo stderr with
# /^\s*-\s*\$\.(\S+?):/mg to find unresolvable node paths for its
# retry-with-prune loop.

UNRESOLVABLE_FIXTURE="$FIXTURE_DIR/unresolvable.yml"
STDERR_FILE="$WORKDIR/unresolvable.stderr"

"$GRAFT_BIN" vaultinfo "$UNRESOLVABLE_FIXTURE" >/dev/null 2>"$STDERR_FILE"
rc_unresolvable=$?

if [ "$rc_unresolvable" -eq 0 ]; then
  fail "case3a: unresolvable-node fixture unexpectedly succeeded (rc=0), no stderr to scrape"
else
  pass "case3a: unresolvable-node fixture fails as expected (rc=$rc_unresolvable)"
fi

if grep -qE '^[[:space:]]*-[[:space:]]*\$\.[^:]+:' "$STDERR_FILE"; then
  pass "case3b: stderr contains a line matching genesis's vault_paths() regex (^\\s*-\\s*\\\$\\.(\\S+?):)"
else
  fail "case3b: no stderr line matched genesis's vault_paths() regex; stderr was: $(cat "$STDERR_FILE")"
fi

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "ALL PASS (vaultinfo-pipefail.sh)"
  exit 0
else
  echo "$FAILURES case(s) FAILED (vaultinfo-pipefail.sh)"
  exit 1
fi
