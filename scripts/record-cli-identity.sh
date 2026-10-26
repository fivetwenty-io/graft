#!/usr/bin/env bash
# record-cli-identity.sh GRAFT_BIN [DIR]
#
# Record the CLI identity goldens: run GRAFT_BIN over every invocation in
# DIR/cases (DIR defaults to tests/cli-identity) and keep its stdout,
# stderr, and exit code under DIR/golden. cmd/graft/cli_identity_test.go
# replays the same corpus in process and compares byte for byte, so the
# help text, usage line, and the fan, cache, and debug surfaces of an
# existing graft cannot drift while new commands are added.
#
# Record from the released binary, never from the working tree, so the
# goldens describe what graft did before the change under test.
set -euo pipefail

BIN="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
DIR="$(cd "${2:-$REPO/tests/cli-identity}" && pwd)"
FIXTURES="$DIR/fixtures"
GOLDEN="$DIR/golden"

rm -rf "$GOLDEN"
mkdir -p "$GOLDEN"

count=0
while read -r name stdin args; do
	case "$name" in '' | '#'*) continue ;; esac
	input=/dev/null
	[ "$stdin" = "-" ] || input="$DIR/$stdin"
	# shellcheck disable=SC2086 # the argument list splits on whitespace by design
	set -- $args
	rc=0
	(cd "$FIXTURES" && env -i PATH="$PATH" TERM=dumb NO_COLOR=1 HOME="$FIXTURES" GRAFT_CACHE_L2_PATH=cache \
		"$BIN" "$@" <"$input" >"$GOLDEN/$name.out" 2>"$GOLDEN/$name.err") || rc=$?
	echo "$rc" >"$GOLDEN/$name.rc"
	count=$((count + 1))
done <"$DIR/cases"

echo "record-cli-identity: recorded $count invocations in $GOLDEN"
