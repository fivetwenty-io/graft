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

# graft jq joins the corpus only once the base binary has it, so the check
# keeps passing before the first release that includes the command and
# starts guarding afterwards.
BASE_HAS_JQ=0
if "$BASE_BIN" jq --help >/dev/null 2>&1; then BASE_HAS_JQ=1; fi

# The jq link joins the corpus the same way, once the base binary answers
# to the jq name.
BASE_HAS_JQ_LINK=0
if [ "$BASE_HAS_JQ" = 1 ]; then
  mkdir -p "$SCRATCH/probe"
  ln -sf "$(cd "$(dirname "$BASE_BIN")" && pwd)/$(basename "$BASE_BIN")" "$SCRATCH/probe/jq"
  case "$("$SCRATCH/probe/jq" --version 2>/dev/null | head -1)" in jq-*) BASE_HAS_JQ_LINK=1 ;; esac
fi

# graft yq and the yq link join the corpus the same way.
BASE_HAS_YQ=0
if "$BASE_BIN" yq --help >/dev/null 2>&1; then BASE_HAS_YQ=1; fi

BASE_HAS_YQ_LINK=0
if [ "$BASE_HAS_YQ" = 1 ]; then
  mkdir -p "$SCRATCH/probe"
  ln -sf "$(cd "$(dirname "$BASE_BIN")" && pwd)/$(basename "$BASE_BIN")" "$SCRATCH/probe/yq"
  case "$("$SCRATCH/probe/yq" --version 2>/dev/null | head -1)" in yq\ *) BASE_HAS_YQ_LINK=1 ;; esac
fi

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

  run_in() { # run_in NAME STDIN_FILE CMD...
    local stdin_file="$2"
    name="$1"; shift 2
    set +e
    "$@" <"$stdin_file" >"$out/$name.out" 2>"$out/$name.err"
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
  run dense-eval    "$bin" merge "$WORK/dense.yml"

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

  if [ "$BASE_HAS_JQ" = 1 ]; then
    printf '{"a":[1,2,{"b":null}],"s":"x"}\n3 "four"\n' >"$WORK/jq-input.json"
    run_in jq-identity "$WORK/jq-input.json" "$bin" jq .
    run_in jq-compact  "$WORK/jq-input.json" "$bin" jq -c '.'
    run_in jq-null     "$WORK/jq-input.json" "$bin" jq -n -c '[inputs]'
    run_in jq-error    "$WORK/jq-input.json" "$bin" jq '.a.b.c'
    run_in jq-compile  "$WORK/jq-input.json" "$bin" jq '.a |'
  fi

  if [ "$BASE_HAS_JQ_LINK" = 1 ]; then
    local links jq_link
    links="$out.links"
    mkdir -p "$links"
    ln -sf "$(cd "$(dirname "$bin")" && pwd)/$(basename "$bin")" "$links/jq"
    jq_link="$links/jq"
    run_in jqlink-identity "$WORK/jq-input.json" "$jq_link" .
    run_in jqlink-compact  "$WORK/jq-input.json" "$jq_link" -c '.'
    run_in jqlink-null     "$WORK/jq-input.json" "$jq_link" -n -c '[inputs]'
    run_in jqlink-error    "$WORK/jq-input.json" "$jq_link" '.a.b.c'
    run_in jqlink-compile  "$WORK/jq-input.json" "$jq_link" '.a |'
  fi

  if [ "$BASE_HAS_YQ" = 1 ]; then
    printf 'a: 1 # one\nb:\n  - x\n  - y\n---\nc: [1, 2]\n' >"$WORK/yq-input.yaml"
    run_in yq-identity "$WORK/yq-input.yaml" "$bin" yq .
    run_in yq-json     "$WORK/yq-input.yaml" "$bin" yq -o json -I 0 '.'
    run_in yq-null     "$WORK/yq-input.yaml" "$bin" yq -n '.a.b = [1, 2]'
    run_in yq-error    "$WORK/yq-input.yaml" "$bin" yq '.a.b.c'
    run_in yq-compile  "$WORK/yq-input.yaml" "$bin" yq '.a |'
  fi

  if [ "$BASE_HAS_YQ_LINK" = 1 ]; then
    local ylinks yq_link
    ylinks="$out.ylinks"
    mkdir -p "$ylinks"
    ln -sf "$(cd "$(dirname "$bin")" && pwd)/$(basename "$bin")" "$ylinks/yq"
    yq_link="$ylinks/yq"
    run_in yqlink-identity "$WORK/yq-input.yaml" "$yq_link" .
    run_in yqlink-json     "$WORK/yq-input.yaml" "$yq_link" -o json -I 0 '.'
    run_in yqlink-null     "$WORK/yq-input.yaml" "$yq_link" -n '.a.b = [1, 2]'
    run_in yqlink-error    "$WORK/yq-input.yaml" "$yq_link" '.a.b.c'
    run_in yqlink-compile  "$WORK/yq-input.yaml" "$yq_link" '.a |'
  fi

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
