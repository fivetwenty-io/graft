#!/usr/bin/env bash
# compat-report.sh [--check]
#
# Rewrites tests/query-parity/jq/report.txt and
# tests/query-parity/yq/report.txt from each inventory, the checked-in
# goldens, and graft itself. Each area gets a line of
# "area total allowlisted passing percent", then the overall line, then
# a count of the out-of-scope entries when there are any, then the
# allowlist with one line per entry and its reason. With --check it
# writes fresh reports to temporary files instead and fails when a
# checked-in report differs. Neither mode needs jq or yq.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"

fresh="$(mktemp)"
trap 'rm -f "$fresh"' EXIT

status=0
for pair in jq:TestJQInventoryReport yq:TestYQInventoryReport; do
  dialect="${pair%%:*}"
  test="${pair#*:}"
  report="$REPO/tests/query-parity/$dialect/report.txt"
  {
    echo "# area total allowlisted passing percent"
    (cd "$REPO" && go test ./cmd/graft/ -count=1 -v -run "^$test\$") |
      sed -n 's/^.*query_inventory_test\.go:[0-9]*: compat-report: //p'
  } >"$fresh"

  if [ "$(wc -l <"$fresh")" -lt 3 ]; then
    echo "compat-report.sh: the $dialect inventory test produced no report" >&2
    exit 1
  fi

  if [ "${1:-}" = "--check" ]; then
    if ! diff -u "$report" "$fresh"; then
      echo "compat-report.sh: $dialect/report.txt is stale; run make compat-report" >&2
      status=1
    else
      echo "compat-report.sh: $dialect/report.txt is current"
    fi
    continue
  fi

  cp "$fresh" "$report"
  echo "compat-report.sh: wrote $report"
done
exit "$status"
