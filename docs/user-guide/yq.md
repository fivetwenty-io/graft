# yq Command

Run yq expressions over YAML and JSON with graft's own engine, so we need no yq binary.

## Usage

```sh
graft yq [flags] [EXPRESSION] [FILE...]
graft yq eval [flags] [EXPRESSION] [FILE...]
graft yq eval-all [flags] [EXPRESSION] [FILE...]
```

`graft yq` takes yq's flags, reads YAML or JSON from each file or from standard input, runs the expression, and writes YAML, or JSON with `-o json`. We target yq 4.54.1, so a script that calls `yq` can call `graft yq` with the same flags, the same expression, and the same inputs, and get the same output and the same exit code. The departures are listed below.

The output keeps the comments, styles, anchors, and tags of the input, which is what makes yq the right tool for editing a file in place.

```sh
$ graft yq '.replicas' config.yml
3

$ graft yq '.replicas = 5' config.yml
# service settings
name: web
replicas: 5 # keep at three
ports:
  - 80
  - 443
labels: &lbl
  tier: front

$ graft yq -o json -I 0 . config.yml
{"name":"web","replicas":3,"ports":[80,443],"labels":{"tier":"front"}}
```

## Eval and eval-all

`eval`, which can also be written `e`, is the default command. It runs the expression once for each document of each file. `eval-all`, written `ea`, runs the expression once with every document of every file as a candidate, so the expression can compare or combine documents.

```sh
$ graft yq -N '.a' multi.yml
1
2

$ graft yq eval-all '[.a]' multi.yml
- 1
- 2
```

With `-n`, the expression runs once on `null` and no input is read. The expression defaults to `.` when we give none. `--expression EXPR` takes the expression from the command line and treats every other argument as a file, and `--from-file FILE` reads the expression from a file.

```sh
$ graft yq -n '.a.b = 1'
a:
  b: 1
```

## Formats

`graft yq` reads and writes YAML and JSON, and nothing else. Asking for another format name is an error that says so, and the exit code is 1.

```sh
$ graft yq -o xml . config.yml
Invalid -o value: "xml" is not built into graft yq (auto, yaml, and json only)
```

- `-o`, `--output-format`
  The output format is `auto`, `yaml` (or `y`), or `json` (or `j`). The default is `auto`, which prints JSON when the first input file has a name ending in `.json` and YAML otherwise. Standard input never counts as a `.json` file, so we pass `-o json` when we want JSON from a pipe. When `-p` pins the input format, `auto` stays YAML and graft prints the same warning yq does.

- `-p`, `--input-format`
  The input format is `auto`, `yaml`, or `json`. YAML is a superset of JSON, so `auto` reads both with the YAML reader.

- `-I`, `--indent`
  This indents the output by N spaces, and the default is 2. For JSON, `-I 0` prints each value on one line.

- `-N`, `--no-doc`
  This leaves out the `---` line between YAML documents.

- `-P`, `--prettyPrint`
  This drops quoting, flow, and block styles from the YAML output.

- `-r`, `--unwrapScalar`
  This prints a top-level string without quotes. It is on by default, and `-r=false` turns it off.

- `-0`, `--nul-output`
  This ends each output with a NUL in place of a newline.

A JSON file named `.json` is read as JSON, and the output format decides how it prints.

- Block style
  `-o yaml`, `-o y`, `-p json`, and `-P -o yaml` all print a `.json` file as block-style YAML.

- Flow style
  JSON that arrives on standard input keeps its flow style when printed as YAML, and so does a `.json` file read with `-p yaml -o yaml`.

- JSON
  `-P` on its own leaves a `.json` file printed as JSON.

```sh
$ graft yq -o yaml . data.json
b: 2
a:
  - 1
  - 2

$ echo '{"b": 2}' | graft yq -o yaml .
{"b": 2}
```

## Editing in place

`-i` rewrites the first file with the result, and it needs a file to rewrite. The file is left alone when the run fails. The `-i` flag cannot be combined with `-m`.

```sh
$ graft yq -i '.replicas = 9' config.yml
```

## Front matter

Some files, such as Markdown pages, begin with a block of YAML between two lines of `---` and carry other text after it. With `-f extract`, `graft yq` runs the expression over that YAML alone and prints the result. With `-f process`, we get the same result followed by the text that came after the YAML, left exactly as it was, so an edit to the front matter keeps the page intact.

```sh
$ graft yq -f process '.title = "Hello"' page.md
---
title: Hello
---
# The page body
```

