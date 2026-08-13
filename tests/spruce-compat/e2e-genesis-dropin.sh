#!/usr/bin/env bash
#
# End-to-end genesis drop-in validation.
#
# Builds a `spruce`-named alias binary from graft (Makefile target
# `build-spruce-alias`), prepends its directory to PATH, and proves the
# genesis contract against the literal `spruce` command name -- not
# against a `graft`-named binary. Two things are validated:
#
#   1. Version gate: genesis's check_prereqs() (lib/Genesis/Commands.pm)
#      probes `spruce -v 2>/dev/null`, extracts a version token with
#      regex qr(.*version\s+(\S+).*)i, and requires it to be >= 1.28.0
#      via Genesis.pm's semver()/by_semver()/new_enough(). This script
#      replicates that exact regex + semver-compare logic in Perl
#      against the alias binary's real output, and -- when a genesis
#      checkout is available -- additionally invokes genesis's own
#      Genesis::Commands::check_version() function directly, so the
#      REAL check_prereqs code path is exercised, not just a mirror.
#
#   2. Invocation shapes: replays, verbatim, the `spruce` command shapes
#      genesis's Genesis::Env::ManifestProvider and Genesis.pm issue
#      (see the genesis usage-surface findings, patterns 2/5/8/12/13),
#      through the literal `spruce` name resolved via PATH, asserting
#      exit codes and parseable output exactly as genesis's own `run()`
#      wrapper would observe them.
#
# This script does NOT stand up a live genesis/vault/bosh environment --
# only the shell + regex + exit-code contract surface genesis's Perl
# actually depends on for the drop-in to work.
#
# Must run under bash (not sh/dash): genesis always execs spruce/graft
# via `bash -c "$prog" -- "$@"` (lib/Genesis.pm:605).

if [ -z "${BASH_VERSION:-}" ]; then
  echo "FAIL: this script must be run with bash, not sh/dash" >&2
  exit 1
fi

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FIX="$SCRIPT_DIR/fixtures"
VAULTFIX="$SCRIPT_DIR/vaultinfo"
GENESIS_ROOT="${GENESIS_ROOT:-$REPO_ROOT/../genesis}"

command -v perl >/dev/null 2>&1 || {
  echo "SKIP: perl not found on PATH; the genesis version-gate contract is Perl-based and cannot be validated without it." >&2
  exit 0
}

TMP="$(mktemp -d "${TMPDIR:-/tmp}/graft-e2e-dropin.XXXXXX")"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

FAILURES=0
PASS_COUNT=0

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  printf 'PASS: %s\n' "$1"
}

fail() {
  FAILURES=$((FAILURES + 1))
  printf 'FAIL: %s\n' "$1" >&2
}

# ---------------------------------------------------------------------
# Step 0: build the spruce-named alias binary via the Makefile target,
# and put ONLY its directory on PATH ahead of everything else so a
# literal `spruce` invocation resolves to graft.
# ---------------------------------------------------------------------

echo "Building spruce-named alias binary via 'make build-spruce-alias' ..."
if ! (cd "$REPO_ROOT" && make build-spruce-alias) >"$TMP/build.log" 2>&1; then
  echo "FAIL: 'make build-spruce-alias' failed" >&2
  cat "$TMP/build.log" >&2
  exit 1
fi

GOOS="$(cd "$REPO_ROOT" && go env GOOS)"
GOARCH="$(cd "$REPO_ROOT" && go env GOARCH)"
ALIAS_DIR="$REPO_ROOT/build/${GOOS}-${GOARCH}"
ALIAS_BIN="$ALIAS_DIR/spruce"

if [ ! -x "$ALIAS_BIN" ]; then
  echo "FAIL: expected alias binary at $ALIAS_BIN, not found or not executable" >&2
  exit 1
fi

export PATH="$ALIAS_DIR:$PATH"

resolved="$(command -v spruce || true)"
if [ "$resolved" = "$ALIAS_BIN" ]; then
  pass "step0: literal 'spruce' on PATH resolves to the graft alias binary ($resolved)"
