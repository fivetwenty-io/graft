# Genesis Compatibility Contract

Genesis shells out to `spruce` by resolving it from `PATH`; it has no
bundled binary and no override variable to point at a different binary
name. For graft to stand in as a drop-in replacement, it must satisfy every
pattern Genesis actually invokes, plus the exact text and formatting
contracts Genesis parses out of stdout, stderr, and the exit code. Both
are covered below.

## Binary resolution and version gate

Genesis resolves `spruce` from `PATH` before every command and probes
its version with `spruce -v`, extracting a token from the
case-insensitive pattern "the word `version` followed by whitespace,
then a non-whitespace token." That token is checked against a minimum
version of `1.28.0`; failing the check stops Genesis from running at
all.

Graft's `-v`/`--version` output follows the same
`"<program> - Version <version>"` shape spruce uses, echoing the name
the binary was invoked as (`os.Args[0]`, exactly like spruce), so a
graft binary reached through a `spruce` symlink or copy reports itself
as `spruce`. Like spruce, graft honors a version flag placed before
the verb ahead of dispatch: `spruce -v merge ...` prints the version
and exits 0 without reading input. Placed after the verb the two
diverge deliberately: spruce treats the token as a filename and exits
2 (`Error reading file -v`), while graft ignores the flag and runs the
verb normally. Graft must never honor a post-verb `-v`: a stray flag
in a scripted merge would write a version string into the captured
manifest with exit 0 (see the "When '-v' follows a subcommand" pin in
`cmd/graft/main_test.go`).

The version string is populated at build time via a linker flag
(`-ldflags "-X main.Version=$(VERSION)"` in the Makefile, taken from
the exact git tag when one matches); a binary built without that flag
falls back to a hardcoded copy of the current release version
(`var Version`, `cmd/graft/main.go`), which is a real semver above the
`1.28.0` minimum, so even an ad-hoc `go build` passes the gate. The
fallback must be bumped alongside each release so untagged builds do
not misreport an older version. A pre-verb `-v` is also handled before
`--color` and `--config` validation, so the version probe succeeds
even with a broken config file or `GRAFT_*` environment.

## The 16 invocation patterns

Every distinct way Genesis calls `spruce` today, and what graft's
equivalent command needs to produce:

| # | Pattern | Consumed as |
|---|---|---|
| 1 | `spruce diff <a> <b>`, run through a pty wrapper | Colorized diff text; exit code recovered from a wrapper-injected marker in the pty output, not read directly. |
| 2 | `spruce json < file` | The file's contents piped to stdin, decoded from the resulting JSON. |
| 3 | `spruce merge --skip-eval file \| line-post-process` | YAML text; a downstream step rejoins any output line that consists solely of `))` (a spruce line-wrap quirk, see [YAML formatting differences](yaml-formatting.md)). |
| 4 | `spruce merge --skip-eval file` | Raw YAML on stdout. |
| 5 | `spruce merge [--multi-doc] [--go-patch] [--skip-eval] files...` | Stdout written to a manifest file, then re-parsed as YAML. |
| 6 | `spruce merge --skip-eval files... file > out` with `--cherry-pick`/`--prune` | A subset file; stdout is not further parsed. |
| 7 | `spruce json file \| jq filter \| spruce merge --skip-eval` | A jq-filtered JSON subset fed back through merge. |
| 8 | `set -o pipefail; spruce vaultinfo file \| spruce json` | JSON of shape `{"secrets": [{"key": ..., "references": [...]}]}`. `pipefail` is required in the caller's shell; without it, `spruce json`'s success on empty stdin would mask a `vaultinfo` failure. |
| 9 | `spruce merge --multi-doc --go-patch files...` (no `--skip-eval`) | Stdout on success; on a non-zero exit, stderr is parsed line by line for adaptive retry. |
| 10 | `spruce merge --multi-doc --go-patch --skip-eval files... \| spruce json` | JSON of the unevaluated tree, used to look up values for deferred-operator rewriting. |
| 11 | `spruce merge --go-patch --multi-doc files... \| spruce json` | Kit metadata JSON. |
| 12 | `cat file \| spruce merge --skip-eval -` | YAML text, read from stdin via the `-` sentinel. |
| 13 | `spruce merge --skip-eval --go-patch -m --cherry-pick releases files...` | Confirms the `-m` short form of `--multi-doc` is required, not just the long form. |
| 14 | `echo "$manifest" \| spruce merge files... -` with `--prune` | A pruned manifest, input piped via `-`. |
| 15 | `cat file \| spruce merge --skip-eval --go-patch --multi-doc \| spruce json` | Multi-document JSON: one JSON object per output line, not one pretty-printed blob per document. |
| 16 | `spruce merge <opts> files...` from a kit hook | Raw output or a hard failure on non-zero exit; hash arguments are serialized to a temp YAML file first. |