The front matter ends at the next line that begins with `---`, and that line starts the text that follows. A file that does not begin with a `---` line has no front matter, so it reads as ordinary YAML. Only the first file is split, the content is printed once after the first output, and nothing is printed when the expression matches nothing. Any value other than `process` extracts, as in yq. Combined with `-i`, the first file is rewritten with the front matter and the text together.

## Splitting the output

With `-s EXPR`, each output is written to its own file instead of to standard output. We evaluate EXPR against the output, and the first value it gives, read as text, names the file, with `.yml` added, or `.json` under `-o json`. A name with a directory in it gets that directory made, and a file that already exists is replaced. A value that is a map or a list gives an empty name, so the file is called `.yml`.

```sh
$ graft yq -s '.name' '.[]' services.yml
$ ls
a.yml  b.yml  services.yml
```

The `--split-exp-file FILE` flag reads the expression from FILE, and it wins when both flags are given. An expression that does not compile, or that fails on an output, ends the run with exit code 1. The `-i` flag cannot be combined with a split. A name can leave the working directory, through `../x` or an absolute path, the same as in yq. With `-f process`, the text after the front matter is written over the last split file, so that file holds only that text, as it does in yq.

## Flags

The table lists the flags that matter here, and `--color` and `--no-color` work as they do for the other graft commands. A flag marked "Inert" is accepted so that scripts which pass it keep working, but it has no effect yet.

| Flag | Meaning | Status |
|------|---------|--------|
| `-n`, `--null-input` | Run the expression once on `null` | Available |
| `-i`, `--inplace` | Rewrite the first file with the result | Available |
| `-e`, `--exit-status` | Exit 1 when nothing matched, or every output was null or false | Available |
| `-o`, `-p` | Output and input format, see above | Available |
| `-I`, `-N`, `-P`, `-r`, `-0` | Output shaping, see above | Available |
| `-C`, `-M` | Force or forbid color | Available |
| `-S`, `--sort-keys[=MODE]` | Sort object keys, see below | Available |
| `--float-numbers` | Do arithmetic in int64 and doubles, as yq does | Available |
| `--expression EXPR`, `--from-file FILE` | Take the expression from a string or a file | Available |
| `--header-preprocess` | Treat a leading comment before `---` as a header, on by default | Available, and `=false` turns it off |
| `--security-disable-env-ops`, `--security-disable-file-ops` | Turn off the env or the file operators | Available |
| `--security-enable-system-operator` | Accepted, because `graft yq` has no system operator | Inert |
| `--yaml-fix-merge-anchor-to-spec` | Make a merge key follow the YAML specification | Available |
| `-m`, `--merge` | Merge the inputs with `graft merge` first | Available |
| `-E`, `--eval` | Resolve each input on its own first | Available |
| `--prune KEY`, `--cherry-pick KEY` | With `-m` or `-E`, trim the result | Available |
| `--repl` | Open the graft debugger on the files | Available |
| `-c`, `--yaml-compact-seq-indent` | Compact sequence indentation | Inert |
| `--string-interpolation` | Interpolation in strings, on by default | Available, and `=false` leaves `\(` in a string as written |
| `-f`, `--front-matter` | Run the expression over the front matter of the first file, see below | Available |
| `-s`, `--split-exp`, `--split-exp-file` | Write each output to a file named by an expression, see below | Available |

The inert flags do nothing today. `-c` leaves sequence indentation as it is. `--string-interpolation=false` and `--header-preprocess=false` are not inert. The first leaves `\(` in a string as written, and the second changes how a leading comment is handled, including the one `from_yaml` reads.

The `-m` and `-E` flags also accept the feature flags `graft merge` reads, which are `--skip-eval`, `--fallback-append`, `--go-patch`, `--multi-doc`, `--skip-vault`, `--skip-aws`, and `--skip-nats`.

## Merging first

Two flags let us query the result of a merge without piping through `graft merge`, and they work as they do in `graft jq`.

- `-m` or `--merge`
  This merges every input file with `graft merge`, resolves the `(( ))` operators, and runs the expression on the merged document.

- `-E` or `--eval`
  This resolves the operators in each input document on its own and runs the expression on every result.

The two flags cannot be combined. Because the merge builds a map, the merged document's keys come out in graft's usual order.

```sh
$ graft yq -m . base.yml overlay.yml
extra: true
meta: web
name: web-v2
z: 1

$ graft yq --prune z -m . base.yml overlay.yml
extra: true
meta: web
name: web-v2
```

## Sorting keys

Objects keep their input order by default, as yq does. `-S` sorts keys lexically, and `--sort-keys` takes an optional mode written with an equals sign.