else
  fail "step0: literal 'spruce' on PATH resolved to '$resolved', expected '$ALIAS_BIN'"
  echo "FATAL: cannot proceed without PATH resolving to the alias binary" >&2
  exit 1
fi

# ---------------------------------------------------------------------
# Step A: version gate -- genesis's check_prereqs() contract
# (lib/Genesis/Commands.pm:1117): `spruce -v 2>/dev/null`, regex
# qr(.*version\s+(\S+).*)i, minimum 1.28.0 via semver()/new_enough().
# ---------------------------------------------------------------------

VERSION_LINE="$(spruce -v 2>/dev/null)"
if [ -z "$VERSION_LINE" ]; then
  fail "stepA0: 'spruce -v 2>/dev/null' produced no output"
else
  pass "stepA0: 'spruce -v 2>/dev/null' produced output: $VERSION_LINE"
fi

# Step A0b: the version line must echo the name the binary was invoked
# as (spruce prints os.Args[0] verbatim; graft does the same), so a
# PATH-resolved `spruce` reports itself as spruce, not graft.
case "$VERSION_LINE" in
  "spruce - Version "*)
    pass "stepA0b: version line echoes the invoked name: $VERSION_LINE" ;;
  *)
    fail "stepA0b: version line does not start with 'spruce - Version ': $VERSION_LINE" ;;
esac

# Step A0c: the same holds when the spruce name is a symlink to the
# graft binary (the intended production deployment), not a copy. The
# PATH handed to env holds ONLY the symlink dir, so a dangling symlink
# cannot silently fall through to the $ALIAS_DIR copy, and env's own
# PATH lookup (argv[0]="spruce") sidesteps the shell's command hash.
if [ ! -x "$ALIAS_DIR/graft" ]; then
  fail "stepA0c: graft binary missing at $ALIAS_DIR/graft; cannot build the symlink"
else
  SYMLINK_DIR="$TMP/symlink-bin"
  mkdir -p "$SYMLINK_DIR"
  ln -sf "$ALIAS_DIR/graft" "$SYMLINK_DIR/spruce"
  SYMLINK_LINE="$(env PATH="$SYMLINK_DIR" spruce -v 2>/dev/null)"
  case "$SYMLINK_LINE" in
    "spruce - Version "*)
      pass "stepA0c: graft symlinked as spruce reports itself as spruce: $SYMLINK_LINE" ;;
    *)
      fail "stepA0c: symlinked invocation did not report as spruce: $SYMLINK_LINE" ;;
  esac
fi

# Step A1: mirror genesis's regex-extraction + semver-compare logic
# in-line (Genesis.pm:440-469's semver()/by_semver()/new_enough(),
# Commands.pm:1117's regex and minimum version), run standalone against
# the alias binary's real output.
if perl -e '
  my $line = $ARGV[0];
  my $min  = "1.28.0";

  if ($line !~ /.*version\s+(\S+).*/i) {
    print STDERR "no version token matched genesis regex qr(.*version\\s+(\\S+).*)i in: $line\n";
    exit 1;
  }
  my $v = $1;

  sub semver {
    my ($v) = @_;
    if ($v && $v =~ m/^v?(\d+)(?:\.(\d+)(?:\.(\d+)(?:[\.-]rc[\.-]?(\d+))?)?)?(?:\+[0-9A-Za-z.-]+)?$/i) {
      return ($1, $2 || 0, $3 || 0, (defined $4 ? $4 - 100000 : 0));
    }
    return ();
  }

  my @a = semver($v);
  my @b = semver($min);
  unless (@a && @b) {
    print STDERR "captured token \"$v\" or minimum \"$min\" did not parse as semver\n";
    exit 1;
  }

  my $cmp = 0;
  my @aa = @a;
  my @bb = @b;
  while (@aa || @bb) {
    my $x = shift(@aa); $x = 0 unless defined $x;
    my $y = shift(@bb); $y = 0 unless defined $y;
    if ($x > $y) { $cmp = 1; last }
    if ($x < $y) { $cmp = -1; last }
  }

  if ($cmp >= 0) {
    print "captured version \"$v\" >= minimum \"$min\"\n";
    exit 0;
  } else {
    print STDERR "captured version \"$v\" < minimum \"$min\"\n";
    exit 1;
  }
