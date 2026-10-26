#!/usr/bin/env bash
# run.sh audits the regex parity corpus by hand. It checks that the
# goldens under want/ still match the pinned jq and pcre2test and then runs
# the parity tests against them. When either binary is missing or has a
# version other than versions.env pins, it skips, unless
# QUERY_COMPAT_STRICT=1 makes that a failure.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)

if ! "$here/gen.sh" --versions 2>"${TMPDIR:-/tmp}/regex-parity-versions.$$"; then
	reason=$(cat "${TMPDIR:-/tmp}/regex-parity-versions.$$")
	rm -f "${TMPDIR:-/tmp}/regex-parity-versions.$$"
	if [ "${QUERY_COMPAT_STRICT:-}" = 1 ]; then
		echo "run.sh: $reason" >&2
		exit 1
	fi
	echo "run.sh: skipping the regex parity audit: $reason"
	exit 0
fi
rm -f "${TMPDIR:-/tmp}/regex-parity-versions.$$"

"$here/gen.sh" --check
go test -C "$root" ./pkg/regex/ -run '^TestRegexParity' -count=1
