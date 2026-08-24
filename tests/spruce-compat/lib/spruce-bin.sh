#!/usr/bin/env bash
# Identify the binary the parity runners compare graft against.
# Sourced by run.sh, run-key-order.sh, and operators/run-operators.sh;
# not meant to be executed directly.
#
# Every runner here resolves that binary by looking for `spruce` on PATH.
# graft ships as a spruce drop-in, and the documented setup puts a
# `spruce`-named symlink to graft on PATH so Genesis calls graft, so on a
# machine set up that way the lookup finds graft and the suite compares
# graft with itself: the semantic harness passes vacuously, and the
# byte-exact key-order runner fails every spruce case, because graft's
# key labels are deliberately not byte-identical to spruce's for coerced
# numeric keys.
#
# Neither answer is the parity signal this suite exists to give, so the
# runners ask first and treat a graft in spruce's clothing as no spruce
# at all.

# spruce_bin_is_graft <path> — true when <path> is graft rather than
# spruce, whatever name it is installed under.
spruce_bin_is_graft() {
  local bin="${1:-}"
  [ -n "$bin" ] && [ -x "$bin" ] || return 1
  # Under a spruce name, current graft prints spruce's version line first
  # and its own `graft version ...` line after it. Releases before 1.40.0
  # print the spruce line alone, which is indistinguishable from spruce,
  # so fall back to the help banner: it names the command graft is,
  # not the name it was invoked as.
  if "$bin" -v 2>&1 | grep -q '^graft version '; then
    return 0
  fi
  if "$bin" --help 2>&1 | head -1 | grep -q '^graft '; then
    return 0
  fi
  return 1
}

# spruce_bin_usable <path> — true when <path> is a spruce binary worth
# comparing against. Prints a note and returns false for a graft.
spruce_bin_usable() {
  local bin="${1:-}"
  [ -n "$bin" ] && [ -x "$bin" ] || return 1
  if spruce_bin_is_graft "$bin"; then
    echo "note: $bin is graft under a spruce name, not spruce itself; ignoring it for parity comparisons"
    return 1
  fi
  return 0
}

# spruce_bin_resolve <workdir> <repo-root> — print the path of a real
# spruce binary and return 0, or print nothing and return 1. Candidates,
# in order: $SPRUCE_BIN, a `spruce` on PATH, and a build from
# $SPRUCE_REPO (default: a spruce checkout beside <repo-root>). Notes
# about rejected candidates go to stderr, so the caller can capture the
# path from stdout.
spruce_bin_resolve() {
  local workdir="${1:-}" repo_root="${2:-}"

  if spruce_bin_usable "${SPRUCE_BIN:-}" >&2; then
    printf '%s\n' "$SPRUCE_BIN"
    return 0
  fi

  local on_path
  on_path="$(command -v spruce || true)"
  if spruce_bin_usable "$on_path" >&2; then
    printf '%s\n' "$on_path"
    return 0
  fi

  local repo="${SPRUCE_REPO:-$repo_root/../spruce}"
  if [ -n "$workdir" ] && [ -d "$repo/cmd/spruce" ]; then
    local out="$workdir/spruce"
    # NOTE: build ./cmd/spruce specifically, not the module root — `go
    # build .` at spruce's repo root produces an archive package, not a
    # binary.
    if (cd "$repo" && go build -o "$out" ./cmd/spruce) >"$workdir/spruce-build.log" 2>&1; then
      printf '%s\n' "$out"
      return 0
    fi
  fi

  return 1
}
