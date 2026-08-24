#!/usr/bin/env bash
# Byte-exact map-key-order parity runner.
#
# The main harness (run.sh) compares stdout SEMANTICALLY (re-parsed and
# structurally diffed), so it is blind to key reordering by design. This
# runner is the byte-exact gate for graft's spruce-compatible key
# ordering (numeric-looking keys first, numerically; then string keys in
# spruce's natural order — see pkg/graft/keysort.go).
#
# For every tests/spruce-compat/key-order/<name>.yml it byte-compares:
#   - `graft merge` stdout against <name>.graft.expected (always), and
#   - `spruce merge` stdout against <name>.spruce.expected (when a
#     spruce binary is available) to catch drift in spruce itself.
#
# The two expected files are byte-identical for string-only-key fixtures
# (except a quote-style difference: goccy double-quotes keys spruce
# single-quotes). For fixtures with bare numeric keys they differ only
# in key labels (spruce keeps typed keys bare, graft's coerced keys stay
# quoted strings) — key ORDER matches position-for-position, which is
# the parity this runner pins. No fuzzy key extraction: full stdout
# bytes only.
#
# Usage:
#   bash tests/spruce-compat/run-key-order.sh
#
# Env overrides:
#   GRAFT_BIN   - path to a prebuilt graft binary (default: build fresh)
#   SPRUCE_BIN  - path to a spruce binary (default: `spruce` on PATH,
#                 then a build from SPRUCE_REPO; spruce comparisons are
#                 skipped if none is found, and a graft installed under
#                 a spruce name does not count as one)
#   SPRUCE_REPO - path to a spruce source checkout to build from
#                 (default: a `spruce` checkout beside this repo)

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIR="$ROOT/tests/spruce-compat/key-order"

# shellcheck source=lib/spruce-bin.sh
source "$ROOT/tests/spruce-compat/lib/spruce-bin.sh"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

GRAFT_BIN="${GRAFT_BIN:-}"
if [[ -z "$GRAFT_BIN" ]]; then
  echo "building graft..."
  if ! (cd "$ROOT" && go build -o "$WORK/graft" ./cmd/graft); then
    echo "FATAL: go build failed" >&2
    exit 1
  fi
  GRAFT_BIN="$WORK/graft"
fi

SPRUCE_BIN="$(spruce_bin_resolve "$WORK" "$ROOT" || true)"
if [[ -z "$SPRUCE_BIN" ]]; then
  echo "note: no spruce binary found; spruce-drift comparisons skipped"
fi

pass=0
fail=0
skip=0

check() { # tool binary fixture expected
  local tool="$1" bin="$2" fixture="$3" expected="$4"
  local name got
  name="$(basename "${fixture%.yml}")"
  got="$(mktemp)"
  "$bin" merge "$fixture" >"$got" 2>/dev/null
  local rc=$?
  if [[ $rc -ne 0 ]]; then
    echo "FAIL [$tool] $name: merge exited $rc"
    fail=$((fail + 1))
  elif ! cmp -s "$got" "$expected"; then
    echo "FAIL [$tool] $name: stdout differs from $(basename "$expected")"
    diff "$expected" "$got" | sed 's/^/    /'
    fail=$((fail + 1))
  else
    echo "PASS [$tool] $name"
    pass=$((pass + 1))
  fi
  rm -f "$got"
}

for fixture in "$DIR"/*.yml; do
  base="${fixture%.yml}"
  check graft "$GRAFT_BIN" "$fixture" "$base.graft.expected"
  if [[ -n "$SPRUCE_BIN" ]]; then
    check spruce "$SPRUCE_BIN" "$fixture" "$base.spruce.expected"
  else
    skip=$((skip + 1))
  fi
done

echo
echo "key-order: $pass passed, $fail failed, $skip skipped"
[[ $fail -eq 0 ]]