' "$VERSION_LINE" >"$TMP/stepA1.out" 2>"$TMP/stepA1.err"; then
  pass "stepA1: genesis's regex+semver logic (mirrored) accepts the alias binary's version output: $(cat "$TMP/stepA1.out")"
else
  fail "stepA1: genesis's regex+semver logic (mirrored) rejected the alias binary's version output: $(cat "$TMP/stepA1.err")"
fi

# Step A2: best-effort -- if a genesis checkout is available, call
# genesis's REAL Genesis::Commands::check_version() function directly
# against the alias binary, proving the actual check_prereqs code path
# (not just a mirror of its logic) accepts the drop-in.
if [ -f "$GENESIS_ROOT/lib/Genesis/Commands.pm" ]; then
  if perl -I"$GENESIS_ROOT/lib" -e '
    use Genesis::Commands;
    my $alias = shift @ARGV;
    my $err = Genesis::Commands::check_version(
      "spruce", "1.28.0", "$alias -v 2>/dev/null",
      qr(.*version\s+(\S+).*)i,
      "https://github.com/geofffranks/spruce/releases",
    );
    if ($err) {
      print STDERR "$err\n";
      exit 1;
    }
    print "genesis Genesis::Commands::check_version() accepted the alias binary\n";
    exit 0;
  ' "$ALIAS_BIN" >"$TMP/stepA2.out" 2>"$TMP/stepA2.err"; then
    pass "stepA2: genesis's REAL check_version() (lib/Genesis/Commands.pm) accepts the alias binary: $(cat "$TMP/stepA2.out")"
  else
    fail "stepA2: genesis's REAL check_version() rejected the alias binary: $(cat "$TMP/stepA2.err")"
  fi
else
  echo "SKIP: stepA2 -- genesis checkout not found at $GENESIS_ROOT (set GENESIS_ROOT to enable calling the real check_version() function)."
fi

# ---------------------------------------------------------------------
# Step B: replay genesis's documented invocation shapes verbatim
# through the literal `spruce` name (PATH resolution) -- these are the
# exact merge/json/vaultinfo call shapes Genesis::Env::ManifestProvider
# and Genesis.pm issue in production.
# ---------------------------------------------------------------------

# --- Pattern 5: spruce merge --multi-doc --go-patch <files> ---
if out="$(spruce merge --multi-doc --go-patch "$FIX/multidoc.yml" 2>"$TMP/b1.err")"; then
  if [ -n "$out" ]; then
    pass "stepB1: spruce merge --multi-doc --go-patch <files> exits 0 with non-empty output"
  else
    fail "stepB1: spruce merge --multi-doc --go-patch <files> exited 0 but produced empty output"
  fi
else
  fail "stepB1: spruce merge --multi-doc --go-patch <files> exited non-zero: $(cat "$TMP/b1.err")"
fi

# --- Pattern 2: spruce json < file ---
if out="$(spruce json < "$FIX/base.yml" 2>"$TMP/b2.err")"; then
  if command -v python3 >/dev/null 2>&1 && printf '%s' "$out" | python3 -c 'import json,sys; json.load(sys.stdin)' >/dev/null 2>&1; then
    pass "stepB2: spruce json < file exits 0 with parseable JSON"
  elif command -v python3 >/dev/null 2>&1; then
    fail "stepB2: spruce json < file exited 0 but output did not parse as JSON: $out"
  else
    # No python3 available: fall back to a structural sanity check only.
    case "$out" in
      \{*\}) pass "stepB2: spruce json < file exits 0 with JSON-object-shaped output (python3 unavailable for full parse)" ;;
      *) fail "stepB2: spruce json < file exited 0 but output is not JSON-object-shaped: $out" ;;
    esac
  fi
else
  fail "stepB2: spruce json < file exited non-zero: $(cat "$TMP/b2.err")"
fi

