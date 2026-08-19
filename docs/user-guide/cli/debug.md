# debug Command

Interactive debugging REPL for step-through merge analysis.

## Usage

```sh
graft debug file1.yml file2.yml ... fileN.yml
```

Or use the merge command with `--interactive`:

```sh
graft merge --interactive file1.yml file2.yml
```

At least one file is required; `debug` reads REPL commands from stdin, so
it cannot also read a document from stdin the way `merge`/`json` do.

## Flags

| Flag | Description |
|------|-------------|
| `--go-patch` | Same meaning as `merge --go-patch`: parse an array-rooted file as a [go-patch](https://github.com/cppforlife/go-patch) document instead of erroring on a non-map root. Applies to every merge the session performs (`load`'s base document, each `step`, `diff`'s recomputed base). |
| `--fallback-append` | Same meaning as `merge --fallback-append`: use append semantics instead of inline for the default array-merge fallback. |

These are the only two flags `debug` itself accepts. `--prune` and
`--cherry-pick` are cleared for the session's own raw merge steps (they
only make sense once, on the final result, and would otherwise strip data
`step`/`inspect` need to show), and `--skip-eval`/`--dataflow-order`/
`--history`/`--trace-path`/`--show-changes`/`--changes-only` have no
meaning for an interactive session.

One exception applies to the `merge --interactive` spelling only: because
input files are resolved before the REPL starts, `-m`/`--multi-doc` does
take effect there, splitting each `---`-separated document into its own
step. `graft merge --interactive -m multi.yml` loads two documents where
`graft merge --interactive multi.yml` loads one. `graft debug` does not
accept `-m`.

## Overview

The debug REPL provides interactive control over the merge process:

- Step through merges one file at a time
- Set breakpoints on specific paths
- Inspect values at any point
- View per-path change history
- Set Vault connection settings for the rest of the session
- Defer or force operator evaluation
- Recall, search, and complete commands at the prompt

## REPL Commands

| Command | Description |
|---------|-------------|
| `load` | Parse every input file and report each one's top-level key count |
| `step` | Merge the next file (or, on the final step, evaluate operators) |
| `continue` | Run every remaining step, stopping early at a breakpoint |
| `break <path>` | Set a breakpoint on a path |
| `unbreak <path>` | Remove a breakpoint |
| `breaks` | List all breakpoints |
| `inspect [path]` | Show the current value at path (the whole document if omitted) |
| `history <path>` | Show the same per-file history `merge --history` would for path |
| `defer <path>` | Leave the operator at path unevaluated on the next `step`/`continue` |
| `eval <path>` | Immediately evaluate the operator at path, regardless of `defer` |
| `config [key] [value]` | View or set `vault.addr`/`vault.token`/`vault.namespace` for this session |
| `output` | Show the current document state as YAML |
| `diff` | Show changes from the first loaded file to the current state |
| `export <file>` | Export the current state to file (YAML, or JSON if the name ends `.json`) |
| `help [command]` | Show command list, or detail for one command |
| `quit` / `exit` | Exit the debugger |

`config` wires three Vault settings — `VAULT_ADDR`, `VAULT_TOKEN`, and
`VAULT_NAMESPACE`. That is a deliberately narrow set, not the complete list
of environment variables graft reads: `VAULT_SKIP_VERIFY`, the AWS session
fallback's `AWS_PROFILE`/`AWS_REGION`/`AWS_ROLE`, and NATS's `NATS_URL` plus
its `NATS_*` tuning variables are all read from the environment too, and
none of them are settable from `config` today.

## Editing, Recall, and Completion

When you run the debugger from a terminal, the prompt is a full line editor
rather than a bare read of stdin. The keys behave the way they do in a
shell.

