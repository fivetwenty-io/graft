#!/usr/bin/env bash
# byte-identity.sh BASE_BIN HEAD_BIN
#
# The drop-in guarantee, enforced: run two graft binaries over the same
# corpus of invocation shapes and fail unless every byte of stdout,
# stderr, and every exit code is identical. The corpus is a generated
# Genesis-shaped merge workload (scripts/gen-workload) plus the repo's
# example fixtures, covering merge, multi-doc, go-patch, cherry-pick,
# prune, and json across operator-light and operator-heavy documents.
#
# Any intentional output change must be visible here as a failing diff
# and justified in the change that carries it.
set -euo pipefail

BASE_BIN="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
HEAD_BIN="$(cd "$(dirname "$2")" && pwd)/$(basename "$2")"
REPO="$(cd "$(dirname "$0")/.." && pwd)"

SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT
WORK="$SCRATCH/work"

command -v sha256sum >/dev/null 2>&1 && SHA=sha256sum || SHA="shasum -a 256"

(cd "$REPO" && go run ./scripts/gen-workload "$WORK")

# Example dirs whose operators dial external services, plus shuffle,
# whose output is nondeterministic by design.
SKIP="aws aws-targets base64-decode nats nats-targets shuffle targets ternary unified-parser vault vault-defaults vault-migration vault-targets vault-try"

capture() { # capture BIN OUTDIR
  local bin="$1" out="$2" name
  mkdir -p "$out"

  run() {
    name="$1"; shift
    set +e
    "$@" >"$out/$name.out" 2>"$out/$name.err"
    echo "$?" >"$out/$name.rc"
    set -e
  }

  local overlays gp_overlays
  overlays=$(ls "$WORK"/o[0-3]*.yml "$WORK"/o40.yml | sort)
  gp_overlays=$(ls "$WORK"/o*.yml | sort)

  run heavy-gp      "$bin" merge --multi-doc --go-patch "$WORK/big.yml" $gp_overlays
  run heavy-gp-skip "$bin" merge --skip-eval --multi-doc --go-patch "$WORK/big.yml" $gp_overlays
  run heavy-plain   "$bin" merge "$WORK/big.yml" $overlays
  run heavy-cherry  "$bin" merge --cherry-pick instance_groups "$WORK/big.yml" "$WORK/o01.yml" "$WORK/o02.yml"
  run heavy-prune   "$bin" merge --prune meta --prune releases "$WORK/big.yml" "$WORK/o01.yml"
  run json-big      "$bin" json "$WORK/big.yml"

  local d files
  cd "$REPO/examples"
  for d in */; do
    d="${d%/}"
    case " $SKIP " in *" $d "*) continue ;; esac
    files=$(ls "$d"/*.yml 2>/dev/null | sort) || true
    [ -z "$files" ] && continue
    run "ex-$d" "$bin" merge $files
  done
  cd "$REPO"

  (cd "$out" && $SHA ./*.out ./*.err ./*.rc | sort -k2 >MANIFEST)
}

capture "$BASE_BIN" "$SCRATCH/base"
capture "$HEAD_BIN" "$SCRATCH/head"

if ! diff -u "$SCRATCH/base/MANIFEST" "$SCRATCH/head/MANIFEST"; then
  echo ""
  echo "byte-identity: FAILED - output differs between binaries" >&2
  for f in $(diff "$SCRATCH/base/MANIFEST" "$SCRATCH/head/MANIFEST" | sed -n 's/^[<>] *[0-9a-f]* *//p' | sort -u); do
    echo "--- diff for $f ---" >&2
    diff "$SCRATCH/base/$f" "$SCRATCH/head/$f" >&2 | head -40 || true
  done
  exit 1
fi

echo "byte-identity: OK ($(grep -c . "$SCRATCH/base/MANIFEST") artifacts identical)"
