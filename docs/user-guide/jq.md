# jq Command

Run jq programs over JSON and YAML, with graft's own engine, so no jq binary is needed.

## Usage

```sh
graft jq [flags] PROGRAM [FILE...]
graft jq [flags] --args PROGRAM [STRING...]
graft jq [flags] --jsonargs PROGRAM [JSON...]
```

`graft jq` takes jq's flags, reads JSON or YAML from each file or from standard input, runs the program, and writes JSON. We target jq 1.8.2, so a script that calls `jq` can call `graft jq` with the same flags, the same program, and the same inputs, and get the same output and the same exit code. The departures are listed below.

```sh
$ graft jq '.items[].n' config.yml
1
2

$ graft jq -c . config.yml
{"name":"web","items":[{"n":1},{"n":2}]}

$ graft jq -o yaml '.items' config.yml
- n: 1
- n: 2
```

## YAML input

Every input file may be JSON or YAML, because JSON is a subset of YAML and graft users live in YAML. A file that holds several YAML documents separated by `---` is read as several inputs, so the program runs once for each document, just as it does for a stream of JSON values.

With `--input-format auto`, which is the default, graft looks at the first token and commits to JSON or to YAML without trying the other. That means a YAML flow mapping such as `{a: 1}` is read as JSON and fails, because its key is not a string. We pin the reading with `--input-format yaml` for that case, and with `--input-format json` when we want strict JSON errors.

```sh
$ echo '{a: 1}' | graft jq -c .
Invalid input (at <stdin>:1): line 1, column 2: object key must be a string, found 'a'

$ echo '{a: 1}' | graft jq --input-format yaml -c .
{"a":1}
```

## Output

The default output is JSON, indented by two spaces. `-c` prints each result on one line, `--tab` indents with a tab, and `--indent N` indents with N spaces, from -1 to 7, where -1 means a tab. `-r`, `-j`, and `--raw-output0` print strings without quotes, with no newline, or with a NUL after each result. `-a` escapes everything outside ASCII, and `-C` and `-M` force or forbid color.

`-o yaml`, which can also be written `-o y`, prints each result as a YAML document instead. jq has no such flag, so this one is graft's own.

## Flags

The table lists the flags that matter here, and `--color` and `--no-color` work as they do for the other graft commands. A phase note means the flag is accepted by the parser but not wired up yet, and graft answers with a usage error and exit code 2 until that release.

| Flag | Meaning | Status |
|------|---------|--------|
| `-n`, `--null-input` | Run the program once on `null` | Available |
| `-R`, `--raw-input` | Read each line as a string | Available |
| `-s`, `--slurp` | Read every input into one array | Available |
| `--input-format FORMAT` | Read input as `auto`, `json`, or `yaml` | Available |
| `-c`, `-r`, `-j`, `-a`, `--raw-output0` | Output shaping, as in jq | Available |
| `-o FORMAT` | Print `json` or `yaml` | Available |
| `-S`, `--sort-keys[=MODE]` | Sort object keys, see below | Available |
| `-C`, `-M` | Force or forbid color | Available |
| `--tab`, `--indent N` | Indentation | Available |
| `--unbuffered` | Write each output as it is produced | Available |
| `--float-numbers` | Do arithmetic in doubles, as jq does | Available |
| `--depth-limit N` | Bound call and input-file nesting, default 10000 | Available |
| `-f`, `--from-file FILE` | Read the program from a file | Available |
| `--arg`, `--argjson`, `--slurpfile`, `--rawfile` | Bind a variable | Available |
| `--args`, `--jsonargs` | Read the remaining arguments as positionals | Available |
| `-m`, `--merge` | Merge the inputs with `graft merge` first | Available |
| `-E`, `--eval` | Resolve each input on its own first | Available |
| `--prune KEY`, `--cherry-pick KEY` | With `-m` or `-E`, trim the result | Available |
| `-e`, `--exit-status` | Set the exit code from the last output | Available |
| `--run-tests [FILE]` | Run jq test cases | Available |
| `--repl` | Open the graft debugger on the inputs | Available |
| `-L DIR`, `import`, `include` | Load jq modules | Available |
| `--stream`, `--seq` | Streaming forms of input and output | Available |

The `-m` and `-E` flags also accept the feature flags `graft merge` reads, which are `--skip-eval`, `--fallback-append`, `--go-patch`, `--multi-doc`, `--skip-vault`, `--skip-aws`, and `--skip-nats`.

## Merging first

Two flags let us query the result of a merge without piping through `graft merge`.

- `-m` or `--merge`
  Merges every input file with `graft merge`, resolves the `(( ))` operators, and runs the program on the merged document.

- `-E` or `--eval`
  Resolves the operators in each input document on its own and runs the program on every result. A file with three documents yields three evaluated documents, and each document sees only itself.

The two flags cannot be combined, and asking for both is a usage error with exit code 2.

```sh
$ graft jq -m -c keys base.yml overlay.yml
["a","extra","meta","name","z"]

$ graft jq -E --prune meta -c . base.yml
{"a":2,"name":"x","z":1}
```

Because the merge builds a map, the merged document's keys come out in graft's usual order, with numeric keys first and then names in natural order. For example, merging a file that holds the keys `10`, `9`, `b`, and `a` prints them as `9`, `10`, `a`, and `b`. Pass `--sort-keys=lexical` when we want the keys ordered by code point instead.

## Sorting keys

Objects keep their input order by default, as jq does. `-S` is jq's boolean flag and sorts keys lexically, by Unicode code point. `--sort-keys` takes an optional mode.

