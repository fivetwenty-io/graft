#!/usr/bin/env bash
# run.sh [--as-alias] [jq]
#
# A by-hand audit that runs the reference binary and graft side by side
# over the same cases, each in its own fresh copy under the fixed
# environment in env.sh, and compares stdout, the exit code, stderr for the
# cases that compare it, and the files left behind. JQ_BIN and GRAFT_BIN
# name the binaries. With --as-alias graft runs through a link named jq
# instead of as `graft jq`. A missing reference binary skips the audit
# unless QUERY_COMPAT_STRICT=1, which makes it a failure.
#
# A case that carries a phase file or an allowlist entry is known to differ,
# so its difference is reported and does not fail the audit.
#
# This script is reached only through `make query-oracle-audit`, and never
# through test-unit, CI, or a release.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
# shellcheck source=env.sh
. "$HERE/env.sh"

as_alias=0
if [ "${1:-}" = "--as-alias" ]; then
  as_alias=1
  shift
fi
dialect="${1:-jq}"
if [ "$dialect" != "jq" ]; then
  echo "usage: run.sh [--as-alias] [jq]" >&2
  exit 2
fi

JQ_BIN="${JQ_BIN:-jq}"
if ! command -v "$JQ_BIN" >/dev/null 2>&1; then
  if [ "${QUERY_COMPAT_STRICT:-0}" = 1 ]; then
    echo "run.sh: reference binary $JQ_BIN is missing and QUERY_COMPAT_STRICT=1" >&2
    exit 1
  fi
  echo "run.sh: reference binary $JQ_BIN is missing, skipping the audit"
  exit 0
fi
JQ_BIN="$(command -v "$JQ_BIN")"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

GRAFT_BIN="${GRAFT_BIN:-}"
if [ -z "$GRAFT_BIN" ]; then
  GRAFT_BIN="$scratch/graft"
  (cd "$REPO" && go build -o "$GRAFT_BIN" ./cmd/graft)
fi
GRAFT_BIN="$(cd "$(dirname "$GRAFT_BIN")" && pwd)/$(basename "$GRAFT_BIN")"

if [ "$as_alias" = 1 ]; then
  mkdir -p "$scratch/links"
  ln -s "$GRAFT_BIN" "$scratch/links/jq"
fi

scrub() { # scrub COPY FILE
  CASE_TMP="$1" perl -0pi -e 's/\Q$ENV{CASE_TMP}\E/\$CASE_DIR/g' "$2"
}

# observe runs one binary over a case and leaves its stdout, exit, stderr,
# and copy in OUT.
observe() { # observe CASE_DIR OUT BIN ARG...
  local case_dir="$1" out="$2" bin="$3" tmp stdin_file rc
  shift 3
  tmp="$(parity_copy "$case_dir")"
  stdin_file=/dev/null
  [ -f "$case_dir/stdin" ] && stdin_file="$case_dir/stdin"
  mkdir -p "$out"
  set +e
  parity_run "$tmp" "$stdin_file" "$bin" "$@" >"$out/stdout" 2>"$out/stderr"
  rc=$?
  set -e
  echo "$rc" >"$out/exit"
  scrub "$tmp" "$out/stdout"
  scrub "$tmp" "$out/stderr"
  if [ ! -f "$case_dir/stderr-exact" ]; then
    if [ -s "$out/stderr" ]; then echo 1 >"$out/stderr"; else echo 0 >"$out/stderr"; fi
  fi
  rm -rf "$tmp/args" "$tmp/stdin" "$tmp/graft_args" "$tmp/graft-stdout" "$tmp/phase" "$tmp/stderr-exact"
  mkdir -p "$out/files"
  (cd "$tmp" && tar -cf - .) | (cd "$out/files" && tar -xf -)
  CASE_TMP="$tmp" find "$out/files" -type f -exec perl -0pi -e 's/\Q$ENV{CASE_TMP}\E/\$CASE_DIR/g' {} +
  rm -rf "$tmp"
}

total=0
failed=0
known=0
for case_dir in "$HERE/$dialect/cases"/*/; do
  case_dir="${case_dir%/}"
  name="$(basename "$case_dir")"
  total=$((total + 1))
  parity_args "$case_dir"
  observe "$case_dir" "$scratch/want-$name" "$JQ_BIN" "${PARITY_ARGV[@]}"
  if [ "$as_alias" = 1 ]; then
    observe "$case_dir" "$scratch/got-$name" "$scratch/links/jq" "${PARITY_ARGV[@]}" ${PARITY_GRAFT_ARGV[@]+"${PARITY_GRAFT_ARGV[@]}"}
  else
    observe "$case_dir" "$scratch/got-$name" "$GRAFT_BIN" jq "${PARITY_ARGV[@]}" ${PARITY_GRAFT_ARGV[@]+"${PARITY_GRAFT_ARGV[@]}"}
  fi
  if diff -r "$scratch/want-$name" "$scratch/got-$name" >"$scratch/$name.diff" 2>&1; then
    continue
  fi
  if [ -f "$case_dir/phase" ] || { cut -f1 "$HERE/$dialect/allowlist.txt" 2>/dev/null | grep -qxF "$name"; }; then
    known=$((known + 1))
    echo "known difference: $name"
    continue
  fi
  failed=$((failed + 1))
  echo "DIFFERS: $name"
  head -20 "$scratch/$name.diff"
done

echo "run.sh: $total cases, $failed differ, $known known differences"
[ "$failed" = 0 ]