| Key | Effect |
|-----|--------|
| Up / Down | Walk backward and forward through previous commands |
| Ctrl+R | Search backward through earlier commands as you type |
| Ctrl+S | Search forward again after a Ctrl+R |
| Tab | Complete the word under the cursor |
| Ctrl+A / Ctrl+E | Jump to the start or end of the line |
| Ctrl+W | Delete the word before the cursor |
| Ctrl+C | Abandon the line in hand and prompt again |
| Ctrl+D | Leave the debugger, the same as `quit` |

Tab completion knows what each command takes. On the first word it offers
command names, so `ins` followed by Tab becomes `inspect`. After a command
that takes a path, it offers the document itself, one level per press:

```
graft> inspect meta.<TAB>
meta.cpu_per_replica   meta.domain            meta.environment       meta.replicas
```

Those completions come from the tree as it stands at the current step, not
from a fixed list, so a key that first appears in the second file shows up
only once you have stepped that far. Run `load` first, since
before that there is no document to complete against. `unbreak` offers only
the paths that actually have a breakpoint, `config` offers its three known
keys, and `export` completes filenames from the working directory.

Command recall outlives the session. Commands are appended to
`~/.graft/debug_history`, which is created readable only by you, and the next
debug session starts with everything you typed in the last one. One kind of
line is deliberately left out: setting a secret, as in
`config vault.token s.something`, is never written to the file, though
reading the same key with a bare `config vault.token` is.

The `history` command is unrelated to any of this. It reports where a path's
value came from across the merged files. Recall has no command of its own and
lives on the arrow keys and Ctrl+R.

None of this applies when the debugger's input is a pipe or a file, as in
`graft debug base.yml < script.txt`. There is no terminal to edit on, so
graft reads the script a line at a time and records nothing for recall.

## Session Example

The transcript below is real output, captured from `base.yml`/`env.yml`/
`secrets.yml`:

```yaml
# base.yml
database:
  host: localhost
  port: 5432
  pool_size: 10
meta:
  version: "1.0"
```

```yaml
# env.yml
database:
  host: db.prod.example.com
  pool_size: 50
server:
  timeout: 60
```

```yaml
# secrets.yml
database:
  password: (( grab meta.version ))
```

```sh
$ graft debug base.yml env.yml secrets.yml

Welcome to the Graft Debugger
Type 'help' for available commands.

graft> load
Loaded 3 documents:
  [0] base.yml (2 keys)
  [1] env.yml (2 keys)
  [2] secrets.yml (1 key)

graft> step
[1/3] Merging env.yml...
  database.host: localhost → db.prod.example.com
  database.pool_size: 10 → 50
  server: <none> → timeout: 60

graft> inspect database
host: db.prod.example.com
pool_size: 50
port: 5432

graft> break database.password
Breakpoint set on database.password

graft> continue
[2/3] Merging secrets.yml...
  database.password: <none> → (( grab meta.version ))
Breakpoint hit: database.password
  Current: (( grab meta.version ))

graft> config vault.addr
Current: (not set)

graft> config vault.addr https://vault-dev.example.com
Updated vault.addr

graft> eval database.password
Evaluating: (( grab meta.version ))
Result: "1.0"

graft> continue
[3/3] Evaluating operators...
Evaluation complete.

graft> output
database:
  host: db.prod.example.com
  password: "1.0"
  pool_size: 50
  port: 5432
meta:
  version: "1.0"
server:
  timeout: 60

graft> history database.host
database.host:
  [0] base.yml       → localhost
  [1] env.yml        → db.prod.example.com
  Final              → db.prod.example.com

graft> export result.yml
Exported to result.yml

graft> quit
```

Two things worth noting from this real transcript, versus what you might
expect:

- `inspect database` prints database's fields directly (no `database:`
  header line) — `inspect` shows the *value* at a path, and a map's own
  YAML rendering has no key line for itself.
- When an entire new top-level section (`server`) is added by a later
  file, `step`/`continue`/`diff` render it as `server: <none> → timeout:
  60` rather than a nested block. graft's diff engine reports a
  wholly-new subtree as one change at the subtree's root, carrying the
  whole subtree as the value; for a multi-key subtree this looks a little
  odd inline, but no data is lost — `inspect server` or `output` show the
  real nested structure.

