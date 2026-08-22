#!/usr/bin/env bash
# Operator parity suite: runs fixtures/<operator>/<case>/*.yml through both
# graft and spruce, comparing stdout, stderr, and exit code. See README.md
# in this directory for the fixture-case file contract.
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURES_DIR="$SCRIPT_DIR/fixtures"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

SPRUCE_BIN="${SPRUCE_BIN:-$(command -v spruce || true)}"

if [ -z "$SPRUCE_BIN" ] || [ ! -x "$SPRUCE_BIN" ]; then
  echo "SKIP: spruce binary not found on PATH (set SPRUCE_BIN to override)."
  echo "SKIP: operator parity suite requires a spruce binary to compare against; skipping cleanly."
  exit 0
fi

GRAFT_BIN="${GRAFT_BIN:-}"
CLEANUP_GRAFT_BIN=""
if [ -z "$GRAFT_BIN" ]; then
  BUILD_DIR="$(mktemp -d)"
  GRAFT_BIN="$BUILD_DIR/graft"
  CLEANUP_GRAFT_BIN="$BUILD_DIR"
  if ! (cd "$REPO_ROOT" && go build -o "$GRAFT_BIN" ./cmd/graft) >"$BUILD_DIR/build.log" 2>&1; then
    echo "FAIL: could not build graft binary from $REPO_ROOT/cmd/graft"
    cat "$BUILD_DIR/build.log"
    exit 1
  fi
fi

cleanup() {
  [ -n "$CLEANUP_GRAFT_BIN" ] && rm -rf "$CLEANUP_GRAFT_BIN"
}
trap cleanup EXIT

PASS=0
FAIL=0
SKIP=0
INFO=0

# read_lines FILE -> prints one non-empty, non-comment line per line
read_lines() {
  local file="$1"
  [ -f "$file" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      ''|'#'*) continue ;;
    esac
    printf '%s\n' "$line"
  done < "$file"
}

# strip_leading_marker FILE -> removes the first line when it is exactly
# "---". graft deliberately starts merge output with a document marker
# (see the merge document marker changelog entry); spruce does not.
# Applied to both tools' stdout so the comparison is symmetric, and only
# to the first line so interior multi-doc separators still count.
strip_leading_marker() {
  local file="$1"
  if [ "$(head -n1 "$file" 2>/dev/null)" = "---" ]; then
    tail -n +2 "$file" > "$file.stripped" && mv "$file.stripped" "$file"
  fi
}