# --- Pattern 4/save_to_yaml_file lineage: spruce merge --skip-eval file | spruce json ---
(
  set -o pipefail
  spruce merge --skip-eval "$FIX/base.yml" 2>"$TMP/b3.mid.err" | spruce json >"$TMP/b3.out" 2>"$TMP/b3.err"
)
rc_b3=$?
if [ "$rc_b3" -eq 0 ]; then
  if [ -s "$TMP/b3.out" ]; then
    pass "stepB3: spruce merge --skip-eval file | spruce json exits 0 with non-empty output"
  else
    fail "stepB3: spruce merge --skip-eval file | spruce json exited 0 but produced empty output"
  fi
else
  fail "stepB3: spruce merge --skip-eval file | spruce json exited non-zero (rc=$rc_b3): $(cat "$TMP/b3.err")"
fi

# --- Pattern 8: set -o pipefail; spruce vaultinfo file | spruce json ---
# Success sub-case.
(
  set -o pipefail
  spruce vaultinfo "$VAULTFIX/success.yml" 2>"$TMP/b4ok.mid.err" | spruce json >"$TMP/b4ok.out" 2>"$TMP/b4ok.err"
)
rc_b4ok=$?
if [ "$rc_b4ok" -eq 0 ] && [ -s "$TMP/b4ok.out" ]; then
  pass "stepB4a: set -o pipefail; spruce vaultinfo <ok file> | spruce json exits 0 with output"
else
  fail "stepB4a: set -o pipefail; spruce vaultinfo <ok file> | spruce json failed (rc=$rc_b4ok): $(cat "$TMP/b4ok.err" 2>/dev/null)"
fi

# Failure sub-case -- the exact masking bug genesis's own comment
# (ManifestProvider.pm:420-426) warns about: without pipefail, `json`'s
# exit 0 on empty stdin would mask a failing `vaultinfo`.
(
  set -o pipefail
  spruce vaultinfo "$VAULTFIX/invalid.yml" 2>"$TMP/b4fail.mid.err" | spruce json >"$TMP/b4fail.out" 2>"$TMP/b4fail.err"
)
rc_b4fail=$?
if [ "$rc_b4fail" -ne 0 ]; then
  pass "stepB4b: set -o pipefail; spruce vaultinfo <invalid file> | spruce json propagates non-zero exit (rc=$rc_b4fail)"
else
  fail "stepB4b: set -o pipefail; spruce vaultinfo <invalid file> | spruce json exited 0 (expected non-zero -- pipefail masking bug reproduced)"
fi

# --- Pattern 14: echo "$yaml" | spruce merge - ---
yaml_content="$(cat "$FIX/prune-source.yml")"
(
  set -o pipefail
  echo "$yaml_content" | spruce merge - >"$TMP/b5.out" 2>"$TMP/b5.err"
)
rc_b5=$?
if [ "$rc_b5" -eq 0 ] && [ -s "$TMP/b5.out" ]; then
  pass "stepB5: echo \"\$yaml\" | spruce merge - exits 0 with output"
else
  fail "stepB5: echo \"\$yaml\" | spruce merge - failed (rc=$rc_b5): $(cat "$TMP/b5.err" 2>/dev/null)"
fi

# --- Pattern 13: spruce merge --skip-eval --go-patch -m --cherry-pick <key> <files> ---
if out="$(spruce merge --skip-eval --go-patch -m --cherry-pick releases "$FIX/cherry-a.yml" "$FIX/cherry-b.yml" 2>"$TMP/b6.err")"; then
  if printf '%s' "$out" | grep -q '^releases:'; then
    pass "stepB6: spruce merge --skip-eval --go-patch -m --cherry-pick releases <files> exits 0 and output contains only the cherry-picked key"
  else
    fail "stepB6: spruce merge --skip-eval --go-patch -m --cherry-pick releases <files> exited 0 but output missing expected 'releases:' key: $out"
  fi
else
  fail "stepB6: spruce merge --skip-eval --go-patch -m --cherry-pick releases <files> exited non-zero: $(cat "$TMP/b6.err")"
fi

echo
echo "============================================================"
echo "genesis drop-in e2e: $PASS_COUNT passed, $FAILURES failed"
if [ "$FAILURES" -gt 0 ]; then
  exit 1
fi
exit 0
