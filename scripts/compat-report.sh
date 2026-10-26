#!/usr/bin/env bash
# compat-report.sh [--check]
#
# Rewrites tests/query-parity/jq/report.txt from the inventory, the
# checked-in goldens, and graft itself. Each area gets a line of
# "area total allowlisted passing percent", then the overall line, then
# the allowlist with one line per entry and its reason. With --check it
# writes a fresh report to a temporary file instead and fails when the
# checked-in report differs. Neither mode needs jq.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
REPORT="$REPO/tests/query-parity/jq/report.txt"

fresh="$(mktemp)"
trap 'rm -f "$fresh"' EXIT

{
  echo "# area total allowlisted passing percent"
  (cd "$REPO" && go test ./cmd/graft/ -count=1 -v -run '^TestJQInventoryReport$') |
    sed -n 's/^.*query_inventory_test\.go:[0-9]*: compat-report: //p'
} >"$fresh"

if [ "$(wc -l <"$fresh")" -lt 3 ]; then
  echo "compat-report.sh: the inventory test produced no report" >&2
  exit 1
fi

if [ "${1:-}" = "--check" ]; then
  if ! diff -u "$REPORT" "$fresh"; then
    echo "compat-report.sh: report.txt is stale; run make compat-report" >&2
    exit 1
  fi
  echo "compat-report.sh: report.txt is current"
  exit 0
fi

cp "$fresh" "$REPORT"
echo "compat-report.sh: wrote $REPORT"
