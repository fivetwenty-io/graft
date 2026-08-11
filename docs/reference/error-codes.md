# Error Codes and Troubleshooting

This reference covers graft's error model, its opt-in error-code system, and debugging tools.

## Error model

Every error graft raises internally is a plain Go `error`. Two additional types layer stable, machine-readable classification on top, without changing any error's message text:

- `graft.GraftError` (`pkg/graft/errors.go`): `{Type ErrorType, Message, Path string, Cause error}`. `ErrorType` is a string enum: `parse_error`, `merge_error`, `evaluation_error`, `operator_error`, `configuration_error`, `validation_error`, `external_error`. Constructed by the library API (`Document`, `MergeBuilder`, `Engine.ParseYAML`/`ParseJSON`) for structural and configuration failures.

- `graft.PathError` (`pkg/graft/errors.go`): `{Path string, Cause error}`. Wraps every per-operator evaluation failure with the document path where it occurred. `Error()` returns `"$.<path>: <cause>"` — this is the line format spruce and genesis have always used, and is exactly what appears (one per line, `- ` prefixed) in a `MultiError`'s `"N error(s) detected:"` block.

Both types implement `Code() ErrorCode`, and `graft.ClassifyError(err error) ErrorCode` classifies any error, including plain ones never explicitly tagged, by unwrapping through `errors.As`/`errors.Is` and matching a fixed set of known types and message patterns. `ErrorCode` is a string like `"E201"`; the empty string means "no assigned code" (not itself an error).

## CLI stderr output does not change by default

Merge and evaluation errors print as `- $.<path>: <message>` lines inside a `"N error(s) detected:"` block — this is spruce's format, and genesis's `ManifestProvider.pm` parses it with the regex `^\s*-\s*\$\.(\S+?):`. That format, the `secret <key> not found` wording, warning formatting, and exit codes are unchanged and byte-identical to previous graft releases; nothing below alters default output.

## Opt-in: `GRAFT_ERROR_CODES`

Set `GRAFT_ERROR_CODES=1` (also accepts `true`, `yes`, `on`, case-insensitive) to have `MultiError`'s per-path lines gain a `[Ecode]` tag on the message segment, after the `$.path: ` prefix genesis's regex depends on:

Given `jobs.web.instances: (( concat "only-one-arg" ))`, `graft merge` produces:

```bash
# default
 - $.jobs.web.instances: concat operator requires at least two arguments

# GRAFT_ERROR_CODES=1
 - $.jobs.web.instances: [E206] concat operator requires at least two arguments
```

(both captured verbatim from `graft merge`; the pair differs by exactly the `[E206] ` tag)

