# CLI Reference

Full command and flag reference for the `graft` binary, as implemented in
`cmd/graft/main.go`. `graft` is built on [Cobra](https://github.com/spf13/cobra)
with `SilenceUsage`/`SilenceErrors` enabled, so graft's own handlers print
errors instead of Cobra's default usage text.

## Global flags

These are persistent flags on the root `graft` command; they apply to every
subcommand.

| Flag | Short | Description |
|---|---|---|
| `--debug` | `-D` | Enable debug logging. Equivalent to setting the `DEBUG` environment variable to a non-empty, non-`false`/`0` value. |
| `--trace` | `-T` | Enable trace logging (implies `--debug`). Equivalent to setting the `TRACE` environment variable. |
| `--version` | `-v` | Print the version to stdout and exit `0`. Only takes effect when no subcommand is given. See [Version output](#version-output) below. |
| `--color` | | Force colorized output on: bare `--color`. Default is `auto` (color only when `NO_COLOR` is unset, `TERM` isn't `dumb`, and stderr is a terminal); see [Color flags](#color-flags) below. |
| `--no-color` | | Force colorized output off, overriding `--color`. Wins if both are given. |
| `--theme <name>` | | Color theme for colorized output: `auto` (default), `dark`, `light`, or `mono`. Currently applies to the debugger REPL only (`graft debug`, `graft merge --interactive`); every other command ignores it. Also settable with `GRAFT_THEME`; see [Color flags](#color-flags) below. |
| `--config <path>` | | Path to a YAML configuration file. See [Config flag](#--config-flag) below. |
| `--max-loop-iterations <n>` | | Iteration cap for `(( while ))` loops, default `1000`. Also settable with `GRAFT_MAX_LOOP_ITERATIONS`; the flag wins when both are given. Exceeding the cap is a hard error (exit `2`), not a truncation. |

graft reads `DEBUG`/`TRACE` directly from the process environment
(`os.Getenv`); a value counts as "set" unless it is empty, `"false"`
(case-insensitive), or `"0"`.

### Version output

`graft -v` (or `--version`) prints one line: the release, the commit and
timestamp it was built from, and the toolchain and platform it targets:

```
graft version 1.39.0 (commit: e6a24bc, built: 2026-08-24T22:05:31Z, go: go1.27.0, os/arch: darwin/arm64)
```

The commit and build date come from linker flags set by the release build
(`-X main.Commit`, `-X main.BuildDate`). A binary built without them falls
back to the revision and timestamp the Go toolchain embeds, suffixing the
commit with `-dirty` when the working tree was modified, and reports
`unknown` when even that is unavailable.

Invoked through a `spruce`-named symlink or copy, graft leads with the line
spruce itself prints, byte for byte, and follows it with its own:

```
spruce - Version 1.39.0
graft version 1.39.0 (commit: e6a24bc, built: 2026-08-24T22:05:31Z, go: go1.27.0, os/arch: darwin/arm64)
```

That ordering matters to Genesis, which scans the whole output for the
first `version <token>` it can find; see the
[Genesis compatibility contract](../spruce/genesis-compat-contract.md).

### Color flags

`--color`/`--no-color` apply to every subcommand, including `diff`
(`--no-color` is this same global flag, not a `diff`-specific one).
Precedence, highest first:

1. `--no-color` - forces color off, and wins outright over `--color` when
   both are given.
2. `--color` - forces color on. A bare `--color` is equivalent to
   `--color=on`; the value forms `--color=on`/`--color=off`/`--color=auto`
   (and `=true`/`=false`) are also accepted for script compatibility, but
   are otherwise undocumented.
3. Otherwise (neither flag given, or `--color=auto`): auto-detect, color
   only when `NO_COLOR` is unset or empty, `TERM` is not `dumb`, and
   stderr is a terminal.

An unrecognized `--color` value (anything other than the forms above)
prints an error and exits `1` before any subcommand runs.

`graft debug` and `graft merge --interactive` resolve color against
their own stdout writer rather than stderr, since debugger output goes
to stdout; a piped or redirected debug session gets plain output with no
`--color`-related special casing needed on the caller's part.

#### Theme flag

`--theme <name>` picks which palette color applies to once color itself
is on; it never turns color on or off by itself. Precedence is `--theme`,
then `GRAFT_THEME`, then the default `auto`. An unrecognized `--theme`
value prints an error listing the four known names and exits `1`,
mirroring an invalid `--color` value; an unrecognized `GRAFT_THEME`
prints one warning to stderr and falls through to `auto` instead of
aborting. See [Environment variables reference](environment-variables.md)
for `GRAFT_THEME` and [`graft debug`'s Colors and Themes
section](../user-guide/cli/debug.md#colors-and-themes) for the full
behavior, including theme switching with `config theme` and background
auto-detection.

| Color resolved | Theme resolved | Debugger output |
|---|---|---|
| off (any reason) | any | Plain text, zero escape bytes, no background-detection I/O performed at all. |
| on | `dark` / `light` | Full palette for that background. |
| on | `mono` | Weight, underline, and reverse-video attributes only, no color codes anywhere, including in error text. |
| on | `auto` | Background detection once at startup, before the first prompt; resolves `dark` or `light`, falling back to `dark`. |

Color-off always wins over any theme, including an explicit
`--theme dark`: a theme never forces color on. When color is off,
background detection never runs at all, so a piped session can never
emit a detection query or wait out its timeout.

### --config flag

The root command's `PersistentPreRunE` resolves a `*config.Config` on
every invocation, `--config` or not: with `--config <path>` given, it
loads and validates that YAML file (`internal/config.Load`); without it,
it starts from `config.DefaultConfig()`. Either way, the CLI then applies `GRAFT_*`
environment-variable overrides (`internal/config.ApplyEnv`) on top and
re-validates the result, so an environment variable can push an
otherwise-valid config out of range even when no config file was loaded. A missing `--config` file, an unreadable or unparseable one,
or a value that fails validation before or after the environment overrides
prints an error to stderr and exits `1` before any subcommand runs.
`GRAFT_FEATURE_*` environment overrides are resolved the same way, once
per invocation, into a `*features.FeatureFlags` set.

The resulting config and feature flags are attached to the engine built by
`merge`, `fan`, and `vaultinfo` (via `graft.WithConfigInstance` and
`graft.WithFeatureFlags`), and the config is retrievable from that engine
through `GetConfig()`. `diff` and `json` don't build an engine through
this path, so neither `--config` nor `GRAFT_*` environment variables
affect them today.

Concurrency and parallel evaluation for `merge`/`fan`/`vaultinfo` come
from this resolved config: `configEngineOpts` reads `cfg.Parallel.Enabled`
to decide whether `graft.WithParallel` is applied, and `resolveConcurrency`
derives the worker count from an explicit `cfg.Parallel.MaxWorkers` (set
via config file or `GRAFT_PARALLEL_MAX_WORKERS`) or, when that is unset,
`runtime.NumCPU()` floored at `1`. `cfg.Parallel.Enabled` defaults to
`true`, so parallel evaluation runs by default with no `--config` file and
no `GRAFT_*` variables set. Cache size still comes from the CLI's own
fixed default (`WithCache(true, 1000)`), independent of the loaded config.
Full precedence (env variable, config file, built-in default, and which
settings each one affects) is documented in
[Configuration reference](config.md).

## Commands

### graft merge

```
graft merge [flags] [files...]
```

Merges one or more YAML/JSON files (or go-patch documents) and evaluates
graft operators against the result, writing the merged YAML document to
stdout: a leading `---\n` document-start line (so the output can be piped
straight into another YAML document; suppressible with `--no-doc-start`
or `GRAFT_NO_DOC_START=1`), the merged document, then a
trailing newline (`renderMergedTree`, cmd/graft/main.go). If anything was
deferred (`--defer-on-error`/`--adaptive`, or a `--skip-vault`/
`--skip-aws`/`--skip-nats` flag), a `--report-deferred` comment block is
woven in too - see [Adaptive merge](#adaptive-merge---defer-on-error)
below; a merge that never defers anything is unaffected either way. With
no files given, `merge` reads from stdin; passing `-` as a filename also
reads stdin for that position.

| Flag | Description |
|---|---|
| `--skip-eval` | Merge documents but skip operator evaluation (`EvalPhase`/`ParamPhase` are not run). |
| `--prune <key>` | Remove `<key>` from the final output. Repeatable. |
| `--cherry-pick <key>` | Output only `<key>` (and its descendants) from the final output. Repeatable; the inverse of `--prune`. |
| `--max-loop-iterations <n>` | Global flag; see [Global flags](#global-flags). |
| `--fallback-append` | Use append semantics instead of inline (key-then-index) semantics for the default array-merge fallback. |
| `--go-patch` | Parse array-rooted documents as [go-patch](https://github.com/cppforlife/go-patch) operations instead of erroring on a non-map document root. |
| `-m`, `--multi-doc` | Treat each `---`-separated document within a file as a separate input document rather than parsing the file as one document. |
| `--dataflow-order` | Key ordering for the merged output: `alphabetical` (default when unset) or `insertion`. |
| `--history` | Print per-path merge history instead of the merged document. See [History/tracing flags](#history-tracing-flags) below. |
| `--trace-path <path>` | Print detailed history for a single path instead of the merged document. |
| `--show-changes` | Print a merge/evaluation change summary instead of the merged document. |
| `--changes-only` | Print only the paths that changed during merge/evaluation instead of the merged document. |
| `--interactive` | Launch the interactive debug REPL instead of merging directly; equivalent to `graft debug <files...>`. See [graft debug](#graft-debug) below. |
| `--skip-vault` | Defer `(( vault ... ))`/`(( vault-try ... ))` calls instead of contacting Vault: each leaves its own expression intact in the output (e.g. `(( vault "secret/db:password" ))`), so the document can be merged again once Vault is reachable. Covers OpenBao too (same API, same operator). Composable with `--skip-aws`/`--skip-nats`. `REDACT=1` is unaffected and keeps returning `"REDACTED"` regardless of this flag. |
| `--skip-aws` | Same defer behavior as `--skip-vault`, for `(( awsparam ... ))`/`(( awssecret ... ))`. |
| `--skip-nats` | Same defer behavior as `--skip-vault`, for `(( nats ... ))`. |
| `--defer-on-error` | Adaptive merge: on an operator failure, defer that expression (and any dependent path a later retry round reveals) and re-merge, instead of failing the whole merge. See [Adaptive merge](#adaptive-merge---defer-on-error) below. |
| `--adaptive` | Alias for `--defer-on-error`. |
| `--report-deferred <placement>` | Where to report deferred keys (from `--defer-on-error`/`--adaptive` or `--skip-vault`/`--skip-aws`/`--skip-nats`) as YAML comments in the output: `beginning` (default), `inline`, `end`, or `none`. See [Adaptive merge](#adaptive-merge---defer-on-error). |
| `--no-doc-start` | Do not prepend the leading `---\n` document-start line to the merged output, for consumers that concatenate merge output into a stream where a `---` line would open an unwanted second document. Also settable as `GRAFT_NO_DOC_START` (`true`/`1`/`yes`/`on` suppress the marker; `false`/`0`/`no`/`off` or anything unrecognized keep it) for callers that invoke graft with a fixed flag set; an explicitly given flag wins over the environment in both directions, so `--no-doc-start=false` keeps the marker even with the variable set. Merge-only: `fan`'s per-document `---` matches spruce and is unaffected. |

A value composed from a deferred call - a `(( grab ))` of a field that
itself deferred, or a vault path segment built from another deferred
vault lookup - defers transitively too, since the deferred call's own
document value is just its own expression text, copied like any other
string.

A `--skip-<backend>` flag exits `3` (a "successful partial merge")
*only* when it actually deferred something - a document with nothing
for that backend to skip merges cleanly and exits `0`, exactly as
without the flag. The same is true of `--defer-on-error`/`--adaptive`:
`0` if nothing ever failed, `3` if anything was deferred, in any
combination of these flags. `REDACT=1` never produces exit `3`: it
redacts (returns the literal `"REDACTED"` string) rather than deferring,
regardless of which of these flags are also given, so it always exits
`0` on success like a plain merge. See [Exit codes](#exit-codes) below.

Every merge internally runs with caching enabled
(`graft.WithCache(true, 1000)`); cache size isn't currently exposed as a
CLI flag. Concurrency and parallel evaluation come from the resolved
config instead of a fixed value; see [Config flag](#--config-flag) above.

```bash
graft merge base.yml overlay.yml > result.yml
graft merge --skip-eval base.yml overlay.yml
graft merge --prune meta --prune internal base.yml overlay.yml
graft merge --cherry-pick database --cherry-pick server base.yml overlay.yml
cat base.yml | graft merge - overlay.yml
```

A path segment given to `--prune` or `--cherry-pick` may be a `field=value`
list predicate, which selects the first list entry whose `field` equals
`value`:

```bash
graft merge --cherry-pick 'servers.name=primary' inventory.yml
graft merge --prune 'servers.name=primary' inventory.yml
```

This is a graft extension — spruce rejects predicate segments in either flag.
Only the dotted spelling is accepted here. The bracketed spelling
(`servers[name=primary]`) works in expressions but not in these flags:
`--cherry-pick` reports `validation_error: key not found`, and `--prune`
silently matches nothing.

#### History/tracing flags

`--history`, `--trace-path`, `--show-changes`, and `--changes-only` are
mutually exclusive: at most one may be given (combining two exits `1` with
"mutually exclusive; pick one"). Each replaces the merged-document output
with a report of how the final document's values were derived, built by
re-running the merge one file at a time (raw, then fully evaluated, then
post-processed if `--prune`/`--cherry-pick` were also given) and diffing
each step against the last (`internal/history`, itself built on
`internal/histdiff` — the same dyff-backed comparison `graft diff` uses).
Every entry is attributed to a *file*, not a file:line — graft does not
carry source line numbers through its merge pipeline.

```bash
graft merge --history base.yml env.yml
```

```
Merge History:

database.host:
  [0] base.yml       → localhost
  [1] env.yml        → db.prod.example.com
  Final              → db.prod.example.com

database.port:
  [0] base.yml       → 5432
  Final              → 5432  (unchanged)
```

`--trace-path <path>` prints the same per-file entries for one path, each
annotated with a `Type:` line (`operator (<name>)` when that entry's value
is still an unevaluated `(( ... ))` expression, otherwise `value`). An
unrecorded path exits `2` with "No history found for path".

`--show-changes` prints a `Merge Summary: N files → M keys (...)` header
followed by every added/changed/removed path (paths that never changed
after the first file are omitted), each entry marked `✗` (overwritten),
`✓` (final value used), or `+`/`-` (added/removed).

`--changes-only` prints a compact `Changed paths (X of M):` list, one line
per added/changed/removed path, `<old> → <new>` (`<none>` for an added
path's old side, `<removed>` for a removed path's new side).

#### Adaptive merge (`--defer-on-error`)

`--defer-on-error` (alias `--adaptive`) merges everything it can instead
of failing on the first operator error: on a failure, it wraps that
expression in `(( defer ... ))` and re-merges, repeating until the merge
succeeds (cleanly, or partially with every deferred expression intact)
or no further path can be deferred - at which point the original error
is reported and `merge` exits normally (`2`, or `1` for a usage error),
exactly as it would without this flag. A genuine operator-dependency
cycle is always this kind of hard failure: it has no path to defer, so
it is reported immediately.

A `(( grab ))` of a deferred value copies its still-unevaluated
expression text cleanly, so it defers transitively too, with no comment
of its own (see `--report-deferred` below - only the path with the
actual failure is reported). A value-transforming operator like
`(( concat ))` that reads a deferred value does not itself fail (there
is nothing to defer), but embeds the deferred text into a larger string
that is no longer itself a re-evaluable expression on a later merge -
a limitation inherent to `(( defer ... ))` itself (shared by `graft
debug`'s own manual defer-and-retry), not something this flag corrects.

`--defer-on-error` cannot be combined with `--history`/`--trace-path`/
`--show-changes`/`--changes-only` (exit `1`); given with nothing that
ever fails, it is byte-identical to a plain merge, including exit code
`0`.

`--report-deferred <placement>` controls how deferred keys - from
`--defer-on-error`/`--adaptive`, or from a `--skip-vault`/`--skip-aws`/
`--skip-nats` flag - are reported, in-band as YAML comments so the
report travels with the document and the output stays valid, re-mergeable
YAML. Comments are placed after the leading `---` document-start line;
the parser ignores them entirely on a later merge.

- `beginning` (default): a summary block at the top, one line per
  deferred key with its original error (or, for a `--skip-<backend>`
  deferral, a `skipped (--skip-<backend>)` reason):

  ```yaml
  ---
  # graft: 2 keys deferred
  # graft: deferred $.meta.cert: vault "secret/certs:pem": connection refused
  # graft: deferred $.props.password: vault "secret/db:pass": connection refused
  meta:
    cert: (( vault "secret/certs:pem" ))
  props:
    password: (( vault "secret/db:pass" ))
  ```

- `inline`: the comment is attached directly above each deferred key
  instead, with no path (its position already conveys it):

  ```yaml
  props:
    # graft: deferred: vault "secret/db:pass": connection refused
    password: (( vault "secret/db:pass" ))
  ```

- `end`: the same summary block as `beginning`, appended after the
  document instead.

- `none`: no comments at all. Exit code `3` (see [Exit codes](#exit-codes))
  still distinguishes a partial merge from a clean one, so a caller that
  wants the deferred-key report fully silenced can still tell the two
  apart programmatically.

An invalid `--report-deferred` value exits `1` with a clear error,
independent of whether the merge would have deferred anything at all.

```bash
graft merge --defer-on-error base.yml overlay.yml
graft merge --adaptive --report-deferred=inline base.yml overlay.yml
graft merge --defer-on-error --report-deferred=none base.yml overlay.yml
```

### graft debug

```
graft debug [flags] [files...]
```

Also reachable as `graft merge --interactive <files...>`. Launches an
interactive REPL for stepping through a merge one file at a time,
inspecting intermediate values, and controlling evaluation
(`docs/user-guide/cli/debug.md` has the full command reference and a
worked session transcript). Files are read once at `load` time; the REPL
itself reads commands from stdin, one per line, until `quit`/`exit` or
EOF — it never reads a merge document from stdin.

| Flag | Description |
|---|---|
| `--go-patch` | Same meaning as `merge --go-patch`; applied to every merge the session performs. |
| `--fallback-append` | Same meaning as `merge --fallback-append`; applied to every merge the session performs. |
| `--prune <key>` | Same meaning as `merge --prune`; may be given more than once. Not applied to the session's own `step`/`output`/`export`/`history` (they always show the pre-prune tree); see `prune-report`. |
| `--cherry-pick <key>` | Same meaning as `merge --cherry-pick`; may be given more than once. Same `--prune` caveat. |

These are the only `merge` flags `debug`/`merge --interactive` honor. The
remaining `merge` flags are not accepted by `debug` and have no meaning
for an interactive session (though `merge --interactive --skip-eval`, for
example, parses without error since it is `merge`'s own flag set — it is
simply ignored once the REPL takes over). The one exception is
`-m`/`--multi-doc` under `merge --interactive`: input files are resolved
before the REPL starts, so it really does split each `---`-separated
document into its own step.

The REPL's own `prune-report` command reports what `--prune`/`--cherry-pick`
would remove once the session is fully evaluated, without applying it, and
`autodefer` runs the same defer-on-error retry loop `merge --defer-on-error`/
`--adaptive` uses against the session's current tree — see
`docs/user-guide/cli/debug.md` for both.

```bash
graft debug base.yml overlay.yml
graft debug --go-patch base.yml overlay.yml
```

### graft fan

```
graft fan [flags] source.yml [target1.yml target2.yml ...]
```

Fans a source document across one or more target documents, merging the
source into each target independently. By default each merged result is
written to stdout, one YAML document per target, each prefixed with a
`---` separator line; with `-o`/`--output-dir <dir>`, each result is
written to `<dir>/<target-basename>` instead (target files inside a
directory passed as a positional argument are expanded to their sorted
`.yml`/`.yaml`/`.json` entries first). The first positional argument is
always the source; every remaining positional argument is a target.

Stdin is only ever consulted when **fewer than two** file arguments were
given (i.e. a source with no explicit target at all — `cat targets.yml |
graft fan source.yml`, mirroring `cat x | graft merge`'s own
read-from-stdin convention): a piped-in document then becomes the target
list. Once a source *and* at least one explicit target file are both
given, stdin is left alone — piping data into `fan` alongside two or more
file arguments no longer adds a further implicit target (this fixes a
hang: it previously read a non-terminal stdin unconditionally, blocking
forever on an open pipe with no writer). An explicit `-` argument, in
either position, always works as a stdin source regardless of this rule,
since it is already present in the argument list.

`fan` shares the same flag set as `merge` (`--skip-eval`, `--prune`,
`--cherry-pick`, `--fallback-append`, `--go-patch`, `-m`/`--multi-doc`,
`--dataflow-order`), plus `-o`/`--output-dir`.

```bash
graft fan base.yml env/dev.yml env/staging.yml env/prod.yml
```

### graft json

```
graft json [flags] [files...]
```

Converts YAML input to JSON, one JSON object per line. A file containing
multiple `---`-separated YAML documents produces one JSON line per
document. With no files given, `json` reads from stdin.

| Flag | Description |
|---|---|
| `--strict` | Refuse to convert non-string map keys to strings; error instead. |
| `-r`, `--reverse` | Convert JSON to YAML instead of YAML to JSON. Accepts either one JSON document (compact or pretty-printed) or newline-delimited JSON documents (the shape `graft json`'s own default output takes) per file/stdin; multiple documents in one input print as `---`-separated YAML documents. |
| `--multi-doc` | Forward direction only: wrap every JSON line into a single pretty-printed JSON array instead of one object per line. |

```bash
graft json config.yml
cat config.yml | graft json
graft json --strict config.yml
graft json --reverse config.json
echo '{"key": "value"}' | graft json -r
graft json config.yml | graft json --reverse   # round-trip
```

### graft diff

```
graft diff [flags] [file1] [file2]
```

Shows the semantic difference between exactly two YAML files, built on
[dyff](https://github.com/homeport/dyff). With no format flag, this is
dyff's own human-readable report; `--changes`, `--unified`, and
`--side-by-side` select an alternate rendering of the same underlying
comparison instead (`internal/histdiff.Compare`, also what `merge
--history`/`--show-changes`/`--changes-only` are built on). `diff` honors
the root `--color`/`--no-color` flags (`auto` by default, colored only
when stderr is a terminal; see [Color flags](#color-flags) above). Calling
`diff` with a number of positional arguments other than two prints usage
and exits `1`, independent of the exit-code rules below.

| Flag | Description |
|---|---|
| `-y`, `--side-by-side` | Two-column view of both files' full content, aligned by a line-level diff; unchanged, added, removed, and modified rows are colored differently. |
| `-u`, `--unified` | Git-style diff, grouped by top-level key (`@@ <key> @@` headers, not numeric line ranges). |
| `--changes` | `Changes (N modified, M added, K removed):` list, grouped by kind then sorted by path. |
| `--context <n>` | Context lines around each `--unified` hunk (default `3`). |
| `--width <n>` | Total output width for `--side-by-side` (default `80`). |
| `-q`, `--quiet` | Suppress output; only the exit code is meaningful. |

At most one of `--side-by-side`/`--unified`/`--changes` may be given;
combining two exits `1` with "mutually exclusive; pick one". The exit-code
rules below (`0` identical, `1` differences found, `2` error) apply to
every format, including `--quiet`.

```bash
graft diff before.yml after.yml
graft diff --changes before.yml after.yml
graft diff --unified --context=5 before.yml after.yml
graft diff --side-by-side --width=160 before.yml after.yml
graft diff --quiet before.yml after.yml; echo $?
```

### graft vaultinfo

```
graft vaultinfo [flags] [files...]
```

Merges the given files while skipping Vault resolution by default
(`graft.WithSkipVault(true)`) and prints every Vault reference found,
sorted by secret key, with each secret's referring paths sorted
underneath it. The default output is YAML:

```yaml
secrets:
  - key: secret/db:password
    references:
      - database.password
```

| Flag | Description |
|---|---|
| `--go-patch` | Same meaning as `merge --go-patch`. |
| `--json` | Same `secrets`/`key`/`references` shape as the default, as indented JSON, instead of YAML. |
| `--paths-only` | Only the secret keys (not their referring locations), one per line — or, combined with `--json`, a JSON array of the same keys. |
| `--resolve` | Perform real Vault lookups instead of skipping them (requires a reachable Vault). |

None of these flags change the default (no-flag, no-`--resolve`) output
for a fixture with no vault-from-vault path composition, which stays
byte-identical for genesis's scraping of it
(`docs/spruce/genesis-compat-contract.md`). A path segment built from
another `(( vault ... ))` lookup is the one exception: without
`--resolve`, it renders as a symbolic `<path/to/secret:key>` reference
rather than the corrupted, literal `secret/paths:REDACTED` graft used to
report (see [vaultinfo](../user-guide/cli/vaultinfo.md#composed-paths-and---resolve)
for the full example); with `--resolve`, it reports the concrete
resolved path instead.

```bash
graft vaultinfo config.yml
graft vaultinfo --json config.yml
graft vaultinfo --paths-only config.yml
graft vaultinfo --paths-only --json config.yml
graft vaultinfo --resolve config.yml
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success, and (for `merge`) nothing was deferred. For `diff`, success additionally means no differences were found. `debug` always exits `0` on a normal `quit`/`exit`/EOF, regardless of what happened during the session. |
| `1` | Usage error (no subcommand given, unknown command, invalid flag, an invalid `--color` value, or an invalid `--report-deferred` value); combining mutually exclusive flags (`merge`'s `--history`/`--trace-path`/`--show-changes`/`--changes-only`, `merge --defer-on-error` with any of those four, or `diff`'s `--side-by-side`/`--unified`/`--changes`); or, specifically for `diff`, differences were found between the two files. |
| `2` | Runtime error: file read failure, parse failure, merge failure, cycle detected, evaluation failure, YAML marshal failure, or (for `merge --trace-path`) no history recorded for the given path. |
| `3` | `merge` only: a successful *partial* merge - at least one path was deferred, from `--defer-on-error`/`--adaptive` or a `--skip-vault`/`--skip-aws`/`--skip-nats` flag. See [Adaptive merge](#adaptive-merge---defer-on-error). |

## stdin / stdout / stderr

- `merge`, `json`, and `vaultinfo` read stdin when no files are given, or
  when `-` is passed explicitly as a filename. `fan` follows the same
  convention counted against its *targets* rather than its arguments: it
  reads stdin when no target is given (a bare `graft fan`, or a source
  with no target file), or when `-` is passed explicitly. `debug` never
  reads a merge document from stdin — its files are always positional
  arguments — but reads REPL commands from stdin, one per line.

- Merged/converted output is written to stdout.

- All errors, debug (`-D`/`--debug`), and trace (`-T`/`--trace`) logging go
  to stderr, via graft's `log` package.

## Environment variables

| Variable | Effect |
|---|---|
| `DEBUG` | Same as `--debug`, if set to a truthy value (anything but empty, `"false"`, or `"0"`). |
| `TRACE` | Same as `--trace`, if set to a truthy value. Also implies `DEBUG`-level logging. |
| `REDACT` | Any non-empty value sets skip-vault, skip-AWS, and skip-NATS state for the run, in "redact" mode: `vault`/`vault-try`, `nats`, and `awsparam`/`awssecret` all return the literal string `"REDACTED"` in place of the real value. This differs from the `--skip-vault`/`--skip-aws`/`--skip-nats` flags, whose own default is "defer" (leave the expression intact) rather than redact; `REDACT=1` always wins over those flags when both are active. |
| `DEFAULT_ARRAY_MERGE_KEY` | Overrides the identifier key (`name` by default) used for by-key array merges in `pkg/graft/merger`. |

`internal/config`'s `GRAFT_*` environment variables (engine strict mode,
cache size/TTL, parallel worker bounds, metrics, logging level, and so on)
are applied automatically on every `graft` invocation, `--config` or not,
as described in [Config flag](#--config-flag) above; they affect
`merge`, `fan`, and `vaultinfo`, not `diff` or `json`. Full variable-by-
variable reference and precedence rules are in
[Configuration reference](config.md).

## Related documentation

- [Operator reference](operators.md)

  Every operator evaluated during `merge`/`fan`, with syntax and error behavior.

- [Engine overview](../architecture/engine-overview.md)

  How the CLI's `mergeAllDocs` constructs and runs the engine underneath these commands.
