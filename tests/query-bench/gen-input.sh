#!/usr/bin/env bash
# gen-input.sh OUT [BYTES] writes the benchmark document to OUT. It is the
# same array of small objects that internal/query/bench_test.go builds, so
# graft and jq read identical bytes. BYTES defaults to 50 MiB.
set -euo pipefail

out="${1:?usage: gen-input.sh OUT [BYTES]}"
size="${2:-$((50 * 1024 * 1024))}"

awk -v size="$size" 'BEGIN {
  printf "["
  len = 1
  for (i = 0; len < size; i++) {
    row = sprintf("{\"n\":%d,\"k\":\"k%d\",\"s\":\"item number %d\",\"f\":%d.5}", i, i % 100, i, i % 1000)
    if (i > 0) row = "," row
    printf "%s", row
    len += length(row)
  }
  printf "]"
}' >"$out"