The tag is only added when the error resolves to a `*PathError` with a non-empty `Code()`; unclassified errors, and errors outside the `MultiError` per-path shape (YAML/JSON parse failures reported per input file, cycle-detection's single summary line, marshal errors), are unaffected by this flag today — use the Go API (`ClassifyError`, `GraftError.Code()`, `PathError.Code()`) to classify those programmatically.

## Codes

Every code below has a real trigger in graft's code and a passing test (`pkg/graft/errors_test.go`, `pkg/graft/operators/op_vault_errorcode_test.go`). "CLI-reachable" means a plain `graft merge` on ordinary input reaches it; "library API" means the trigger is a public `pkg/graft` function/method a Go program embedding graft can call, not something the `graft` CLI itself exposes a direct path to today.

### Parse (E1xx)

| Code | Meaning | Trigger |
|------|---------|---------|
| E100 | YAML/JSON parsing, or `(( if/for/while/case ))` control-flow expansion, failed | CLI-reachable: `graft merge` on a file with invalid YAML/JSON. Reported per input file, not as a `MultiError` `*PathError` entry, so `GRAFT_ERROR_CODES=1` does not tag it on stderr today — see the carve-out above. `ClassifyError`/`GraftError.Code()` still return `E100` for it programmatically. |
| E101 | A reference/path expression (e.g. inside `(( grab ))`) could not be parsed | Library API: `tree.ParseCursor` given malformed bracket syntax (e.g. unbalanced `]`) |

### Evaluation (E2xx)

| Code | Meaning | Trigger |
|------|---------|---------|
| E200 | Generic operator-evaluation failure not covered by a more specific code | Library API: `graft.NewEvaluationError` (used internally by `MergeBuilder`'s evaluation fallback) |
| E201 | A reference resolved to a path that doesn't exist | CLI-reachable: `(( grab missing.path ))` |
| E202 | A path held the wrong kind of value (map/list/scalar) for the operation | CLI-reachable: `(( grab a.b ))` where `a` is a scalar |
| E203 | The operator data-flow graph has a cycle | CLI-reachable: `a: (( grab b ))` / `b: (( grab a ))`. The cycle detector returns a single summary error, not a `MultiError` of `*PathError` entries, so `GRAFT_ERROR_CODES=1` does not tag it on stderr today — see the carve-out above. `ClassifyError` still returns `E203` for it programmatically. |
| E204 | `(( param "..." ))` was never overridden | CLI-reachable: any unresolved `(( param ))` |
| E205 | An operator name is not registered | Library API: `graft.ValidateOperatorArgs("unknown", n)`. Not reachable via a normal merge today — the parser (`identifierOpensOpcallAt`) only recognizes an identifier as an operator call at all once it is already a registered name, so an unregistered name in `(( ... ))` position falls back to `NullOperator` (left unevaluated, not an error) rather than reaching this code path. |
| E206 | Operator called with the wrong number of arguments, or a required argument was nil/missing | CLI-reachable: e.g. `(( concat "only-one" ))`, `(( join ))`, `(( empty ))`, `(( file ))` with 0 or 3+ arguments. Note `GraftError{Type: OperatorError}` (`graft.NewOperatorError`) is deliberately *not* mapped to E206 or any other code (`Code()` returns `""`): its message is caller-supplied free text, not reliably an argument-count problem, and it has no real (non-example) construction site today. |
| E207 | Division/modulo by zero (or null) | CLI-reachable: `(( a / b ))` with `b` equal to `0` |
| E210 | `(( op@target ... ))` used on an operator that doesn't support `@target` | CLI-reachable: `(( concat@x "a" "b" ))` |

### Merge (E3xx)

| Code | Meaning | Trigger |
|------|---------|---------|
| E300 | Documents could not be merged, for a reason other than a type mismatch | Library API: `graft.NewMergeError` (used internally by `MergeBuilder`) |
| E301 | A structural/path operation on the `Document`/`MergeBuilder` API was invalid (empty path, segment not found, array index out of bounds, navigating through a non-container value, wrong value type for a typed getter, ...) | Library API: e.g. `doc.GetString("a")` where `a` holds an int |

### Backend (E4xx)

| Code | Meaning | Trigger |
|------|---------|---------|
| E400 | Generic external-service integration failure | Library API only: `graft.NewExternalError` — exported for custom operators/backends; no internal graft call site constructs it today |
| E403 | A Vault secret path or field does not exist | CLI-reachable: `(( vault "secret/path:key" ))` against a path Vault doesn't have |

Connection/authentication failures for Vault, AWS, and NATS are not assigned codes: graft does not currently classify those failure modes distinctly from one another at any single call site, and inventing codes for them without a real, distinguishable trigger would be exactly the kind of aspirational code this taxonomy avoids.

### System (E9xx)

| Code | Meaning | Trigger |
|------|---------|---------|
| E900 | Invalid engine/library configuration | Library API: `graft.NewEngine(graft.WithConcurrency(-1))` |
| E901 | A file referenced by `(( file ))`, or (via the stdlib `fs.ErrNotExist` sentinel) any other file path graft opens, does not exist | CLI-reachable: `(( file "/nonexistent/path" ))`. Note `(( load ))` does not trigger this: it checks `os.Stat` first and returns a generic "not a file or usable URI" message for a missing path rather than propagating the underlying not-found error. |
| E902 | A file referenced the same way could not be read due to permissions (stdlib `fs.ErrPermission` sentinel) | Classification-tested directly against a synthetic `fs.ErrPermission`; not exercised through an actual permission-denied file in CI, since that depends on not running as root |

## CLI exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Usage error (bad flags/arguments) |
| 2 | Merge/evaluation/parse failure, or any other runtime error |

There is no per-category exit code (no separate code for parse vs. evaluation vs. backend failures) and no use of `126`/`127` — those are shell conventions, not something graft itself returns.

## Debugging Tips

### Enable Debug Output

```bash
# Debug logging
DEBUG=1 graft merge base.yml overlay.yml

# Trace logging (verbose)
TRACE=1 graft merge base.yml overlay.yml

# Or with flags
graft merge -D base.yml overlay.yml
graft merge -T base.yml overlay.yml
```

### Use the Interactive Debugger

`graft debug` (also `graft merge --interactive`) launches a step-through REPL over the merge:

```bash
graft debug base.yml overlay.yml
```

Run `help` inside the REPL for the current command list (step-through, tree inspection, and history commands); it evolves independently of this document, so it is the source of truth for exact subcommand names rather than a duplicated list here.

### Skip Evaluation for Parsing Issues

```bash
graft merge --skip-eval base.yml overlay.yml
```

### Opt Into Error Codes

```bash
GRAFT_ERROR_CODES=1 graft merge base.yml overlay.yml
```

### Test Backend Connectivity

```bash
# Vault
vault status
vault kv get secret/test

# AWS
aws sts get-caller-identity
aws ssm get-parameter --name "/test"

# NATS
nats account info
```

## See Also

- [CLI Quick Reference](cli-quick-reference.md) - Debug flags

- [Environment Variables](environment-variables.md) - Configuration

- [Troubleshooting Guide](../user-guide/configuration.md) - Common issues