## Commands in Detail

### load

Parses every input file individually (not merged) and reports each one's
own top-level key count, then establishes the first file as the starting
document:

```
graft> load
Loaded 3 documents:
  [0] base.yml (2 keys)
  [1] env.yml (2 keys)
  [2] secrets.yml (1 key)
```

### step

Runs exactly one step: a merge of the next file, or, once every file has
been merged, operator evaluation:

```
graft> step
[1/3] Merging env.yml...
  database.host: localhost → db.prod.example.com
```

### continue

Runs every remaining step, or stops early at the first breakpoint hit:

```
graft> continue
[2/3] Merging secrets.yml...
  database.password: <none> → (( grab meta.version ))
[3/3] Evaluating operators...
Evaluation complete.
```

### break / unbreak / breaks

Manage breakpoints. A breakpoint fires the moment `step`/`continue`
changes that exact path (checked against the same change list the step
itself prints):

```
graft> break database.password
Breakpoint set on database.password

graft> break server.timeout
Breakpoint set on server.timeout

graft> breaks
Breakpoints:
  - database.password
  - server.timeout

graft> unbreak database.password
Breakpoint removed
```

Removing a breakpoint that isn't set reports that instead of erroring:

```
graft> unbreak no.such.path
No breakpoint on no.such.path
```

### inspect

Show the current value at a path, or the whole document with no path:

```
graft> inspect database
host: db.prod.example.com
pool_size: 50
port: 5432

graft> inspect database.host
db.prod.example.com
```

A path that doesn't exist (yet, or ever) is reported, not silently empty:

```
graft> inspect no.such.path
Path not found: no.such.path
```

### history