## stderr format contract

Genesis parses spruce's stderr in two places, and both require exact
text, not just a non-zero exit code:

- **Per-error path/message lines.** Genesis's adaptive-merge retry logic
  matches each stderr line against the pattern
  `` - $.<path>: <message>``, one error per line. Graft already produces
  this exact line shape for merge and evaluation errors (verified
  directly against graft's own test suite, which asserts output like
  `` - $.secret: vault operator requires at least one argument``).

- **Vault-not-found detection.** Within a matched line, Genesis further
  checks the message text against the pattern "starts with `secret `,
  ends with ` not found`." Graft's vault operator raises exactly
  `secret <key> not found` for a missing secret, matching this
  substring requirement (checked directly against the source).

- **Vault-path retry loop.** The `vaultinfo` pipeline (pattern 8) has
  its own stderr scrape, matching `` - $.<path>:`` at the start of a
  line to collect unresolved paths for a bounded retry-with-prune loop.
  This is satisfied by the same error-line format described above.

## Vault key-parse error text

A malformed vault key (missing the `path:key` separator) must produce
the exact text `invalid argument <value>; must be in the form
path/to/secret:key`. Graft's vault operator already produces this exact
wording, confirmed by reading the source; the ANSI color tags wrapping
it are stripped in a non-tty context, which is how Genesis always
invokes it.

## Vault KV version detection

Spruce's vault client (`vaultkv`) asks the server which KV engine
version backs a mount before reading, via
`sys/internal/ui/mounts/<path>`, and inserts the `data/` path segment a
KV v2 read requires (`/v1/<mount>/data/<rest>`). Genesis-provisioned
vaults (including OpenBao labs) mount `secret/` as KV v2, so a client
that always reads `/v1/<mount>/<rest>` fails every lookup with
"Invalid path for a versioned K/V secrets engine".

Graft's vault reader (`internal/backends/vault/reader.go`) performs the
same mount lookup, caches the detected version per mount, rewrites v2
read paths to include `data/` (skipping paths that already carry the
prefix), unwraps the KV v2 `data`/`metadata` response envelope, and
degrades to v1 addressing when the mount endpoint is unavailable.
Pinned by `internal/backends/vault/reader_kv2_test.go`.

## Multi-document JSON framing

`spruce json` on multi-document input, and every pipeline that pipes
merge output into `json`, must produce exactly one JSON object per
output line, not a pretty-printed multi-line object per document.
Graft's `json` command writes each converted document with its own
trailing newline in a loop, so the framing matches.

## `vaultinfo` JSON shape

Pattern 8 requires `vaultinfo` piped into `json` to produce JSON keyed
as `secrets`, with each entry keyed as `key` and `references`, all
lowercase. Graft's `vaultinfo` output already renders these fields in
lowercase, and graft's own test suite confirms it, asserting output like
`secrets:\n- key: secret/bar:beep\n  references:`, matching the shape
Genesis expects once piped through `json`.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success. |
| `1` | Usage error, or `diff` found differences between the two inputs. |
| `2` | Runtime error during merge, evaluation, or JSON conversion. |

Graft's exit codes already follow this same three-way scheme across
`merge`, `fan`, `json`, `diff`, and `vaultinfo`.

## PATH-only resolution

