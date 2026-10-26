#!/usr/bin/env bash
# gen.sh records the regex parity goldens under want/, one per case in
# cases.yaml, from jq for an Oniguruma case and from pcre2test for a PCRE
# case. It refuses to run when either binary's version differs from
# versions.env.
#
#   gen.sh             record into a temporary directory and swap it in for want/
#   gen.sh --check     record into a temporary directory and diff it with want/
#   gen.sh --versions  check the binaries' versions and record nothing
#
# JQ_BIN and PCRE2TEST name the binaries and default to jq and pcre2test
# on PATH. The unit tests read the goldens and never run either binary.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
# shellcheck source=versions.env
. "$here/versions.env"

die() {
	echo "gen.sh: $*" >&2
	exit 1
}

jq_bin=$(command -v "${JQ_BIN:-jq}") || die "jq not found; set JQ_BIN"
pcre2test_bin=$(command -v "${PCRE2TEST:-pcre2test}") || die "pcre2test not found; set PCRE2TEST"

jq_version=$("$jq_bin" --version)
[ "$jq_version" = "jq-$JQ_VERSION" ] || die "$jq_bin is $jq_version, want jq-$JQ_VERSION"
pcre2_version=$("$pcre2test_bin" -version | awk '{print $3}')
[ "$pcre2_version" = "$PCRE2_VERSION" ] || die "$pcre2test_bin is PCRE2 $pcre2_version, want $PCRE2_VERSION"

record() {
	local log
	log=$(mktemp)
	if ! REGEX_PARITY_OUT="$1" JQ_BIN="$jq_bin" PCRE2TEST="$pcre2test_bin" \
		go test -C "$root" ./pkg/regex/ -run '^TestRecordParityGoldens$' -count=1 >"$log" 2>&1; then
		cat "$log" >&2
		rm -f "$log"
		die "recording failed"
	fi
	rm -f "$log"
}

case "${1:-}" in
"")
	# Record beside want/ and swap the new goldens in only when every case
	# recorded, so a failed run leaves the old goldens in place.
	tmp=$(mktemp -d "$here/want.XXXXXX")
	trap 'rm -rf "$tmp"' EXIT
	record "$tmp"
	chmod 755 "$tmp"
	rm -rf "$here/want"
	mv "$tmp" "$here/want"
	echo "gen.sh: recorded $(find "$here/want" -name '*.json' | wc -l | tr -d ' ') goldens"
	;;
--check)
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	record "$tmp"
	diff -r "$here/want" "$tmp" || die "the goldens under want/ are stale; run gen.sh"
	echo "gen.sh: goldens match jq $JQ_VERSION and PCRE2 $PCRE2_VERSION"
	;;
--versions) ;;
*)
	die "usage: gen.sh [--check | --versions]"
	;;
esac