| Form | Order |
|------|-------|
| `--sort-keys` or `--sort-keys=lexical` | Unicode code point |
| `--sort-keys=spruce` | Numeric keys first, then strings in natural order, which is the order `graft merge` writes |
| `--sort-keys=none` | The order the keys arrived in, which is the default |

```sh
$ graft yq -S . data.json
{
  "a": [
    1,
    2
  ],
  "b": 2
}
```

## Exit codes

Every failure exits 1, as it does in yq. That covers a usage error, an unreadable file, input that does not parse, an expression that does not compile, and a runtime error. With `-e`, the exit code is also 1 when nothing was produced or every output was null or false.

```sh
$ graft yq -e '.missing' config.yml
null
No matches found
```

Errors use graft's diagnostics, which are a bold red phrase, a colon, and the detail. Color on standard error follows `--color`, `--no-color`, `NO_COLOR`, and whether standard error is a terminal.

## Departures from yq

These are the places where `graft yq` deliberately differs from yq 4.54.1.

- Exact decimal arithmetic
  yq computes in int64 and doubles, and graft computes exactly. `0.1 + 0.2` is `0.3`. Passing `--float-numbers` gives yq's behavior back, so `0.1 + 0.2` prints `0.30000000000000004` again.

  ```sh
  $ graft yq -n '0.1 + 0.2'
  0.3

  $ graft yq --float-numbers -n '0.1 + 0.2'
  0.30000000000000004
  ```

- Regular expressions
  The regex builtins use PCRE syntax from graft's own engine, where yq uses Go's RE2. A pattern with lookbehind or lookahead works here, and it is a syntax error in yq.

  ```sh
  $ graft yq -n '"xyz" | sub("(?<=y)z"; "Q")'
  xyQ
  ```

- from_unix range
  `from_unix` fails on a value outside the range of int64 seconds, where yq saturates.

  ```sh
  $ graft yq -n '1e30 | from_unix'
  Runtime error (at <unknown>): from_unix: 1e30 is outside the range of int64 seconds
  ```

- Formats
  XML, TOML, CSV, TSV, properties, and the other formats yq reads and writes are out of scope. People ask most often about XML input and output, and `-o xml` is refused with the message shown above.

- JSON output of special numbers
  Both tools print not-a-number and infinity as `.nan`, `.inf`, and `-.inf` in YAML. In JSON output, yq fails with an error, while graft prints `null` for not-a-number and `1.7976931348623157e+308` and `-1.7976931348623157e+308` for the two infinities.

  ```sh
  $ graft yq -o json . n.yml
  {
    "a": null,
    "b": 1.7976931348623157e+308,
    "c": -1.7976931348623157e+308
  }
  ```

- The system operator
  yq's `system` operator is not in graft. `--security-enable-system-operator` is accepted so that scripts which pass it keep running, but `system(...)` fails with `system/1 is not defined`.

- Diagnostics
  Errors use graft's phrases in place of yq's `Error:` prefix. Exit codes are unchanged, so a script that tests `$?` keeps working, while a script that searches standard error for `Error:` does not.

- Extra flags
  `-S` with named modes, `-m`, `-E`, and `--repl` are graft's own. None of them collides with a yq flag.

- Names and help
  Only the exact name `yq` dispatches to this command, and a link named `yq-4` runs as graft. Both `yq --version` and `graft yq --version` lead with yq's version line and then print graft's own. Under the `yq` name, `-v` is yq's verbose flag, while under the `graft yq` form `-v` prints graft's version and `--debug` is the equivalent. `--help` shows graft's layout rather than yq's text. See [Aliases](aliases.md).

## Debugging with --repl

`graft yq --repl EXPRESSION FILE` opens the graft debugger on the files and runs the expression once. In a debug session, the `yq` verb runs a yq expression over the tree as it stands at the current step, `break yq:EXPR` stops where a query matches, and `config dialect yq` makes `select` and `watch` read yq expressions. The debugger holds the merged tree as a plain map, so comments and styles are not available there, and yq's metadata operators return null. The [debug command](cli/debug.md) page describes each verb.

## Compatibility

We measure compatibility against a coverage inventory of yq behaviors, where each entry passes when every differential case it names matches a golden recorded from the real yq 4.54.1. The yq report, `tests/query-parity/yq/report.txt`, has not been generated at the commit this page describes, so we publish no percentages for yq yet. The page will carry them, each with the phase it was measured in, once the report exists.

## Related

- [jq](jq.md) describes the sibling command for jq programs.

- [Aliases](aliases.md) covers running graft as `yq`.

- [merge](cli/merge.md) describes the merge that `-m` and `-E` run first.

- [debug](cli/debug.md) describes the debugger that `--repl` opens.
