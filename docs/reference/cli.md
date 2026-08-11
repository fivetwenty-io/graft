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

### graft fan

```
graft fan [flags] source.yml [target1.yml target2.yml ...]
```

Fans a source document across one or more target documents, merging the
source into each target independently and writing each merged result to
stdout, one YAML document per target, each prefixed with a `---` separator
line. The first positional argument is always the source; every remaining
positional argument is a target. If stdin is piped in (not a terminal), it
is appended to the target list automatically. `fan` shares the same flag
set as `merge` (`--skip-eval`, `--prune`, `--cherry-pick`,
`--fallback-append`, `--go-patch`, `-m`/`--multi-doc`, `--dataflow-order`).
There is no `--output-dir` flag; each result is written to stdout in
sequence.

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

```bash
graft json config.yml
cat config.yml | graft json
graft json --strict config.yml
```

### graft diff

```
graft diff [file1] [file2]
```

Shows the semantic difference between exactly two YAML files using
[dyff](https://github.com/homeport/dyff)'s human-readable report format.
`diff` takes no subcommand-specific flags; it honors the root `--color`
flag (`auto` by default, colored only when stdout is a terminal). Calling
`diff` with a number of positional arguments other than two prints usage
and exits `1`, independent of the exit-code rules below.

```bash
graft diff before.yml after.yml
```

### graft vaultinfo

```
graft vaultinfo [flags] [files...]
```

Merges the given files while skipping Vault resolution
(`graft.WithSkipVault(true)`) and prints a YAML document listing every
Vault reference found, sorted by secret key, with each secret's referring
paths sorted underneath it:

```yaml
secrets:
  - key: secret/db:password
    references:
      - database.password
```

| Flag | Description |
|---|---|
| `--go-patch` | Same meaning as `merge --go-patch`. |

There is no `--json` or `--paths-only` flag; the output is always the YAML
shape above. To consume it as JSON (as some downstream tooling does), pipe
it into `graft json`:

```bash
graft vaultinfo config.yml | graft json
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success. For `diff`, success additionally means no differences were found. |
| `1` | Usage error (no subcommand given, unknown command, invalid flag, or an invalid `--color` value), or, specifically for `diff`, differences were found between the two files. |
| `2` | Runtime error: file read failure, parse failure, merge failure, cycle detected, evaluation failure, or YAML marshal failure. |

## stdin / stdout / stderr

- `merge`, `fan`, `json`, and `vaultinfo` all read stdin when no files are
  given, or when `-` is passed explicitly as a filename.

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
