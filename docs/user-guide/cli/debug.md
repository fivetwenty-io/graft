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

## Overview

The debug REPL provides interactive control over the merge process:

- Step through merges one file at a time
- Set breakpoints on specific paths
- Inspect values at any point
- View change history
- Modify configuration mid-debug
- Defer or force operator evaluation

## REPL Commands

| Command | Description |
|---------|-------------|
| `load` | Load all documents |
| `step` | Execute next merge step |
| `continue` | Run to completion |
| `break <path>` | Set breakpoint on path |
| `unbreak <path>` | Remove breakpoint |
| `breaks` | List all breakpoints |
| `inspect <path>` | Show current value at path |
| `history <path>` | Show change history for path |
| `defer <path>` | Mark path for deferred evaluation |
| `eval <path>` | Force evaluate operator at path |
| `config [key] [value]` | View/set configuration |
| `output` | Show current document state |
| `diff` | Show changes from original |
| `export <file>` | Export current state to file |
| `help [command]` | Show help |
| `quit` / `exit` | Exit debugger |

## Session Example

```sh
$ graft debug base.yml overlay.yml secrets.yml

Welcome to the Graft Debugger
Type 'help' for available commands.

graft> load
Loaded 3 documents:
  [0] base.yml (45 keys)
  [1] overlay.yml (12 keys)
  [2] secrets.yml (8 keys)

graft> step
[1/3] Merging overlay.yml...
  database.host: "localhost" → "db.prod.com"
  database.pool_size: 10 → 50
  server.timeout: 30 → 60
  server.ssl: <none> → true

graft> inspect database
database:
  host: "db.prod.com"
  port: 5432
  pool_size: 50
  password: (( vault "secret/db:password" ))  # unevaluated

graft> break database.password
Breakpoint set on database.password

graft> continue
[2/3] Merging secrets.yml...
[3/3] Evaluating operators...
Breakpoint hit: database.password
  Current: (( vault "secret/db:password" ))

graft> config vault.addr
Current: https://vault.example.com

graft> config vault.addr https://vault-dev.example.com
Updated vault.addr

graft> eval database.password
Evaluating: (( vault "secret/db:password" ))
Result: "dev-password-123"

graft> continue
Evaluation complete.

graft> output
application:
  name: my-app
database:
  host: db.prod.com
  port: 5432
  pool_size: 50
  password: dev-password-123
...

graft> history database.host
database.host:
  [0] base.yml:12    → "localhost"
  [1] overlay.yml:5  → "db.prod.com"
  Final              → "db.prod.com"

graft> export result.yml
Exported to result.yml

graft> quit
```

## Commands in Detail

### load

Load all documents without merging:

```
graft> load
Loaded 3 documents:
  [0] base.yml (45 keys)
  [1] overlay.yml (12 keys)
  [2] secrets.yml (8 keys)
```

### step

Execute the next merge step:

```
graft> step
[1/3] Merging overlay.yml...
  database.host: "localhost" → "db.prod.com"
```

### continue

Run to completion (or next breakpoint):

```
graft> continue
[2/3] Merging secrets.yml...
[3/3] Evaluating operators...
Done.
```

### break / unbreak / breaks

Manage breakpoints:

```
graft> break database.password
Breakpoint set on database.password

graft> break server.ssl
Breakpoint set on server.ssl

graft> breaks
Breakpoints:
  - database.password
  - server.ssl

graft> unbreak database.password
Breakpoint removed
```

### inspect

Show current value at a path:

```
graft> inspect database
database:
  host: "db.prod.com"
  port: 5432
  password: (( vault "secret/db:password" ))

graft> inspect database.host
"db.prod.com"
```

### history

Show how a value changed:

```
graft> history database.host
database.host:
  [0] base.yml:12    → "localhost"
  [1] overlay.yml:5  → "db.prod.com"
```

### defer

Mark a path to skip evaluation:

```
graft> defer database.password
Marked database.password for deferred evaluation

graft> continue
# password remains as (( vault ... ))
```

### eval

Force evaluate an operator:

```
graft> eval database.password
Evaluating: (( vault "secret/db:password" ))
Result: "my-secret-password"
```

### config

View or modify configuration:

```
graft> config
vault.addr: https://vault.example.com
vault.namespace: (not set)
aws.region: us-west-2
...

graft> config vault.addr
Current: https://vault.example.com

graft> config vault.addr https://vault-dev.example.com
Updated vault.addr
```

### output

Show current document state:

```
graft> output
application:
  name: my-app
database:
  host: db.prod.com
...
```

### diff

Show changes from original:

```
graft> diff
Changes from base.yml:

  database.host: "localhost" → "db.prod.com"
  database.pool_size: 10 → 50
  server.ssl: <none> → true
```

### export

Save current state to file:

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
  ...

graft> help break
break <path>

Set a breakpoint on a path. The debugger will pause when
this path is modified during merge or evaluation.

Example:
  break database.password
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
graft> output  # expensive call still shows operator
```

## Tips

### Quick Inspection

Use `inspect` with partial paths to see subtrees:

```
graft> inspect database
# Shows entire database section

graft> inspect database.connection
# Shows just connection subsection
```

### Breakpoint Strategy

Set breakpoints on paths you're debugging:

```
graft> break database.password
graft> break api.key
graft> continue
# Stops at first match
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

Use diff to see what changed:

```
graft> step
graft> diff  # See changes so far
graft> step
graft> diff  # See cumulative changes
```

## See Also

- [merge](merge.md) - Non-interactive merge
- [History Tracking](../history-tracking.md) - History features
- [Operators](../operators/) - Operator reference
