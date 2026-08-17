#!/usr/bin/env bash
# cache-identity.sh BIN
#
# The persistent-cache guarantee, enforced: run one graft binary over the
# byte-identity corpus three times - cache disabled, cache enabled with a
# fresh directory (every store is a miss), and cache enabled again over
# the now-populated directory (hits replay stored entries) - and fail
# unless every byte of stdout, stderr, and every exit code is identical
# across all three passes. A cached result may only ever be a faster
# route to the exact bytes a cache-off run produces.
set -euo pipefail

BIN="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
REPO="$(cd "$(dirname "$0")/.." && pwd)"

SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT
WORK="$SCRATCH/work"
CACHE_DIR="$SCRATCH/cache"

command -v sha256sum >/dev/null 2>&1 && SHA=sha256sum || SHA="shasum -a 256"

(cd "$REPO" && go run ./scripts/gen-workload "$WORK")

# Example dirs whose operators dial external services, plus shuffle,
# whose output is nondeterministic by design.
SKIP="aws aws-targets base64-decode nats nats-targets shuffle targets ternary unified-parser vault vault-defaults vault-migration vault-targets vault-try"

capture() { # capture OUTDIR CACHE_ENABLED
  local out="$1" cache_enabled="$2" name
  mkdir -p "$out"

  run() {
    name="$1"; shift
    set +e
    GRAFT_CACHE_L2_ENABLED="$cache_enabled" GRAFT_CACHE_L2_PATH="$CACHE_DIR" \
      "$@" >"$out/$name.out" 2>"$out/$name.err"
    echo "$?" >"$out/$name.rc"
    set -e
  }

  local overlays gp_overlays
  overlays=$(ls "$WORK"/o[0-3]*.yml "$WORK"/o40.yml | sort)
  gp_overlays=$(ls "$WORK"/o*.yml | sort)

  run heavy-gp      "$BIN" merge --multi-doc --go-patch "$WORK/big.yml" $gp_overlays
  run heavy-gp-skip "$BIN" merge --skip-eval --multi-doc --go-patch "$WORK/big.yml" $gp_overlays
  run heavy-plain   "$BIN" merge "$WORK/big.yml" $overlays
  run heavy-cherry  "$BIN" merge --cherry-pick instance_groups "$WORK/big.yml" "$WORK/o01.yml" "$WORK/o02.yml"
  run heavy-prune   "$BIN" merge --prune meta --prune releases "$WORK/big.yml" "$WORK/o01.yml"
  run json-big      "$BIN" json "$WORK/big.yml"
  run dense-eval    "$BIN" merge "$WORK/dense.yml"

  local d files
  cd "$REPO/examples"
  for d in */; do
    d="${d%/}"
    case " $SKIP " in *" $d "*) continue ;; esac
    files=$(ls "$d"/*.yml 2>/dev/null | sort) || true
    [ -z "$files" ] && continue
    run "ex-$d" "$BIN" merge $files
  done
  cd "$REPO"

  (cd "$out" && $SHA ./*.out ./*.err ./*.rc | sort -k2 >MANIFEST)
}

capture "$SCRATCH/off"  false
capture "$SCRATCH/cold" true
capture "$SCRATCH/warm" true

fail=0
compare() { # compare LABEL A B
  local label="$1" a="$2" b="$3"
  if ! diff -u "$a/MANIFEST" "$b/MANIFEST"; then
    echo ""
    echo "cache-identity: FAILED - $label output differs" >&2
    for f in $(diff "$a/MANIFEST" "$b/MANIFEST" | sed -n 's/^[<>] *[0-9a-f]* *//p' | sort -u); do
      echo "--- diff for $f ---" >&2
      diff "$a/$f" "$b/$f" >&2 | head -40 || true
    done
    fail=1
  fi
}

compare "off vs cold (cache-enabled first run)" "$SCRATCH/off"  "$SCRATCH/cold"
compare "cold vs warm (cache-hit second run)"   "$SCRATCH/cold" "$SCRATCH/warm"
[ "$fail" -eq 0 ] || exit 1

entries=$(find "$CACHE_DIR" -type f ! -name '.*' 2>/dev/null | wc -l | tr -d ' ')
if [ "$entries" -eq 0 ]; then
  echo "cache-identity: FAILED - cache-enabled passes stored no entries" >&2
  exit 1
fi

echo "cache-identity: OK ($(grep -c . "$SCRATCH/off/MANIFEST") artifacts identical across off/cold/warm; $entries cache entries)"
