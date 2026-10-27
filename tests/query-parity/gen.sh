#!/usr/bin/env bash
# gen.sh [--check] jq|yq
#
# Records the goldens for every case under tests/query-parity/DIALECT/cases by
# running the pinned reference binary in a fresh copy of each case under
# the fixed environment in env.sh, and writes want/stdout, want/exit,
# want/stderr-flag, want/stderr for cases with a stderr-exact marker, and
# want/files/ for files a run created or changed. The copy's absolute path
# is replaced with the literal $CASE_DIR in everything it records.
#
# With --check it regenerates into a temporary directory instead and fails
# when any checked-in golden differs. Only this script and run.sh need the
# reference binary. JQ_BIN and YQ_BIN name them and default to jq and yq on
# PATH.
#
# The cases are independent, so they run in parallel, GEN_JOBS at a time
# (the CPU count by default). Each worker works in its own scratch and
# output directories, so the recorded files are the same whatever the order.
# A stale golden is reported after the workers finish, in case-name order.
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
case "$dialect" in
  jq)
    bin_name="${JQ_BIN:-jq}"
    pinned="$JQ_BUILD"
    ;;
  yq)
    bin_name="${YQ_BIN:-yq}"
    pinned="$YQ_BUILD"
    ;;
  *)
    echo "usage: gen.sh [--check] jq|yq" >&2
    exit 2
    ;;
esac

# parity_run resets PATH to /usr/bin:/bin, so a bare name would run
# whatever lives there rather than the binary checked below.
if ! REF_BIN="$(command -v "$bin_name")"; then
  echo "gen.sh: reference binary $bin_name is missing" >&2
  exit 2
fi
got="$("$REF_BIN" --version 2>&1 | head -1)" || true
if [ "$got" != "$pinned" ]; then
  echo "gen.sh: $REF_BIN reports '$got' but versions.env pins '$pinned'" >&2
  exit 2
fi

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# record reruns one case under the reference binary and writes what it did
# into OUT_DIR. It scrubs the copy's path to $CASE_DIR in every file it
# records with one perl run, so a case costs a single scrub process.
record() { # record CASE_DIR OUT_DIR
  local case_dir="$1" out="$2" tmp stdin_file rc rel
  local -a scrub_files
  tmp="$(parity_copy "$case_dir")"
  stdin_file=/dev/null
  [ -f "$case_dir/stdin" ] && stdin_file="$case_dir/stdin"
  parity_args "$case_dir"

  mkdir -p "$out"
  set +e
  parity_run "$tmp" "$stdin_file" "$REF_BIN" "${PARITY_ARGV[@]}" >"$out/stdout" 2>"$out/stderr.raw"
  rc=$?
  set -e
  echo "$rc" >"$out/exit"
  if [ -s "$out/stderr.raw" ]; then echo 1 >"$out/stderr-flag"; else echo 0 >"$out/stderr-flag"; fi
  scrub_files=("$out/stdout" "$out/stderr.raw")

  # Files the run created or changed.
  while IFS= read -r -d '' rel; do
    rel="${rel#./}"
    parity_is_control "${rel%%/*}" && continue
    if [ ! -f "$case_dir/$rel" ] || ! cmp -s "$case_dir/$rel" "$tmp/$rel"; then
      case "$rel" in
        */*) mkdir -p "$out/files/${rel%/*}" ;;
        *) mkdir -p "$out/files" ;;
      esac
      cp -p "$tmp/$rel" "$out/files/$rel"
      scrub_files+=("$out/files/$rel")
    fi
  done < <(cd "$tmp" && find . -type f -print0)

  # scrub rewrites the copy's path to $CASE_DIR.
  CASE_TMP="$tmp" perl -0pi -e 's/\Q$ENV{CASE_TMP}\E/\$CASE_DIR/g' "${scrub_files[@]}"
  if [ -f "$case_dir/stderr-exact" ]; then mv "$out/stderr.raw" "$out/stderr"; else rm "$out/stderr.raw"; fi
  rm -rf "$tmp"
}

# do_case handles one case directory. In check mode a stale golden leaves its
# report in $scratch/NAME.report and an empty $scratch/NAME.stale marker, and
# the worker still succeeds, so the parent can print every report in order.
do_case() { # do_case CASE_DIR
  local case_dir="${1%/}" name
  name="$(basename "$case_dir")"
  if [ "$check" = 1 ]; then
    record "$case_dir" "$scratch/$name"
    if ! diff -r "$scratch/$name" "$case_dir/want" >"$scratch/$name.diff" 2>&1; then
      {
        echo "gen.sh: golden for $name is stale"
        head -20 "$scratch/$name.diff"
      } >"$scratch/$name.report" 2>&1
    fi
    rm -rf "$scratch/$name" "$scratch/$name.diff"
  else
    rm -rf "$case_dir/want"
    record "$case_dir" "$case_dir/want"
  fi
}

jobs="${GEN_JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}"
export HERE check scratch dialect REF_BIN
export -f parity_run parity_args parity_copy parity_is_control record do_case

status=0
if ! printf '%s\0' "$HERE/$dialect/cases"/*/ | xargs -0 -n1 -P "$jobs" bash -c 'set -euo pipefail; do_case "$1"' _; then
  echo "gen.sh: a worker failed while running the $dialect cases" >&2
  status=1
fi

if [ "$check" = 1 ]; then
  for report in "$scratch"/*.report; do
    [ -e "$report" ] || continue
    cat "$report" >&2
    status=1
  done
  [ "$status" = 0 ] && echo "gen.sh: every $dialect golden matches $pinned"
  exit "$status"
fi
[ "$status" = 0 ] || exit "$status"
echo "gen.sh: recorded $(ls "$HERE/$dialect/cases" | wc -l | tr -d ' ') $dialect cases with $pinned"