| Form | Order |
|------|-------|
| `--sort-keys` or `--sort-keys=lexical` | Unicode code point, which is what jq's `-S` does |
| `--sort-keys=spruce` | Numeric keys first, then strings in natural order, which is how `graft merge` writes |
| `--sort-keys=none` | The order the keys arrived in, which is the default |

## Exit codes

| Situation | Exit code |
|-----------|-----------|
| Success | 0 |
| `-e` and the last output was `false` or `null` | 1 |
| Usage error, unknown flag, unreadable file, or `--indent` out of range | 2 |
| `-m` combined with `-E` | 2 |
| Program does not parse or compile | 3 |
| `-e` and the program produced no output | 4 |
| Runtime error, invalid input, or depth limit | 5 |
| `halt_error(n)` | n |

jq looks only at the last output for `-e`, so `(false, true)` exits 0 and `(true, false)` exits 1.

## Diagnostics

Errors use graft's style, which is a bold red phrase, a colon, and the detail. Color on standard error follows `--color`, `--no-color`, `NO_COLOR`, and whether standard error is a terminal, exactly as `graft merge` does.

| Phrase | When |
|--------|------|
| `Compile error` | The program does not parse or compile |
| `Runtime error (at FILE:LINE)` | The program fails while running |
| `Invalid input (at FILE:LINE)` | An input does not parse |
| `Unable to open file` | An input file cannot be read |
| `Unknown option` | A flag is not recognized |
| `Invalid flags` | A flag is used wrongly |

Text that a program writes to standard error on purpose, through `debug`, `stderr`, and `halt_error`, is printed exactly as jq prints it, with no prefix and no color.

## Departures from jq

These are the places where `graft jq` deliberately differs from jq 1.8.2.

- YAML input
  jq reads only JSON, and `graft jq` reads YAML as well. See the section on YAML input for how auto-detection decides.

- Exact decimal arithmetic
  jq computes in doubles, and graft computes exactly. `0.1 + 0.2` is `0.3`, `100000000000000000000 + 1` is `100000000000000000001`, and division rounds to 34 significant digits. Passing `--float-numbers` restores jq's arithmetic, so `0.1 + 0.2` prints `0.30000000000000004` again. A number literal past 100,000 significant digits passes through unchanged in both modes, and only arithmetic on it differs.

- YAML special numbers
  Not-a-number and infinity print as `.nan`, `.inf`, and `-.inf` in YAML output, as YAML 1.2 defines them.

- Diagnostics
  Errors use graft's phrases in place of `jq: error:`, and graft does not print jq's compile-error count or caret display. Exit codes are unchanged, so a script that tests `$?` keeps working, while a script that searches standard error for `jq: error` does not.

- Extra flags
  `-o yaml`, `-S` with named modes, `-m`, `-E`, and `--repl` are graft's own. None of them collides with a jq flag.

- Names and help
  Only the exact name `jq` dispatches to this command, and a link named `jq-1.7` runs as graft. Under the `jq` name, `--version` leads with jq's version line and then prints graft's own. `--help` shows graft's layout rather than jq's text. See [Aliases](aliases.md).

## Debugging with --repl

`graft jq --repl PROGRAM FILE` opens the graft debugger on the inputs. In a debug session, the `jq`, `select`, and `watch` verbs run queries over the tree as it stands at the current step, and `break jq:EXPR` stops where a query matches. The `yq` verb runs yq expressions in the same session. The [debug command](cli/debug.md) page describes each verb.

## Compatibility

We measure compatibility against a coverage inventory of jq behaviors. Each entry passes when every differential case it names matches a golden recorded from the real jq 1.8.2. The percentages below come from `tests/query-parity/jq/report.txt`, and each one was measured at the end of phase J2, which is the release this page describes. The last column says when we expect the figure to rise.

| Area | Passing | Phase measured | Expected to rise |
|------|---------|----------------|------------------|
| Grammar | 100% | J2 | Done |
| Builtins | 100% | J2 | Done |
| Flags | 100% | J2 | Done |
| Exit codes | 97% | J2 | J3 |
| Numbers | 80% | J2 | Stays where it is, because the missing cases are deliberate departures |
| Strings | 100% | J2 | Done |
| Regular expressions | 96% | J2 | J3 |
| Dates | 96% | J2 | Stays where it is, because the missing case is a deliberate departure |
| Math | 73% | J2 | Stays where it is, because the missing cases are deliberate departures |
| Modules | 100% | J2 | Done |
| Streaming | 100% | J2 | Done |
| CLI | 97% | J2 | J3 |
| Overall | 97% | J2 | J3 |

Phase J2 completes the language with dates, math, formats, modules, streaming, and lookaround and backreferences in regular expressions, so modules and streaming now pass in full. The numbers, dates, and math rows stay below 100 percent because `graft jq` keeps exact decimal arithmetic, its own resource limits, and Go's math library, and each case where that gives a different answer from jq is recorded as a deliberate departure. Phase J3 hardens the result with performance work and fuzzing, reads input literals as strictly as jq does, and adds the last regular expression forms, which are subroutine calls, the absent operator, and grapheme clusters. Each phase is a plain minor release.

## Related

- [Aliases](aliases.md) covers running graft as `jq`.

- [merge](cli/merge.md) describes the merge that `-m` and `-E` run first.

- [yq](yq.md) describes the sibling command for yq expressions.

- [debug](cli/debug.md) describes the debugger that `--repl` opens.
