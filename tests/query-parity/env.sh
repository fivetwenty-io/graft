#!/usr/bin/env bash
# env.sh is the fixed environment every parity run uses, so a golden never
# records this machine. gen.sh and run.sh source it, and the in-process
# replay in cmd/graft/query_parity_test.go applies the same variables.
#
# parity_run DIR STDIN_FILE BIN ARG... runs BIN in DIR with HOME and PWD
# set to DIR, TZ=UTC, LANG=C.UTF-8, PATH=/usr/bin:/bin, and nothing else
# inherited. STDIN_FILE may be /dev/null. It prints nothing itself, and
# leaves stdout, stderr, and the exit code to the caller.

parity_run() {
  local dir="$1" stdin_file="$2"
  shift 2
  (cd "$dir" && exec env -i HOME="$dir" TZ=UTC LANG=C.UTF-8 PWD="$dir" PATH=/usr/bin:/bin "$@" <"$stdin_file")
}

# parity_args CASE_DIR fills the array PARITY_ARGV with the case's args
# file, one argument per line, and PARITY_GRAFT_ARGV with its graft_args
# file when there is one.
parity_args() {
  local case_dir="$1" line
  PARITY_ARGV=()
  PARITY_GRAFT_ARGV=()
  while IFS= read -r line || [ -n "$line" ]; do
    PARITY_ARGV+=("$line")
  done <"$case_dir/args"
  if [ -f "$case_dir/graft_args" ]; then
    while IFS= read -r line || [ -n "$line" ]; do
      PARITY_GRAFT_ARGV+=("$line")
    done <"$case_dir/graft_args"
  fi
}

# parity_copy CASE_DIR prints the path of a fresh temporary copy of the
# case without its want/ directory, resolved through symlinks so the path
# matches what a program sees as its working directory.
parity_copy() {
  local case_dir="$1" tmp
  tmp="$(mktemp -d)"
  tmp="$(cd "$tmp" && pwd -P)"
  (cd "$case_dir" && tar -cf - --exclude=./want .) | (cd "$tmp" && tar -xf -)
  printf '%s\n' "$tmp"
}

# parity_is_control NAME is true for the files in a case directory that
# describe the case, as opposed to input files the program may read.
parity_is_control() {
  case "$1" in
    args | stdin | graft_args | graft-stdout | phase | stderr-exact | want) return 0 ;;
  esac
  return 1
}