Shows the same per-file entry list `graft merge --history` would show for
one path, with the session's deferred paths applied (see
[defer](#defer)) — see [History Tracking](../history-tracking.md) for the full
format:

```
graft> history database.host
database.host:
  [0] base.yml       → localhost
  [1] env.yml        → db.prod.example.com
  Final              → db.prod.example.com
```

### defer

Marks a path so evaluation leaves its operator unresolved.
This rewrites the path's `(( op ... ))` text to `(( defer op ... ))` — the
real spruce-compatible defer operator — so the effect is identical to
writing `(( defer ))` in the source YAML by hand:

```
graft> defer database.password
Marked database.password for deferred evaluation

graft> continue
[3/3] Evaluating operators...
Evaluation complete.

graft> inspect database.password
(( grab meta.version ))
```

Deferring applies to the whole session rather than to the next step
alone, so it also covers `history`. That matters more than it sounds
like it should. `history` recomputes the entire merge to trace one path,
and an operator it cannot resolve stops that recompute before any tracing
happens. An unreachable Vault path therefore makes `history` fail for
every path you ask about, including unrelated ones. Defer the offending
path first and the trace works:

```
graft> defer properties.api.admin_password
Marked properties.api.admin_password for deferred evaluation

graft> history meta.replicas
meta.replicas:
  [0] base.yml       → 2
  [1] env-prod.yml   → 6
  Final              → 6
```

To excuse every unreachable secret at once instead of naming them one at
a time, set `REDACT` when you launch the session:

```sh
REDACT=1 graft debug base.yml env-prod.yml sizing.yml
```

Every Vault lookup then resolves to the literal `REDACTED` rather than
failing, which keeps `step`, `continue`, `history`, and `output` all
working and makes the session safe to paste into a ticket. `REDACT` is
read from the environment at launch and is not settable from `config`.

### eval

Force-evaluates the operator at one path immediately, independent of
`defer` or how far `step`/`continue` has progressed:

```
graft> eval database.password
Evaluating: (( grab meta.version ))
Result: "1.0"
```

### config

View or set the three Vault connection settings `config` wires. With no
arguments, lists all three:

```
graft> config
vault.addr: (not set)
vault.token: (not set)
vault.namespace: (not set)

graft> config vault.addr
Current: (not set)

graft> config vault.addr https://vault-dev.example.com
Updated vault.addr

graft> config vault.addr
Current: https://vault-dev.example.com
```

Setting a value here really does `os.Setenv` the corresponding variable for
the rest of the process — a later `eval`/`continue` will use it for real
Vault lookups. It does not, however, force any already-constructed Vault
client (from an earlier evaluation) to reconnect with the new value.

### output

Show the current document state as YAML:

```
graft> output
database:
  host: db.prod.example.com
  password: "1.0"
  pool_size: 50
  port: 5432
meta:
  version: "1.0"
server:
  timeout: 60
```

### diff

Show changes from the first loaded file to the current state:

```
graft> diff
Changes from base.yml:

  database.host: localhost → db.prod.example.com
  database.password: <none> → "1.0"
  database.pool_size: 10 → 50
  server: <none> → timeout: 60
```

### export

Save the current state to a file — YAML, or JSON if the filename ends in
`.json`:

```
graft> export result.yml
Exported to result.yml

graft> export result.json
Exported to result.json
```

### help

Get help:

```
graft> help
Available commands:
  load            Load all documents
  step            Execute next merge step
  continue        Run to completion
  break           Set breakpoint on path
  unbreak         Remove breakpoint
  breaks          List all breakpoints
  inspect         Show current value at path
  history         Show change history for path
  defer           Mark path for deferred evaluation
  eval            Force evaluate operator at path
  config          View/set configuration
  output          Show current document state
  diff            Show changes from original
  export          Export current state to file
  help            Show help
  quit            Exit the debugger

graft> help break
break <path>

Sets a breakpoint on a path. The debugger reports when this path changes
during a later step or continue.

Example:
  break database.password
```

An unrecognized command name is reported, not silently ignored:

```
graft> frobnicate
Unknown command: frobnicate. Type 'help' for available commands.
```

## Use Cases

### Debugging Merge Issues

```sh
# Why isn't my value being overwritten?
graft debug base.yml overlay.yml

graft> step
graft> inspect my.path
graft> history my.path
```

### Testing Vault Integration

```sh
# Test with different Vault servers
graft debug config.yml

graft> load
graft> config vault.addr https://vault-dev.example.com
graft> eval secrets.api_key
graft> config vault.addr https://vault-prod.example.com
graft> eval secrets.api_key
```

### Understanding Complex Merges

```sh
# Step through a complex multi-file merge
graft debug base.yml defaults.yml env.yml secrets.yml overrides.yml

graft> load
graft> step  # See each file's contribution
graft> step
graft> step
```

### Selective Evaluation

```sh
# Evaluate everything except one path
graft debug config.yml

graft> load
graft> defer expensive.external.call
graft> continue
graft> output  # expensive.external.call still shows the operator
```

## Tips

### Quick Inspection

`inspect` with a partial path shows that subtree only:

```
graft> inspect database
# Shows every field under database

graft> inspect database.connection
# Shows just the connection subsection
```

### Breakpoint Strategy

Set breakpoints on the paths you're debugging, then `continue` — the
debugger stops at whichever one fires first:

```
graft> break database.password
graft> break api.key
graft> continue
```

### Export Intermediate States

Save state at different points:

```
graft> step
graft> export after-overlay.yml
graft> step
graft> export after-secrets.yml
```

### Compare States

Use `diff` to see cumulative changes from the first file so far:

```
graft> step
graft> diff
graft> step
graft> diff
```

## See Also

- [Inspecting a Merge](../../examples/inspecting-a-merge.md) - A walkthrough that debugs a failing merge from first symptom to answer
- [merge](merge.md) - Non-interactive merge
- [History Tracking](../history-tracking.md) - History features
- [Operators](../operators/) - Operator reference
