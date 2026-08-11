#!/usr/bin/env bash
#
# check-parallel-determinism.sh
#
# Builds the graft binary and runs it repeatedly (N >= 30 iterations, per
# invocation set below) over representative multi-doc, operator-heavy
# fixtures, diffing every run's stdout against the first run's. Parallel
# evaluation is enabled by default, so any nondeterminism here would
# directly threaten genesis's byte-sensitive stderr/JSON parsing contracts.
# A single pass is not sufficient evidence of determinism - this script
# exists to run enough repetitions to have statistical confidence.
#
# Usage: scripts/check-parallel-determinism.sh [iterations]
#   iterations defaults to 40 (>= 30 required).
#
# Exit code 0: all iterations produced byte-identical output for every
#   fixture set. Non-zero: a divergence was found (details printed) or the
#   build/run itself failed.

set -euo pipefail

ITERATIONS="${1:-40}"
if [[ "$ITERATIONS" -lt 30 ]]; then
  echo "error: iterations must be >= 30 for statistical confidence (got $ITERATIONS)" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

BIN="$WORKDIR/graft"
echo "Building graft binary..."
go build -o "$BIN" ./cmd/graft

# Each entry: a name, followed by the file arguments to pass to `graft merge`.
# Fixtures are reused from the existing test asset tree (array merge
# markers, sort-on-merge, and a grab/calc dependency chain) rather than
# invented, matching existing operator/multi-doc coverage.
declare -a FIXTURE_NAMES=(
  "array_merge_markers"
  "sort_operator"
  "calc_dependency_chain"
)
declare -a FIXTURE_FILES=(
  "assets/merge/first.yml assets/merge/second.yml"
  "assets/sort/base.yml assets/sort/op.yml"
  "assets/calc/dependencies.yml"
)

overall_status=0

for idx in "${!FIXTURE_NAMES[@]}"; do
  name="${FIXTURE_NAMES[$idx]}"
  # shellcheck disable=SC2206 # intentional word-splitting of a fixed file list
  files=(${FIXTURE_FILES[$idx]})

  echo ""
  echo "=== Fixture: $name (${files[*]}) — $ITERATIONS iterations ==="

  baseline="$WORKDIR/$name.baseline"
  mismatch_found=0

  for ((i = 0; i < ITERATIONS; i++)); do
    out="$WORKDIR/$name.$i.out"
    if ! "$BIN" merge "${files[@]}" >"$out" 2>"$WORKDIR/$name.$i.err"; then
      echo "FAIL: iteration $i of fixture '$name' exited non-zero:" >&2
      cat "$WORKDIR/$name.$i.err" >&2
      overall_status=1
      mismatch_found=1
      break
    fi

    if [[ $i -eq 0 ]]; then
      cp "$out" "$baseline"
      continue
    fi

    if ! diff -u "$baseline" "$out" >"$WORKDIR/$name.$i.diff"; then
      echo "FAIL: iteration $i of fixture '$name' diverged from iteration 0:" >&2
      cat "$WORKDIR/$name.$i.diff" >&2
      overall_status=1
      mismatch_found=1
    fi
  done

  if [[ $mismatch_found -eq 0 ]]; then
    echo "OK: all $ITERATIONS iterations of '$name' produced byte-identical output."
  fi
done

echo ""
if [[ $overall_status -eq 0 ]]; then
  echo "PASS: parallel evaluation is deterministic across $ITERATIONS iterations for every fixture set."
else
  echo "FAIL: nondeterminism detected under parallel evaluation - see above." >&2
fi

exit $overall_status
