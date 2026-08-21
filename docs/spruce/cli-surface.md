# CLI surface: graft vs. spruce

This page compares graft's command-line interface to spruce's, flag by flag,
exit code by exit code, and environment variable by environment variable. It
is the reference for anyone wiring graft into a pipeline (such as Genesis)
that currently shells out to spruce.

Both binaries are built from a nearly identical `main.go` command layer;
spruce parses flags with `goptions`, graft parses them with `cobra`. The
practical behavior lines up closely, with the differences called out below.

## Global flags

| Flag | spruce | graft | Notes |
|---|---|---|---|
| `-D`, `--debug` | Yes | Yes | Same effect: enables debug logging. |
| `-T`, `--trace` | Yes | Yes | Same effect: enables trace logging and implicitly turns on debug logging. |
| `-v`, `--version` | Yes | Yes | Same output format on both: `<argv0> - Version <version>`, printed to stdout, exit code 0. |
| `--color`, `--no-color` | No | Yes | graft-only. spruce always auto-detects color via a TTY check with no override flag; graft adds explicit `--color`/`--no-color` persistent flags (default: neither given, which behaves like spruce's TTY check - see [CLI Reference: Color flags](../reference/cli.md#color-flags)). |
| `-h`, `--help` | Yes, per subcommand | Yes, built into cobra | spruce defines `--help`/`-h` per subcommand and prints usage + exits 1 on request or parse failure. graft gets equivalent help output for free from cobra; an unrecognized command or bad flags still exits 1 via graft's own `usage()` fallback. |
| `--config <path>` | No | Yes | graft-only. A persistent root flag pointing at a YAML configuration file (`internal/config`). Absent, graft behaves exactly like the pre-config default. When present, the file is loaded as the base configuration and `GRAFT_*` environment variables are layered on top; see [Configuration reference](../reference/config.md) for the full precedence order and which settings actually change merge behavior. |

## Subcommands

| Command | spruce flags | graft flags | Notes |
|---|---|---|---|
| `merge [files...]` | `--skip-eval`, `--prune` (repeatable), `--cherry-pick` (repeatable), `--fallback-append`, `--go-patch`, `-m`/`--multi-doc` | Same five, plus `--dataflow-order <alphabetical\|insertion>` | `--dataflow-order` is graft-only; it controls the order operators run in when there is no dependency constraint between them, defaulting to alphabetical. |
| `fan [files...]` | Same flags as `merge` | Same flags as `merge`, including `--dataflow-order` | Behavior matches: first file is the source document, the rest are targets; each target is merged against the source independently, and results are written as separate `---`-separated YAML documents. |
| `json [files...]` | `--strict` | `--strict` | Same behavior: converts YAML to JSON without running the merge/eval engine. `--strict` rejects non-string map keys instead of silently stringifying and dropping collisions. |
| `diff [file1] [file2]` | No subcommand-specific flags (root `--color`-equivalent is implicit) | No subcommand-specific flags (uses root `--color`) | Both require exactly two file arguments and both are backed by the `dyff` library for the actual comparison and report rendering. |
| `vaultinfo [files...]` | `--go-patch` | `--go-patch` | Both run a full merge/eval pass with vault resolution suppressed, then print the collected vault references as YAML. |

## Exit codes

Both binaries use the same three-way scheme:

| Code | Meaning | spruce | graft |
|---|---|---|---|
| 0 | Success | Yes | Yes |
| 1 | Usage error, missing/invalid arguments, unrecognized command, or `diff` found differences | Yes | Yes |
| 2 | Runtime error during merge, evaluation, JSON conversion, or vault-info collection | Yes | Yes |

`diff` on two files with no semantic differences returns 0; if it finds
differences, it returns 1 after printing the human-readable report to
stdout. A malformed or unreadable input file for `diff` returns 2 instead.

## Environment variables

| Variable | spruce | graft | Notes |
|---|---|---|---|
| `DEBUG` | Enables debug logging (same truthy rule: anything except empty string, `false`, or `0`, case-insensitive) | Same | Identical semantics, read directly by the CLI. |
| `TRACE` | Enables trace + debug logging, same truthy rule | Same | Identical semantics. |
| `REDACT` | Any non-empty value forces both vault and AWS lookups to return the literal string `REDACTED` | Same, and additionally forces NATS lookups to return `REDACTED` | `pkg/graft/engine.go`'s `DefaultEngine.evaluate` checks `REDACT` unconditionally and calls `SetSkipVault`, `SetSkipAws`, and `SetSkipNats` — a superset of spruce's vault+AWS-only redaction, since spruce has no NATS operator. This is the production merge path the CLI actually calls; the legacy `Evaluator.Run` method carries an equivalent check too, but `DefaultEngine.evaluate` is what runs on every `merge`, `fan`, and `vaultinfo` invocation. |
| `DEFAULT_ARRAY_MERGE_KEY` | Overrides the default array-merge identifier key (`name`) for every merge | Same | The CLI's active merge path constructs a `pkg/graft/merger.Merger` for every merge, and that merger's key-merge logic (`getDefaultIdentifierKey`) reads `DEFAULT_ARRAY_MERGE_KEY` first, falling back to `name` only when the variable is unset. Named-array-entry path lookups (used by, for example, `--prune path.to.name`) also honor the same configured key, with `name`, `id`, and `key` kept as fallbacks for entries that don't use it. |
| `SPRUCE_FILE_BASE_PATH` | Base path prefix for relative paths passed to `(( file ))` and `(( load ))` | Same, as a fallback | graft checks `GRAFT_FILE_BASE_PATH` first; if that is unset or empty, it reads `SPRUCE_FILE_BASE_PATH` instead, so a pipeline already exporting the spruce-named variable keeps working unchanged when it switches to graft. Either variable is only applied to relative paths; an absolute path is used as-is. |
| `GRAFT_FILE_BASE_PATH` | N/A (spruce has no graft-named equivalent) | Base path prefix for relative paths passed to `(( file ))` and `(( load ))` | This is graft's own equivalent of `SPRUCE_FILE_BASE_PATH`, under a different name, and takes priority over it when both are set. |
| `VAULT_ADDR`, `VAULT_TOKEN`, `VAULT_NAMESPACE`, `VAULT_SKIP_VERIFY` | Read directly for the `(( vault ))` operator's backend client | Same four variables, read the same way in graft's vault backend client | Matching semantics; `VAULT_SKIP_VERIFY` is treated as truthy for `"true"` or `"1"` in graft. |
| `~/.vault-token` file fallback | Used when `VAULT_TOKEN` is unset | Same fallback path | Both read `$HOME/.vault-token` if the token is not supplied via environment variable. |
| `~/.svtoken` file fallback | Used for vault/token/namespace/skip-verify when other sources are absent | Not found in graft's vault backend client | graft's vault client does not read a `~/.svtoken`-style config file. |
| `AWS_PROFILE`, `AWS_REGION`, `AWS_ROLE` | Read for the `(( awsparam ))`/`(( awssecret ))` operators | Same three variables, read the same way | Matching semantics for AWS SDK session/role setup. |
| 16 `GRAFT_*` config variables (`GRAFT_ENGINE_STRICT_MODE`, `GRAFT_CACHE_ENABLED`, `GRAFT_PARALLEL_ENABLED`, and so on) | N/A | Read on every CLI invocation | The `graft` binary resolves an effective configuration before every `merge`, `fan`, or `vaultinfo` run: an explicit `--config` file (or `internal/config.DefaultConfig()` if `--config` was not given) as the base, then `internal/config.ApplyEnv` layers these 16 variables on top. Every variable is parsed and validated on every run, but not every one changes observable merge behavior yet; the `Parallel` section and the `caching` feature flag do, most other sections don't yet. See [Configuration reference](../reference/config.md) for the full list and current wiring status. |

## stdin, stdout, and file arguments

Both CLIs share the same conventions: pass `-` as a filename (or omit files
entirely when stdin is piped) to read YAML from stdin, and both write merged
YAML to stdout with a single trailing newline appended after whatever the
YAML marshaler produces. Errors go to stderr in both, formatted with the same
`@R{...}`/`@c{...}`/`@m{...}` ANSI color-tag convention (color enabled only
on a real TTY, or forced by graft's `--color`).

## Related pages

- [Parity overview](README.md)
- [Operator inventory](operators.md)
- [Merge semantics](merge-semantics.md)
- [YAML formatting](yaml-formatting.md)
- [Known gaps](known-gaps.md)
- [Genesis compatibility contract](genesis-compat-contract.md)