run_case() {
  local op="$1" case_dir="$2" case_name="$3"

  local verb="merge"
  [ -f "$case_dir/verb" ] && verb="$(cat "$case_dir/verb")"

  local -a files=()
  while IFS= read -r f; do
    files+=("$f")
  done < <(find "$case_dir" -maxdepth 1 -name '*.yml' | sort)

  if [ "${#files[@]}" -eq 0 ]; then
    echo "SKIP  $op/$case_name (no .yml fixture files in case dir)"
    SKIP=$((SKIP + 1))
    return
  fi

  local -a flags=()
  while IFS= read -r line; do flags+=("$line"); done < <(read_lines "$case_dir/flags")

  # @FIXTURES_DIR@ in env values expands to the absolute fixtures
  # directory, so cases can point at _support data without baking in a
  # machine-specific checkout path.
  local -a envpairs=()
  while IFS= read -r line; do envpairs+=("${line//@FIXTURES_DIR@/$FIXTURES_DIR}"); done < <(read_lines "$case_dir/env")

  local mode="byte"
  [ -f "$case_dir/mode" ] && mode="$(cat "$case_dir/mode")"

  local divergence_note=""
  [ -f "$case_dir/expect-divergence" ] && divergence_note="$(cat "$case_dir/expect-divergence")"

  local g_out g_err s_out s_err
  g_out="$(mktemp)"; g_err="$(mktemp)"
  s_out="$(mktemp)"; s_err="$(mktemp)"

  ( env "${envpairs[@]}" "$GRAFT_BIN" "$verb" "${flags[@]}" "${files[@]}" >"$g_out" 2>"$g_err" )
  local g_exit=$?
  ( env "${envpairs[@]}" "$SPRUCE_BIN" "$verb" "${flags[@]}" "${files[@]}" >"$s_out" 2>"$s_err" )
  local s_exit=$?

  strip_leading_marker "$g_out"
  strip_leading_marker "$s_out"

  local matched=1

  if [ "$mode" = "structural" ]; then
    # Structural compare: order is non-deterministic (e.g. shuffle), so
    # compare the sorted set of elements under the top-level `result` key
    # instead of byte-exact stdout. Each tool parses its own YAML output
    # via its own `json` verb to avoid cross-tool YAML-dialect noise.
    local g_json s_json g_sorted s_sorted
    g_json="$("$GRAFT_BIN" json "$g_out" 2>/dev/null)"
    s_json="$("$SPRUCE_BIN" json "$s_out" 2>/dev/null)"
    g_sorted="$(printf '%s' "$g_json" | jq -cS '.result | sort' 2>/dev/null)"
    s_sorted="$(printf '%s' "$s_json" | jq -cS '.result | sort' 2>/dev/null)"
    if [ "$g_sorted" != "$s_sorted" ] || [ "$g_exit" != "$s_exit" ]; then
      matched=0
    fi
  else
    if ! diff -q "$g_out" "$s_out" >/dev/null 2>&1; then matched=0; fi
    if ! diff -q "$g_err" "$s_err" >/dev/null 2>&1; then matched=0; fi
    if [ "$g_exit" != "$s_exit" ]; then matched=0; fi
  fi

  if [ "$matched" -eq 1 ]; then
    if [ -n "$divergence_note" ]; then
      echo "INFO  $op/$case_name matched spruce; a documented divergence note is present and may be stale: $divergence_note"
      INFO=$((INFO + 1))
    else
      echo "PASS  $op/$case_name"
      PASS=$((PASS + 1))
    fi
    rm -f "$g_out" "$g_err" "$s_out" "$s_err"
    return
  fi

  if [ -n "$divergence_note" ]; then
    echo "INFO  $op/$case_name diverges from spruce (documented, not a regression): $divergence_note"
    INFO=$((INFO + 1))
    rm -f "$g_out" "$g_err" "$s_out" "$s_err"
    return
  fi

  echo "FAIL  $op/$case_name"
  echo "      verb=$verb flags=[${flags[*]:-}] files=[${files[*]}]"
  echo "      graft exit=$g_exit  spruce exit=$s_exit"
  if [ "$mode" != "structural" ]; then
    echo "      --- stdout diff (graft vs spruce) ---"
    diff "$g_out" "$s_out" | sed 's/^/      /' || true
    echo "      --- stderr diff (graft vs spruce) ---"
    diff "$g_err" "$s_err" | sed 's/^/      /' || true
  fi
  FAIL=$((FAIL + 1))
  rm -f "$g_out" "$g_err" "$s_out" "$s_err"
}

for op_dir in "$FIXTURES_DIR"/*/; do
  op="$(basename "$op_dir")"

  # Operator-level marker: no spruce equivalent exists at all (e.g. nats).
  marker_file=""
  for f in "$op_dir"*; do
    [ -f "$f" ] && case "$(basename "$f")" in
      no-spruce-equivalent) marker_file="$f" ;;
    esac
  done
  if [ -n "$marker_file" ]; then
    echo "INFO  $op: $(cat "$marker_file")"
    INFO=$((INFO + 1))
    continue
  fi

  found_case=0
  for case_dir in "$op_dir"*/; do
    [ -d "$case_dir" ] || continue
    case_name="$(basename "$case_dir")"
    # Directories prefixed with _ hold support data (e.g. a file the `file`/
    # `load` operator fixtures read), not a runnable case.
    case "$case_name" in
      _*) continue ;;
    esac
    found_case=1
    run_case "$op" "$case_dir" "$case_name"
  done

  if [ "$found_case" -eq 0 ]; then
    echo "SKIP  $op (no case directories defined)"
    SKIP=$((SKIP + 1))
  fi
done

echo ""
echo "Operator parity suite: $PASS passed, $FAIL failed, $SKIP skipped, $INFO documented-divergence."

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
