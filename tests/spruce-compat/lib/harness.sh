#!/usr/bin/env bash
# Shared helpers for the spruce/graft golden-output parity harness.
# Sourced by run.sh; not meant to be executed directly.
#
# Comparison bar: exit codes must match exactly on every
# pattern. Stdout is compared semantically (YAML/JSON re-parsed and
# structurally diffed); a byte-identical match is reported as a stronger
# note but a semantic-only match still PASSes, since the parity bar explicitly
# allows cosmetic differences (YAML marshal internals, ANSI) that genesis
# never scrapes. Stderr is compared only on the surfaces genesis actually
# parses: the ` - $.path: message` adaptive-merge line format and the
# `secret X not found` substring.

LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CANON="$LIB_DIR/canon.py"

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0
FAILED_PATTERNS=()

# genesisAdaptiveMergeErrorRx mirrors genesis's _adaptive_merge regex
# (lib/Genesis/Env/ManifestProvider.pm) exactly: `/^ - \$\.([^:]*): (.*)$/`
ADAPTIVE_ERR_RX='^ - \$\.[^:]*: .*$'

report_pass() {
  local name="$1" detail="${2:-}"
  PASS_COUNT=$((PASS_COUNT + 1))
  printf 'PASS  %s\n' "$name"
  [ -n "$detail" ] && printf '      %s\n' "$detail"
}

report_fail() {
  local name="$1" detail="${2:-}"
  FAIL_COUNT=$((FAIL_COUNT + 1))
  FAILED_PATTERNS+=("$name")
  printf 'FAIL  %s\n' "$name"
  if [ -n "$detail" ]; then
    while IFS= read -r line; do
      printf '      %s\n' "$line"
    done <<<"$detail"
  fi
}

report_skip() {
  local name="$1" detail="${2:-}"
  SKIP_COUNT=$((SKIP_COUNT + 1))
  printf 'SKIP  %s\n' "$name"
  [ -n "$detail" ] && printf '      %s\n' "$detail"
}

# run_tool <bin> <outfile> <errfile> <stdin-file-or-NONE> -- <args...>
# Always gives the process a real, closed stdin (either a file or
# /dev/null) so a piped-in `-` sentinel test never blocks on an
# unrelated inherited terminal/session stdin.
run_tool() {
  local bin="$1" outf="$2" errf="$3" stdin_src="$4"
  shift 4
  [ "$1" = "--" ] && shift
  if [ "$stdin_src" != "NONE" ]; then
    "$bin" "$@" <"$stdin_src" >"$outf" 2>"$errf"
  else
    "$bin" "$@" >"$outf" 2>"$errf" </dev/null
  fi
  # shellcheck disable=SC2034  # RC is a deliberate out-param read by callers
  RC=$?
}

# cmp_bytes <fileA> <fileB> -- returns 0 if byte-identical
cmp_bytes() {
  cmp -s "$1" "$2"
}

# cmp_semantic <mode: yaml|json|jsonlines> <fileA> <fileB>
# Sets SEMANTIC_DETAIL on mismatch (canonicalization failure or
# structural diff). Returns 0 on semantic equality, 1 otherwise.
cmp_semantic() {
  local mode="$1" a="$2" b="$3"
  local ca cb caerr cberr
  ca="$(mktemp)"
  cb="$(mktemp)"
  caerr="$(mktemp)"
  cberr="$(mktemp)"
  python3 "$CANON" "$mode" "$a" >"$ca" 2>"$caerr"
  local arc=$?
  python3 "$CANON" "$mode" "$b" >"$cb" 2>"$cberr"
  local brc=$?
  if [ $arc -ne 0 ] || [ $brc -ne 0 ]; then
    local aerr berr
    aerr="$(cat "$caerr")"
    berr="$(cat "$cberr")"
    SEMANTIC_DETAIL="canonicalization failed (mode=$mode): a-rc=$arc b-rc=$brc$([ -n "$aerr" ] && echo " a-err=$aerr")$([ -n "$berr" ] && echo " b-err=$berr")"
    rm -f "$ca" "$cb" "$caerr" "$cberr"
    return 1
  fi
  if diff -q "$ca" "$cb" >/dev/null 2>&1; then
    rm -f "$ca" "$cb" "$caerr" "$cberr"
    return 0
  fi
  SEMANTIC_DETAIL="$(diff -u "$ca" "$cb" | head -20)"
  rm -f "$ca" "$cb" "$caerr" "$cberr"
  return 1
}

# assert_stdout_parity <name> <mode> <spruce-out> <graft-out>
# mode: yaml | json | jsonlines | raw (raw = byte-compare only, used
# when semantic re-parsing does not apply, e.g. plain diff text).
# Echoes a human-readable verdict line via DETAIL_OUT and returns 0/1.
assert_stdout_parity() {
  local mode="$1" a="$2" b="$3"
  if cmp_bytes "$a" "$b"; then
    DETAIL_OUT="stdout byte-identical"
    return 0
  fi
  if [ "$mode" = "raw" ]; then
    DETAIL_OUT="stdout differs (byte comparison required for this pattern):
$(diff -u "$a" "$b" | head -20)"
    return 1
  fi
  if cmp_semantic "$mode" "$a" "$b"; then
    DETAIL_OUT="stdout semantically equal (byte diff present, allowed as a cosmetic exception)"
    return 0
  fi
  DETAIL_OUT="stdout differs semantically (mode=$mode):
$SEMANTIC_DETAIL"
  return 1
}

# extract_adaptive_lines <file>
extract_adaptive_lines() {
  grep -E "$ADAPTIVE_ERR_RX" "$1" 2>/dev/null || true
}

# assert_adaptive_stderr_parity <spruce-err> <graft-err>
# Compares the genesis-scraped subset of stderr: adaptive-merge-format
# lines must match exactly (same paths/messages, same order), since
# that is the literal contract genesis's _adaptive_merge regex depends
# on. If neither side produced any such lines, this is a no-op pass
# (nothing on that surface for genesis to scrape).
assert_adaptive_stderr_parity() {
  local a="$1" b="$2"
  local la lb
  la="$(extract_adaptive_lines "$a")"
  lb="$(extract_adaptive_lines "$b")"
  if [ "$la" = "$lb" ]; then
    DETAIL_OUT="adaptive-merge stderr lines match ($(printf '%s' "$la" | grep -c . || true) line(s))"
    return 0
  fi
  # shellcheck disable=SC2034  # DETAIL_OUT is a deliberate out-param read by callers
  DETAIL_OUT="adaptive-merge stderr lines differ:
--- spruce ---
$la
--- graft ---
$lb"
  return 1
}

# assert_exit_parity <spruce-rc> <graft-rc>
assert_exit_parity() {
  [ "$1" = "$2" ]
}