Genesis has no override variable for the binary name or location; it
relies entirely on `PATH` lookup of the literal name `spruce`. A
drop-in deployment needs a `spruce`-named binary, alias, or symlink on
`PATH` rather than any configuration pointing at `graft` by a different
name.

## stdin handling

Several patterns read a full YAML document from stdin, either via shell
redirection (`spruce json < file`) or a piped `-` sentinel argument
(`spruce merge ... -`). Both forms need to keep working identically: a
bare positional `-` must be treated as "read this document from stdin,"
not as a literal filename.

## `pty`-conditional diff coloring

Genesis runs `spruce diff` through a pty wrapper specifically because
diff output is colorized only when spruce detects a real terminal on
its output stream, not when writing to a pipe. A drop-in replacement's
diff coloring must key off the same kind of terminal detection
(`isatty` on the output file descriptor), not always-on or always-off
coloring, so that Genesis's pty wrapper produces the colorized output it
expects.

Graft's `diff` command does this, with neither `--color` nor
`--no-color` given (auto mode, the default): its default report (no
`--changes`/`--unified`/`--side-by-side` flag) is colored by
[dyff](https://github.com/homeport/dyff)/bunt's own `isatty` check on
stdout, independent of graft's `--color` flag entirely; the
`--changes`/`--unified`/`--side-by-side` renderers are colored by
graft's own `--color` auto-detection, which also keys off `isatty` on
stdout (not stderr - see [Color flags](../reference/cli.md#color-flags)
for the full precedence, including how an explicit `--color`/
`--no-color` overrides this). Either way, confirmed by reading the
source: graft's diff coloring, in the mode Genesis relies on, checks
`isatty` on stdout, matching spruce.

This distinction (stdout vs. graft's stderr-based auto-detection for
every other command's diagnostics) is invisible to Genesis's own pty
wrapper: `fake_tty`/`script` attaches both stdout and stderr to a pty,
so both file descriptors report as terminals to graft regardless of
which one a given code path checks.

## Output byte stability across versions

Genesis re-parses almost every stdout it captures (manifests, JSON,
`vaultinfo`), so map-key order is semantically invisible to it — but
two output-bytes changes across graft versions are worth pinning here:

- **1.32.0 key ordering.** graft's YAML output changed from purely
  lexicographic key order to spruce's two-tier order (numeric-looking
  keys first, then natural-sorted strings; see
  [YAML formatting differences](yaml-formatting.md#known-differences)).
  This moves graft's bytes *toward* spruce's: manifests whose key sets
  contain digit runs (`z1a`/`z10a` AZs, numbered jobs) now serialize in
  the same order spruce would emit. A byte-level comparison between a
  pre-1.32.0 graft manifest and a regenerated one will show reordering
  on such key sets exactly once, with identical parsed content.

- **1.32.0 sort-marker failures.** Documents that previously merged
  with a dangling or mistyped `(( sort ... ))` marker now fail with
  exit 2 and spruce's exact error text (see the
  [known-gaps entry](known-gaps.md#sort-post-processing-silently-skips-two-error-cases)),
  also aligning with spruce.

- **`merge` gains a leading `---\n`.** `graft merge`'s stdout now leads
  with a `---\n` document-start line before the merged document (see
  [CLI surface: stdin, stdout, and file arguments](cli-surface.md) for
  the exact contract), so the output can be piped straight into another
  document. Unlike the 1.32.0 changes above, this is *not* a move toward
  spruce parity: `spruce merge` does not emit a leading `---`
  (`cmd/spruce/main.go`'s `merge` case writes bare `"%s\n"`; only
  `spruce fan` prepends `"---\n"` per document, which graft's own `fan`
  already matched). It is harmless to Genesis either way - `---` is
  YAML's document-start marker, not content, so every re-parse pattern
  above (4, 5, 6, and the `--multi-doc`/`--go-patch`/`--skip-eval`
  combinations) sees the same document whether or not the marker is
  present.

## Related documents

- [Merge semantics](merge-semantics.md)

- [YAML formatting differences](yaml-formatting.md)

- [Known gaps](known-gaps.md)
