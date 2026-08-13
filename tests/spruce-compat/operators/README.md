# Operator parity suite

Runs graft's operator fixtures through both `graft` and `spruce`, comparing
stdout, stderr, and exit code. Covers the high-frequency operators from real
genesis kit/deployment usage (`grab`, `concat`, `param`, `static_ips`, array
merge markers) plus the full operator set graft implements.

## Running

```
./run-operators.sh
```

Skips cleanly (exit 0) if no `spruce` binary is on `PATH`. Override binary
locations with `SPRUCE_BIN=/path/to/spruce` and `GRAFT_BIN=/path/to/graft`;
without `GRAFT_BIN`, the script builds one from this repository's
`cmd/graft` into a temp directory.

Exit code is non-zero only when a real, undocumented divergence is found.
Documented, intentional divergences (graft extensions with no spruce
equivalent, or known formatting-only differences) print as `INFO` and do not
fail the run.

## Fixture case contract

Each case is a directory under `fixtures/<operator>/<case-name>/`:

| File | Required | Meaning |
|---|---|---|
| `*.yml` | yes | Merge inputs, applied in sorted filename order |
| `flags` | no | Extra CLI flags, one per line, appended after the verb |
| `env` | no | `KEY=VALUE` lines, exported for both invocations |
| `verb` | no | CLI verb to run (default `merge`); e.g. `vaultinfo`, `json` |
| `mode` | no | `structural` for order-independent comparison (see below); default is byte-exact |
| `expect-divergence` | no | Free-text reason; presence turns a mismatch into an `INFO` line instead of `FAIL` |

Directories prefixed with `_` (e.g. `fixtures/file/_support/`) hold support
data referenced by a case's `env` file rather than a runnable case, and are
skipped by the scanner.

`structural` mode is for operators with intentionally random output
(`shuffle`): it re-parses each tool's own merged YAML through that same
tool's `json` verb, sorts the `result` array, and compares the sorted sets
instead of raw bytes.

## Backend-dependent operators

`vault`, `awsparam`/`awssecret` are exercised without a live backend:

- a `REDACT=1` smoke fixture, asserting both tools emit the literal string
  `REDACTED` without attempting a backend call

- for `vault`, an additional error-path fixture with a malformed
  `path:key` argument, comparing the parse-error text; this fires before
  either tool would attempt a network call, so it needs only non-empty
  dummy `VAULT_ADDR`/`VAULT_TOKEN` values, not a reachable Vault

`nats` has no spruce equivalent (spruce's operator set has no `nats`
operator) and is not run against spruce; see `fixtures/nats/no-spruce-equivalent`.

## Divergences found (and since fixed) while building this suite

Two sort divergences were found by this suite and have been fixed; their
fixtures now pass as regression pins:

- **`(( sort by "name" ))` (quoted key)**: spruce rejects the list with
  `is a list with map entries, where some do not contain "name"` — spruce
  treats the quoted key literally rather than as the field name. graft
  used to leave the list silently unsorted and exit 0; it now fails with
  spruce's exact error. See `fixtures/sort/quoted-key-divergence/`.

- **orphaned `(( sort ))`** (no earlier document's array exists at the
  path being overwritten): spruce errors with `orphaned (( sort ))
  operator at $.path, no list exists at that path`. graft used to leave
  the marker text unresolved in the output and exit 0; it now emits the
  same error. See `fixtures/sort/orphaned-error/`.

The remaining post-merge sort failure cases (a queued sort whose path was
pruned away, resolves to a map or a scalar, or names a sort key some list
entries lack) are pinned — in both normal and `--skip-eval` modes — by the
`fixtures/sort/unresolved-path*`, `non-list-*`, and `bad-sort-key-skip-eval`
cases.

Documented (non-failing) formatting-only divergences, kept for regression
tracking:

- `stringify` renders a multi-line string as a double-quoted flow scalar in
  graft vs. a literal block scalar (`|`) in spruce — same data, different
  YAML scalar style.

- A `static_ips`-produced string containing `" - "` (e.g. an IP range
  literal echoed back unchanged) gets double-quoted by graft on re-emit but
  left bare by spruce.

- `GRAFT_FILE_BASE_PATH` is a graft-only override with no spruce
  equivalent; spruce only recognizes `SPRUCE_FILE_BASE_PATH`.

- `(( null ))` is a real, evaluating operator in graft. spruce has no such
  operator — any unrecognized zero-arg operator name is left as literal
  unresolved text by spruce's generic unknown-operator fallback.
