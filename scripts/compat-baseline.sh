#!/usr/bin/env bash
# compat-baseline.sh
#
# Rewrites tests/query-parity/jq/baseline.txt with the inventory's entry
# count and the sorted IDs of the entries that pass, and does so only when
# make compat-check passes, so the ratchet moves up and never records a
# gate that failed.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
BASELINE="$REPO/tests/query-parity/jq/baseline.txt"

(cd "$REPO" && make compat-check)

fresh="$(mktemp)"
trap 'rm -f "$fresh"' EXIT

(cd "$REPO" && go test ./cmd/graft/ -count=1 -v -run '^TestJQInventoryPassing$') |
  sed -n 's/^.*query_inventory_test\.go:[0-9]*: compat-entries: \(.*\)$/entries \1/p;s/^.*query_inventory_test\.go:[0-9]*: compat-passing: //p' >"$fresh"

if ! head -1 "$fresh" | grep -q '^entries [0-9]'; then
  echo "compat-baseline.sh: the inventory test produced no entry count" >&2
  exit 1
fi

cp "$fresh" "$BASELINE"
echo "compat-baseline.sh: wrote $BASELINE"
