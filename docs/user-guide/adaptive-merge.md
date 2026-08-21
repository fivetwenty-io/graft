# Adaptive Merge

`graft merge --defer-on-error` (alias `--adaptive`) merges everything it
can instead of failing on the first operator error: a failing expression
is deferred - left intact in the output, exactly as if wrapped in
`(( defer ... ))` - and the merge is retried, so a document that mostly
merges cleanly does not fail outright just because one secret backend or
one reference is unavailable right now.

## Overview

A plain merge fails the whole document on the first operator error, even
when only one value - a Vault secret, an AWS parameter, an unresolved
reference - is the actual problem. Adaptive merge instead:

1. Runs the merge normally. On success, nothing else happens.

2. On failure, wraps every newly-failing expression in `(( defer ... ))`
   and re-merges. Repeats until the merge succeeds (cleanly, or partially
   with every deferred expression intact) or no further path can be
   deferred.

3. A true hard failure - most commonly a genuine operator-dependency
   cycle, which has no path to defer - stops the loop immediately and
   reports the original error, exactly as a plain merge would.

The output of a partial merge is valid YAML with every deferred
expression still intact, so it can be merged again later, once whatever
was unavailable is reachable, and evaluates normally at that point.

## Enabling Adaptive Merge

```sh
graft merge --defer-on-error base.yml secrets.yml
```

**secrets.yml:**

```yaml
database:
  password: (( vault "secret/db:password" ))
  host: db.prod.example.com
```

If Vault is unreachable, a plain merge fails outright. Under
`--defer-on-error`, the merge still succeeds, with `database.password`
left as its own expression:

```yaml
---
# graft: 1 key deferred
# graft: deferred $.database.password: Error during Vault client initialization: ...
database:
  host: db.prod.example.com
  password: (( vault "secret/db:password" ))
```

The exit code is `3`, not `0`, whenever anything was deferred - a
"successful partial merge," distinct from both a clean merge (`0`) and a
hard failure (the usual nonzero codes, unchanged). See
[CLI Reference: Exit codes](../reference/cli.md#exit-codes).

Merging the output above again, once Vault is reachable, evaluates
`database.password` normally - the `# graft: ...` comments are ordinary
YAML comments, ignored by the parser.

## Cascades and Dependents

A `(( grab ))` of a deferred value copies its still-unevaluated
expression text, so it defers transitively too, with no error or comment
of its own:

```yaml
meta:
  password: (( vault "secret/db:password" ))
database:
  connection: (( grab meta.password ))
```

```yaml
---
# graft: 1 key deferred
# graft: deferred $.meta.password: Error during Vault client initialization: ...
database:
  connection: (( vault "secret/db:password" ))
meta:
  password: (( vault "secret/db:password" ))
```

Only `meta.password` - the actual root cause - is reported;
`database.connection` never produced an error of its own, so it is not
listed separately, even though its value also stayed deferred.

**Known limitation:** a value-*transforming* operator that reads a
deferred value, such as `(( concat ))`, does not itself fail (concat has
nothing to defer), but the deferred text ends up embedded inside a
larger string:

```yaml
database:
  url: (( concat "postgres://" meta.password ))
```

```yaml
database:
  url: postgres://(( vault "secret/db:password" ))
```

That result is valid YAML, but it is no longer a re-evaluable graft
expression on its own (the `((` no longer starts the string). This is a
limitation of the `(( defer ... ))` primitive itself, shared by `graft
debug`'s own manual defer-and-retry - not something adaptive merge
corrects.

## Reporting Deferred Keys

`--report-deferred=<placement>` controls how deferred keys are reported,
in-band as YAML comments woven into the output itself, so the report
travels with the document and the output stays valid, re-mergeable YAML.
It also covers `graft merge --skip-vault`/`--skip-aws`/`--skip-nats`
deferrals (see [Vault Integration: Merging Without Vault
Access](secrets/vault.md#merging-without-vault-access)), not just
`--defer-on-error`'s own.

| Placement | Behavior |
|---|---|
| `beginning` (default) | A summary block at the top of the output. |
| `inline` | A comment directly above each deferred key, no path (its position conveys it). |
| `end` | The same summary block as `beginning`, appended after the document instead. |
| `none` | No comments at all. Exit code `3` still distinguishes a partial merge from a clean one. |

```sh
graft merge --defer-on-error --report-deferred=inline base.yml secrets.yml
```

```yaml
---
database:
  host: db.prod.example.com
  # graft: deferred: Error during Vault client initialization: ...
  password: (( vault "secret/db:password" ))
```

```sh
graft merge --defer-on-error --report-deferred=none base.yml secrets.yml
```

```yaml
---
database:
  host: db.prod.example.com
  password: (( vault "secret/db:password" ))
```

An invalid `--report-deferred` value is a usage error (exit `1`),
independent of whether the merge would have deferred anything at all.

## In the Debugger

`graft debug` has the same defer-on-error loop as its own REPL command,
`autodefer` - useful for stepping up to the point of a failure, then
letting the debugger clear it instead of retrying the whole merge from the
command line. See [debug Command: autodefer](cli/debug.md#autodefer) for
the full walkthrough; the short version:

```
graft> continue
[2/2] Evaluating operators...
[Merge failed reported inline, or the value stays an unresolved expression]

graft> autodefer
Autodefer: 1 key deferred:
  deferred $.database.password: Error during Vault client initialization: ...

graft> output
database:
  host: db.prod.example.com
  password: (( vault "secret/db:password" ))
```

`output`/`export`/`history`/`inspect` all agree afterward: the deferred
expression is exactly what a CLI `--defer-on-error` merge would have
produced, without the `--report-deferred` comment block (the REPL prints
the same summary as plain text at the `autodefer` prompt instead).

## Compatibility

`--defer-on-error` is strictly opt-in: a plain `graft merge` (the
default) is completely unaffected, and a `--defer-on-error` merge that
never actually defers anything produces byte-identical output to a plain
merge, including exit code `0`. Default-mode error text (the
`N error(s) detected:\n - $.path: msg` format Genesis's own error
scraping depends on) is unaffected either way - see
[Genesis Compatibility Contract](../spruce/genesis-compat-contract.md).

`--defer-on-error` cannot be combined with `--history`/`--trace-path`/
`--show-changes`/`--changes-only` (see
[History Tracking](history-tracking.md)): each selects a different report
format over the merge, and the two are not composable.

## See Also

- [merge Command](cli/merge.md) - Full flag reference
- [debug Command](cli/debug.md) - The interactive REPL's `autodefer` command
- [Vault Integration: Merging Without Vault Access](secrets/vault.md#merging-without-vault-access) - The related `--skip-vault`/`--skip-aws`/`--skip-nats` flags
- [History Tracking](history-tracking.md) - `--history`/`--trace-path`/`--show-changes`/`--changes-only`
