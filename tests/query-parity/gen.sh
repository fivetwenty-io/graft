#!/usr/bin/env bash
# gen.sh [--check] jq
#
# Records the goldens for every case under tests/query-parity/jq/cases by
# running the pinned reference binary in a fresh copy of each case under
# the fixed environment in env.sh, and writes want/stdout, want/exit,
# want/stderr-flag, want/stderr for cases with a stderr-exact marker, and
# want/files/ for files a run created or changed. The copy's absolute path
# is replaced with the literal $CASE_DIR in everything it records.
#
# With --check it regenerates into a temporary directory instead and fails
# when any checked-in golden differs. Only this script and run.sh need the
# reference binary. JQ_BIN names it and defaults to jq on PATH.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=env.sh
. "$HERE/env.sh"
# shellcheck source=versions.env
. "$HERE/versions.env"

check=0
if [ "${1:-}" = "--check" ]; then
  check=1
  shift
fi
dialect="${1:-}"
if [ "$dialect" != "jq" ]; then
  echo "usage: gen.sh [--check] jq" >&2
  exit 2
fi

JQ_BIN="${JQ_BIN:-jq}"
got="$("$JQ_BIN" --version 2>&1 | head -1)" || true
if [ "$got" != "$JQ_BUILD" ]; then
  echo "gen.sh: $JQ_BIN reports '$got' but versions.env pins '$JQ_BUILD'" >&2
  exit 2
fi

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# scrub rewrites the copy's path to $CASE_DIR in a file.
scrub() { # scrub COPY FILE
  CASE_TMP="$1" perl -0pi -e 's/\Q$ENV{CASE_TMP}\E/\$CASE_DIR/g' "$2"
}

record() { # record CASE_DIR OUT_DIR
  local case_dir="$1" out="$2" tmp stdin_file rc rel
  tmp="$(parity_copy "$case_dir")"
  stdin_file=/dev/null
  [ -f "$case_dir/stdin" ] && stdin_file="$case_dir/stdin"
  parity_args "$case_dir"

  mkdir -p "$out"
  set +e
  parity_run "$tmp" "$stdin_file" "$JQ_BIN" "${PARITY_ARGV[@]}" >"$out/stdout" 2>"$out/stderr.raw"
  rc=$?
  set -e
  echo "$rc" >"$out/exit"
  if [ -s "$out/stderr.raw" ]; then echo 1 >"$out/stderr-flag"; else echo 0 >"$out/stderr-flag"; fi
  scrub "$tmp" "$out/stdout"
  scrub "$tmp" "$out/stderr.raw"
  if [ -f "$case_dir/stderr-exact" ]; then mv "$out/stderr.raw" "$out/stderr"; else rm "$out/stderr.raw"; fi

  # Files the run created or changed.
  (cd "$tmp" && find . -type f | sed 's|^\./||' | sort) | while IFS= read -r rel; do
    parity_is_control "${rel%%/*}" && continue
    if [ ! -f "$case_dir/$rel" ] || ! cmp -s "$case_dir/$rel" "$tmp/$rel"; then
      mkdir -p "$out/files/$(dirname "$rel")"
      cp -p "$tmp/$rel" "$out/files/$rel"
      scrub "$tmp" "$out/files/$rel"
    fi
  done
  rm -rf "$tmp"
}

status=0
for case_dir in "$HERE/$dialect/cases"/*/; do
  case_dir="${case_dir%/}"
  name="$(basename "$case_dir")"
  if [ "$check" = 1 ]; then
    record "$case_dir" "$scratch/$name"
    if ! diff -r "$scratch/$name" "$case_dir/want" >"$scratch/$name.diff" 2>&1; then
      echo "gen.sh: golden for $name is stale" >&2
      head -20 "$scratch/$name.diff" >&2
      status=1
    fi
  else
    rm -rf "$case_dir/want"
    record "$case_dir" "$case_dir/want"
  fi
done

if [ "$check" = 1 ]; then
  [ "$status" = 0 ] && echo "gen.sh: every $dialect golden matches $JQ_BUILD"
  exit "$status"
fi
echo "gen.sh: recorded $(ls "$HERE/$dialect/cases" | wc -l | tr -d ' ') $dialect cases with $JQ_BUILD"
