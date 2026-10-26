#!/usr/bin/env bash
# compat-baseline.sh
#
# Rewrites tests/query-parity/jq/baseline.txt and
# tests/query-parity/yq/baseline.txt with each inventory's entry count
# and the sorted IDs of the entries that pass, and does so only when
# make compat-check passes, so the ratchet moves up and never records a
# gate that failed.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"

(cd "$REPO" && make compat-check)

fresh="$(mktemp)"
trap 'rm -f "$fresh"' EXIT

for pair in jq:TestJQInventoryPassing yq:TestYQInventoryPassing; do
  dialect="${pair%%:*}"
  test="${pair#*:}"
  baseline="$REPO/tests/query-parity/$dialect/baseline.txt"
  (cd "$REPO" && go test ./cmd/graft/ -count=1 -v -run "^$test\$") |
    sed -n 's/^.*query_inventory_test\.go:[0-9]*: compat-entries: \(.*\)$/entries \1/p;s/^.*query_inventory_test\.go:[0-9]*: compat-passing: //p' >"$fresh"

  if ! head -1 "$fresh" | grep -q '^entries [0-9]'; then
    echo "compat-baseline.sh: the $dialect inventory test produced no entry count" >&2
    exit 1
  fi

  cp "$fresh" "$baseline"
  echo "compat-baseline.sh: wrote $baseline"
done
