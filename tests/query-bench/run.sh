#!/usr/bin/env bash
# run.sh times graft jq against the real jq over the benchmark programs and
# prints a table of wall times. It is opt-in and never gates a build. When
# JQ_BIN is unset and jq is not on PATH, the jq column is skipped, so
# nothing in normal CI needs jq. A program that takes more than twice jq's
# time is marked FINDING.
#
# GRAFT_BIN names a built graft, and the script builds one when it is unset.
# BENCH_BYTES sizes the large input and defaults to 50 MiB. BENCH_RUNS is
# how many runs each program gets, and the best one counts.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

runs="${BENCH_RUNS:-3}"
jq_bin="${JQ_BIN:-$(command -v jq || true)}"
graft_bin="${GRAFT_BIN:-}"
if [ -z "$graft_bin" ]; then
  graft_bin="$work/graft"
  (cd "$root" && go build -o "$graft_bin" ./cmd/graft)
fi

"$here/gen-input.sh" "$work/large.json" "${BENCH_BYTES:-$((50 * 1024 * 1024))}"
long_string="$(printf 'a%.0s' $(seq 1 5000))"
printf '"%s"\n' "$long_string" >"$work/long.json"
echo null >"$work/null.json"

now_ms() { perl -MTime::HiRes=time -e 'printf "%d\n", time() * 1000'; }

# best_ms INPUT BIN ARGS... prints the best wall time in milliseconds. A
# run that fails prints "err" so a timeout or crash never reads as fast.
best_ms() {
  local input="$1" best="" t0 t1 ms i
  shift
  for ((i = 0; i < runs; i++)); do
    t0="$(now_ms)"
    if ! "$@" "$input" >/dev/null 2>&1; then
      echo err
      return
    fi
    t1="$(now_ms)"
    ms=$((t1 - t0))
    if [ -z "$best" ] || [ "$ms" -lt "$best" ]; then best="$ms"; fi
  done
  echo "$best"
}

printf '%-22s %10s %10s  %s\n' program graft_ms jq_ms note
row() {
  local name="$1" input="$2" prog="$3" g j note=""
  g="$(best_ms "$input" "$graft_bin" jq -c "$prog")"
  if [ -n "$jq_bin" ]; then
    j="$(best_ms "$input" "$jq_bin" -c "$prog")"
    if [[ "$g" =~ ^[0-9]+$ && "$j" =~ ^[0-9]+$ ]] && [ "$g" -gt $((2 * j + 1)) ]; then note="FINDING over 2x jq"; fi
    if [ "$g" = err ]; then note="FINDING graft failed"; fi
  else
    j="-"
  fi
  printf '%-22s %10s %10s  %s\n' "$name" "$g" "$j" "$note"
}

row identity "$work/large.json" '.'
row select-group-by "$work/large.json" '[.[] | select(.n % 2 == 0)] | group_by(.k) | length'
row reduce-sum "$work/large.json" 'reduce .[] as $x (0; . + $x.n)'
row tostream "$work/large.json" '[tostream] | length'
row update-every-element "$work/large.json" '.[].n |= . + 1 | length'
row setpath-chain "$work/null.json" 'reduce range(10000) as $i ({}; setpath(["k\($i)"]; $i)) | length'
row arithmetic "$work/null.json" 'reduce range(1000000) as $i (0; . + $i / 7)'
row exact-compare "$work/null.json" '[range(200000) | . * 1.5] | sort | length'
row regex-greedy-loop "$work/long.json" 'test(".*x")'

if [ -z "$jq_bin" ]; then
  echo "jq not found, so the jq column is skipped (set JQ_BIN to compare)" >&2
fi
