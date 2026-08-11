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
| `--version` | `-v` | Print `<program> - Version <version>` to stdout and exit `0`. Only takes effect when no subcommand is given. |
| `--color` | | Control ANSI color output: `on`, `off`, or `auto` (default). `auto` colors output only when stderr is a terminal. An invalid value prints an error and exits `1` before any subcommand runs. |
| `--config <path>` | | Path to a YAML configuration file. See [Config flag](#--config-flag) below. |
| `--max-loop-iterations <n>` | | Iteration cap for `(( while ))` loops, default `1000`. Also settable with `GRAFT_MAX_LOOP_ITERATIONS`; the flag wins when both are given. Exceeding the cap is a hard error (exit `2`), not a truncation. |

graft reads `DEBUG`/`TRACE` directly from the process environment
(`os.Getenv`); a value counts as "set" unless it is empty, `"false"`
(case-insensitive), or `"0"`.

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
stdout followed by a single trailing newline. With no files given, `merge`
reads from stdin; passing `-` as a filename also reads stdin for that
position.

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

These are the only two `merge` flags `debug`/`merge --interactive` honor.
`--prune`/`--cherry-pick` are cleared for the session's own internal merge
steps regardless of whether they were given (they would strip data
`step`/`inspect` need mid-session); the remaining `merge` flags are not
accepted by `debug` and have no meaning for an interactive session (though
`merge --interactive --skip-eval`, for example, parses without error since
it is `merge`'s own flag set — it is simply ignored once the REPL takes
over). The one exception is `-m`/`--multi-doc` under `merge --interactive`:
input files are resolved before the REPL starts, so it really does split
each `---`-separated document into its own step.

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
the root `--color` flag (`auto` by default, colored only when stdout is a
terminal). Calling `diff` with a number of positional arguments other than
two prints usage and exits `1`, independent of the exit-code rules below.

| Flag | Description |
|---|---|
| `-y`, `--side-by-side` | Two-column view of both files' full content, aligned by a line-level diff; unchanged, added, removed, and modified rows are colored differently. |
| `-u`, `--unified` | Git-style diff, grouped by top-level key (`@@ <key> @@` headers, not numeric line ranges). |
| `--changes` | `Changes (N modified, M added, K removed):` list, grouped by kind then sorted by path. |
| `--context <n>` | Context lines around each `--unified` hunk (default `3`). |
| `--width <n>` | Total output width for `--side-by-side` (default `80`). |
| `--no-color` | Disable colorized output for this command, overriding `--color`. |
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

Merges the given files while skipping Vault resolution
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

Neither new flag changes the default (no-flag) output, which stays
byte-identical for genesis's scraping of it
(`docs/spruce/genesis-compat-contract.md`).

```bash
graft vaultinfo config.yml
graft vaultinfo --json config.yml
graft vaultinfo --paths-only config.yml
graft vaultinfo --paths-only --json config.yml
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success. For `diff`, success additionally means no differences were found. `debug` always exits `0` on a normal `quit`/`exit`/EOF, regardless of what happened during the session. |
| `1` | Usage error (no subcommand given, unknown command, invalid flag, or an invalid `--color` value); combining mutually exclusive flags (`merge`'s `--history`/`--trace-path`/`--show-changes`/`--changes-only`, or `diff`'s `--side-by-side`/`--unified`/`--changes`); or, specifically for `diff`, differences were found between the two files. |
| `2` | Runtime error: file read failure, parse failure, merge failure, cycle detected, evaluation failure, YAML marshal failure, or (for `merge --trace-path`) no history recorded for the given path. |

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
| `REDACT` | Any non-empty value sets skip-vault, skip-AWS, and skip-NATS state for the run. `vault`/`vault-try`, `nats`, and `awsparam`/`awssecret` all return the literal string `"REDACTED"` in place of the real value. |
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
