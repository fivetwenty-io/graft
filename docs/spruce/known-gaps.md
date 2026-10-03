# Known Gaps

This page lists every open item between graft's current implementation
and full parity with spruce, plus a few internal graft loose ends that
fall outside parity scope but are worth recording. Each open entry states
current behavior, the behavior expected for parity (where spruce has an
equivalent), and the impact. A
[Deliberate divergences](#deliberate-divergences) section records the
places where graft intentionally does not match spruce and intends to
keep it that way. A separate [Resolved](#resolved) section
keeps a record of items that were open at some point and have since been
closed, so links from other pages into this file keep working.

## Open gaps

### mixed-key-type-map-encoding-order

**Narrowed in 1.32.0.** The ordering component of this gap is fixed:
graft now sorts keys on every YAML emit with a port of spruce's
comparator (`pkg/graft/keysort.go`) — numeric-looking keys first,
ordered numerically, then string keys in spruce's natural order
(digit runs numeric, non-letters before letters). Maps with bare
integer keys (`exit_codes: {0:, 1:, 2:}`, numbered curator actions),
integer-and-string mixes, and string keys with embedded digit runs
(`z1a`/`z2b`/`z10a`, `item2`/`item10`) all match spruce's key order
position-for-position; string-only maps are byte-identical. Pinned
by the byte-exact runner `tests/spruce-compat/run-key-order.sh`.

**Remaining open scope — key typing, quoting, and labels:** graft's
decoder coerces every key to a Go string as soon as the document is
parsed, so a bare `10:` and a quoted `"10":` are indistinguishable
by the time graft encodes. Three consequences:

- **Labels.** spruce re-renders typed keys from their value (`10:`
  bare; `1e3:` becomes `1000:`; `1.0:` becomes `1:`), while graft
  keeps the source spelling as a quoted string (`"10":`, `"1e3":`).
  Position matches; bytes differ.

- **Quoted-numeric mixed maps (provably unreachable).** spruce
  orders a bare `10:` in the numeric tier but a quoted `"10":` in
  the string tier; graft cannot tell them apart, and classifies
  every numeric-looking key into the numeric tier. For
  `{3: a, "10": b, 2x: c}` spruce emits `3, 2x, "10"` while graft
  emits `3, 10, 2x`. No comparator over coerced keys can satisfy
  both this case and the bare-key case; graft targets the bare-key
  ordering, which is what occurs in real kits and deployments.

- **Exotic typed keys.** spruce types hex `0x10:` and octal `010:`
  via YAML 1.1 (ordering them as 16 and 8), sorts bool keys as 0/1
  among the numbers, and places a null key after numbers but before
  strings. graft's coerced `"0x10"`, `"true"`, and `"null"` sort as
  words in the string tier.

**Mitigation:** for a byte-identical result across both tools, quote
*all* keys of the affected map. That moves every key into spruce's
string tier and equalizes the decoded key types downstream as well.
Quoting only the numeric keys is not equivalent — it changes their
spruce ordering from the numeric tier to the string tier.

### y-n-boolean-values-not-coerced

**Current behavior:** graft's YAML 1.1 compatibility layer coerces a
bare `yes`, `no`, `on`, or `off` value (any case variant) to a
boolean, but leaves a bare single-letter `y`, `Y`, `n`, or `N` as a
string: `a: y` merges to `a: "y"`.

**Expected behavior:** spruce's YAML 1.1-flavored parser treats the
single-letter forms as booleans too: `a: y` merges to `a: true`, and
`b: N` to `b: false` (confirmed against spruce 1.35.16).

**Impact:** documents using bare single-letter YAML 1.1 booleans get
a string where spruce produces a boolean, which can change comparison
and ternary results as well as output bytes. Rare in practice —
Genesis kits spell booleans `true`/`false` or `yes`/`no`. Quoting the
value (`"y"`) keeps it a string in both tools; spelling it `yes`
coerces in both.

The same holds for a key. `graft json --strict` fails on a `yes`, `no`, `on`, or `off` key, as spruce does, but it accepts a bare `y` or `n` key, which spruce refuses as a boolean.

### line-and-paragraph-separators-outside-quotes-and-blocks

**Current behavior.** graft reads a line separator (U+2028) or a paragraph separator (U+2029) as a line break inside a single-quoted scalar and inside a literal block scalar (`|`), which are the two places where spruce writes them. In every other place graft still reads the character as ordinary text, where libyaml reads a line break. A next-line character (U+0085) is already a line break everywhere, in both tools. We compared both binaries on each of the cases below, with U+2028 and with U+2029, and the two separators behaved the same way.

- Take a double-quoted scalar made of `a: "x`, the separator, two spaces, and `y"`. spruce reads it as `x`, the separator, and `y`, because libyaml drops the spaces as the indentation of a new line, and graft keeps the two spaces. When the separator is followed directly by text, as in `"x`, the separator, and `y"`, the two tools agree.

- In a folded block scalar (`>`), spruce drops the indentation after the separator and graft keeps it. For a line that holds `x`, the separator, two spaces, and `y`, spruce reads `x`, the separator, and `y`, and graft reads the same text with the two spaces kept. When the separator is followed directly by text at the start of the line, spruce fails with exit code 2 and `could not find expected ':'`, and graft accepts it.

- In a plain scalar, such as `a: x`, the separator, and `y`, spruce fails with exit code 2 and `could not find expected ':'`. graft accepts the file and keeps the character as part of the text.

- In an implicit key, such as `'a`, the separator, two spaces, and `b': 1`, spruce fails with exit code 2 and `mapping values are not allowed in this context`, because a line break cannot sit inside an implicit key. graft accepts the key and writes it back in the explicit `? key` form.

**Expected behavior.** Each of these reads exactly as spruce reads it.

**Impact.** A file has to hold a raw separator character in one of these positions to be affected, which is rare. The two double-quoted and folded cases are the ones to watch, because graft produces a different value without reporting anything. Writing the separator as the escape `\L` or `\P` in a double-quoted scalar reads the same way in both tools, and a single-quoted scalar or a literal block already agrees. `graft diff` shares the same reader, so it carries the same differences.

### load-reads-files-through-its-own-path

**Current behavior.** `(( load ))` reads the file it names with its own code path, which does not share every fix that `graft merge` and `graft json` got. Two differences remain.

- A file whose last value is a block scalar with an indentation indicator, such as `|2-`, fails to load with `unsupported root type in loaded content, only map or list roots are supported`. graft merge writes such a block for a string that starts with a space or a line break, so `k: " a\nb"` merges to `k: |2-` followed by the lines `   a` and `  b`. `graft merge` reads that output back and so does spruce, but `(( load ))` of the same file fails in graft. The same block in the middle of a file, with another key after it, loads correctly.

- An alias to an anchor that was defined inside another anchored collection still decodes as null. A loaded file with `x: &o` holding `p: &p v` and `q: *p`, followed by `r: *o`, gives `q: null` in graft where spruce gives `q: v`, and `a: &x [&x 1, *x]` loads as `[1, null]` where spruce gives `[1, 1]`. Merging the same files directly already gives spruce's values.

**Expected behavior.** `(( load ))` reads a file exactly as `graft merge` reads it.

**Impact.** Both cases are rare. The first one fails loudly, and the second one silently produces null. Until this is closed, we can pass the file to `graft merge` as another input instead of loading it, or we can put another key after the final block scalar and avoid aliases that point into an anchored collection.

### memory-use-on-very-large-single-line-inputs

**Current behavior.** graft needs a lot of memory for a file with a very long single line, because its YAML library builds a token list and a syntax tree at roughly 1 KB per token. We measured peak resident size with `/usr/bin/time -l` on three rejected inputs and on one valid input. The first line of `r2m3` and `r2m5` is `a: [[], [], ...` and runs to about 800,000 bytes, which is 400,000 tokens or more, followed by a `...#c` line in `r2m3` or a `...` line in `r2m5`, and then 500,000 lines of `# pad`. The third file, `r2m7`, is `a: 1` followed by a `...#c` line and the same kind of padding. The valid input is a single line of 800,000 `[]` tokens in a file of 3,200,004 bytes.

| Input | Size | Result in both tools | graft peak | spruce peak |
|-------|------|----------------------|------------|-------------|
| r2m3 | 3,800,018 bytes | exit 2 | about 580 MB | 50 MB |
| r2m5 | 3,800,016 bytes | exit 2 | about 400 MB | 48 MB |
| r2m7 | about 3 MB | exit 2 | about 285 MB | 26 MB |
| One line of 800,000 `[]` tokens | 3,200,004 bytes | exit 0 | 1.6 to 1.9 GB across runs | 1.37 GB |

The rejected inputs fail with the right exit code and spruce's kind of error. The cost sits in the step that finds the end of the first document, which tokenizes a growing prefix of the file whenever a line could end that document. On the valid input graft finishes in under 2 seconds, and spruce takes about 20 seconds on the same machine.

**Expected behavior.** On a rejected input we would like graft to stay close to spruce's peak, which is 50 MB or less. On a valid input the peak is already the same order as spruce's, so there is nothing to match.

**Impact.** Only files with a single line of hundreds of thousands of tokens are affected, and no real configuration looks like that. The valid-input peak is set by the YAML library, and neither tool can stay under a few hundred megabytes for such a file. A fix for the rejected case would need a streaming lexer, and goccy does not offer one, so we have chosen to record the cost and not to replace the lexer. Deeply nested input is no longer a concern, because the depth check stops it before anything is tokenized. The figures above came from a heavily loaded machine, so they move from run to run and from build to build.

## Deliberate divergences

Places where graft intentionally behaves differently from spruce. These
are not open gaps: the divergence is the desired behavior, and closing
it to match spruce would reintroduce the problem it fixes.

### merge-stdout-leads-with-document-start-marker

**Graft behavior:** invoked under its own name, `graft merge` writes a
`---\n` document-start line before the merged document
(`renderMergedTreeWithReport`, cmd/graft/deferred_report.go), so the
result pipes straight into another YAML document and
`--report-deferred`'s comment block has a header to sit under. `graft
fan` already prepended `---\n` per document, matching spruce, so this
aligns the two subcommands with each other. Invoked under the spruce
name (a `spruce` symlink, copy, or hardlink on `PATH`, which is the
Genesis drop-in deployment), `merge` leaves the marker off by default,
so its stdout matches spruce's byte for byte. `merge --no-doc-start` or
`GRAFT_NO_DOC_START=1` drops the marker under either name, and
`GRAFT_NO_DOC_START=0` or `--no-doc-start=false` brings it back under
the spruce name; see the [CLI reference](../reference/cli.md#graft-merge)
for the full precedence.

**Spruce behavior:** spruce's `merge` case writes bare
`fmt.Fprintf(os.Stdout, "%s\n", string(merged))` with no leading marker;
only its `fan` case prepends one. A byte comparison of `merge` stdout
therefore differs from graft's by exactly this one line when graft runs
under its own name, in graft 1.35.0 and later.

**Impact:** resolved under the spruce name as of 1.42.0, which is where
it mattered. From 1.35.0 through 1.41.0, Genesis prepended its own `---`
header to the output of `spruce merge --skip-eval -` when it wrote
`.genesis/config`, so graft's marker opened a second YAML document in
that file and `spruce json` refused to load it back. A spruce-named
graft now produces the same bytes real spruce would, and that file is
one document again. Under the graft name the extra line remains, and it
is harmless to any consumer that re-parses the output, because `---` is
YAML's document-start marker, not content, so the parsed document is
the same either way. Byte-level consumers comparing `graft merge`
stdout against `spruce merge` stdout can drop the line with
`tail -n +2` or pass `--no-doc-start`. See [CLI surface](cli-surface.md)
and the
[Genesis compatibility contract](genesis-compat-contract.md#output-byte-stability-across-versions)
for the full contract.

### named-insert-works-on-scalar-lists

**Graft behavior:** `(( insert after "<value>" ))` and
`(( insert before "<value>" ))` on a list of scalars anchor on the entry
value itself: with a base of `[checkout, build, deploy]`, inserting
after `"build"` places the new entries between `build` and `deploy`.
Matching compares strings only (a numeric `2` is never matched by
`"2"`), the first match wins on duplicates, and a missing anchor fails
the merge with `unable to find specified modification point with
'<value>'` — mirroring how the named delete already treats simple
lists, except that delete-if-present stays a silent no-op while a
missing insert anchor is an error. The keyed form
`(( insert after <key> "<value>" ))` is rejected on a list of scalars
with `unable to insert, because the keyed insertion point
'<key>: <value>' cannot target entries in a list of scalars`.

**Spruce behavior:** spruce (verified against v1.34.1) routes every
named insert through map-key lookup, so on a list of scalars the merge
always fails with `original object is a string, not a map - cannot
merge by key`, even though its documentation shows the named form.

**Impact:** overlays can position new scalar entries relative to
existing ones instead of falling back to numeric indices. Documents
that relied on the spruce error to catch a misplaced marker now merge
successfully on lists of scalars.

### insert-duplicate-check-covers-first-entry

**Graft behavior:** the duplicate detection for keyed inserts treats a
new entry whose identifier matches the *first* entry of the original
list like any other collision: inserting `name: alpha` into
`[alpha, bravo, charlie]` fails with `unable to insert, because new
list entry 'name: alpha' is detected multiple times`.

**Spruce behavior:** spruce's duplicate check tests the found index
with `> 0`, so a collision at index 0 goes undetected and the
duplicate entry is silently inserted.

**Impact:** an insert that would silently produce two entries with the
same identifier under spruce now fails loudly. This is an off-by-one
fix, not a semantic redesign; only index-0 collisions change behavior.

### operator-under-duplicate-list-name-is-an-error

**Graft behavior:** when entries of the same list carry the same
`name`, `key`, or `id` value, and an operator appears anywhere beneath
any of them, the merge fails with one error per affected operator:

```
 - $.jobs: duplicate name "alpha" at jobs.0, jobs.1 makes the operator
   at $.jobs.0.cmd unaddressable; give each entry a unique name
```

Entries sharing a name with no operator beneath them are untouched, so
a list that repeats a name merges exactly as it always has. Only the
ambiguous-operator combination is rejected.

**Spruce behavior:** spruce (verified against a build of
geofffranks/spruce at `d1b00ad`) accepts the document and exits 0 with
silently corrupt output. A name-keyed cursor resolves through
`listFind`, which returns the first entry carrying that name, so every
operator under every entry sharing that name canonicalizes to the same
path, and the operators overwrite one another in the dataflow map. The surviving
operator writes its value into the **first** entry's slot, and the
other entries keep their raw operator text. For three entries named
`alpha` whose `cmd` values are `(( grab meta.first ))`,
`(( grab meta.second ))`, and `(( grab meta.third ))`, spruce emits
`THIRD` as `jobs.0.cmd` and leaves `jobs.1.cmd` and `jobs.2.cmd`
unevaluated. graft produced byte-identical output before this change.

**Impact:** documents that hit this already had one entry's computed
value written into a different entry, with no diagnostic. They now fail
loudly and point at the entries to rename. Every other document is unaffected: a differential sweep of
both binaries over the repository's 95-document YAML corpus found no
behavioral change outside the `shuffle` operator, whose output is
random by design and differs run to run on either binary.

### duplicate-mapping-keys-are-an-error

**Graft behavior.** `graft merge` and `graft json` fail with exit code 2 on a mapping that defines the same key twice. The message names both places, as in `mapping key "a" already defined at [1:1]`. That holds for a block mapping, for a flow mapping such as `a: {b: 1, b: 2}`, and for a mapping inside a list. `graft diff` still reads such a file, and it prints the same report as `spruce diff`.

**Spruce behavior.** spruce merges `a: 1` followed by `a: 2` to `a: 2`, and `spruce json` gives `{"a":2}`, so the last value wins and nothing is reported.

**Impact.** We keep this on purpose, because failing loudly is better than silently choosing one of two values. A repeated key is almost always a copy and paste slip or a bad hand merge, and the value that wins in spruce is rarely the one the author meant. A file that relies on the last value winning needs the earlier definition removed before graft will read it.

### json-and-merge-keep-y-and-n-keys

**Graft behavior.** graft never changes the type of a mapping key. The YAML 1.1 compatibility layer only touches values, so a key spelled `y`, `n`, `yes`, `no`, `on`, or `off` stays the string it was written as, in any case. `graft json` prints `{"y":1}` for the file `y: 1`, and `graft merge` keeps the key and quotes it, as in `"y": 1`.

**Spruce behavior.** spruce reads those keys as booleans and writes them back as `true` or `false`. Both `y` and `yes` give `true`, and both `n` and `no` give `false`, in any letter case, so `spruce json` prints `{"true":1}` for the file `y: 1`, and `spruce merge` prints `true: 1`. The words `t`, `f`, `T`, and `F` stay strings in both tools, and a key spelled `true` or `false` comes out the same in both. We compared both binaries on all 24 spellings.

**Impact.** The value half of this is a separate open gap, described in [y-n-boolean-values-not-coerced](#y-n-boolean-values-not-coerced), where graft already agrees with spruce on `yes`, `no`, `on`, and `off`. For keys, spruce's rewrite loses data, since the file `y: 1` followed by `true: 2` merges to the single key `true: 2`, and a `grab y` of a key spelled `y` fails there with "could not be found". graft keeps both keys and resolves the path. Matching spruce would make graft turn a key such as GitHub Actions' `on:` into `true:`, so we keep keys untouched, and `graft json` agrees with `graft merge` on every key. This is also why the [mixed-key-type](#mixed-key-type-map-encoding-order) entry says typed keys can't be told apart from quoted ones. The exception is `graft json --strict`, which does look at key types. It refuses a `yes`, `no`, `on`, or `off` key, as spruce does, and it accepts a bare `y` or `n` key.

## Resolved

Items that were tracked as open gaps and have since been closed. Kept
here, under their original heading, so links from other pages that
point at a specific gap keep resolving to the right place.

### stringify-block-scalar-style

**Resolved.** graft now chooses every string's output style with a port of the scalar-style rules in spruce's YAML emitter, so `(( stringify ))` of a map or list comes out as a literal block exactly as spruce writes it. Earlier, goccy wrote such a string as a quoted flow scalar whenever its lines held `": "`, as every stringified map's lines do. The tables in `pkg/graft/yaml_scalar_style_test.go` pin this.

One case is still different. `(( stringify ))` of a single scalar returns the raw string, where spruce marshals the value into a literal block with a trailing newline. For `s: hello` and `r: (( stringify s ))`, graft writes `r: hello`, and spruce writes `r: |` followed by the line `  hello`, so its value is `hello` with a newline at the end. A number behaves the same way, so `(( stringify num ))` of `num: 5` gives the quoted string `"5"` in graft and a literal block holding `5` and a newline in spruce. A map or a list matches in both tools.

### scalar-array-default-merge-replaces-instead-of-inlining

**Resolved.** The scalar-array path's default strategy now merges
pairwise by position, matching spruce's `inline` fallback: with
`base.yml` holding `f: [a, b, c]` and `overlay.yml` holding `f: [X]`,
both tools produce `f: [X, b, c]`. An explicit strategy option can
still request replace, and `--fallback-append` is unchanged.

### quoted-boolean-strings-coerced-to-booleans

**Resolved.** graft's YAML 1.1 compatibility layer now inspects quoting
at the AST level: a bare `yes`, `no`, `on`, or `off` still coerces to a
boolean (matching spruce's YAML 1.1-flavored parsing), while a quoted
form such as `a: "yes"` stays the literal string through merge, json,
and output, byte-identical to spruce. Both `graft merge` and
`graft json` parse through the same compatibility pipeline, so the two
subcommands agree on value types. Pinned by tests in
`pkg/graft/yaml_spruce_parity_test.go` and `pkg/graft/json_test.go`,
plus binary-comparison fixtures in the operator parity corpus.

### trailing-newline-byte-parity-unverified

Resolved. A live byte-for-byte comparison of built spruce and graft
binaries confirmed identical output tails across single-doc merge,
multi-doc merge, json (single and multi-doc), `--skip-eval`, stdin
merge, and diff invocations. The same sweep exposed and fixed a crash
on blank, comment-only, and null (`---` alone) input files, which both
tools now handle by producing `{}` for that document.

### vault-target-extraction

**Resolved.** The vault operator's target-extraction code used to be an
explicit placeholder that always returned an empty string for a
non-empty `@target`, which risked a wrong-target cache-key collision for
multi-target lookups. The target is now carried through to `Run` and
resolved against `internal/backends/vault.DefaultPool`, so
`(( vault@production "path:key" ))` reaches the client configured by
`VAULT_PRODUCTION_ADDR` and `VAULT_PRODUCTION_TOKEN`. A target with no
matching configuration errors at evaluation time and names the variables
it expected, rather than falling back to the default client.

The spruce spelling that puts the target on the path
(`(( vault production@"path:key" ))`) is rejected with a message
redirecting to the operator-name spelling.

### aws-nats-target-extraction

**Resolved.** Same fix as [vault-target-extraction](#vault-target-extraction):
`(( awsparam@<target> ... ))`, `(( awssecret@<target> ... ))`, and
`(( nats@<target> ... ))` resolve their target against
`internal/backends/aws.ClientPool` and
`internal/backends/nats.ClientPool`, configured through
`AWS_<TARGET>_REGION` (and the profile, role, and access-key variants) and
`NATS_<TARGET>_URL`.

### or-operator-unregistered

**Resolved.** No standalone `or` operator implementation, registered or
commented out, exists in the current source tree. Logical-or
short-circuit behavior is expressed with `||` (`OrElseOperator`), which
graft already supports and which spruce's own operator set relies on in
the same way, so there was never a functional gap here.

### env-feature-flags-not-wired-to-cli

**Resolved.** `--config <path>` on the root command, plus `GRAFT_*` and
`GRAFT_FEATURE_*` environment variables, are now resolved once per CLI
invocation, `--config` given or not: an explicit config file or
`config.DefaultConfig()` as the base, `internal/config.ApplyEnv` layered
on top, and `internal/features.DefaultFlags().LoadFromEnv()` for feature
flags. Both are wired into engine construction for `merge`, `fan`, and
`vaultinfo` (not `diff` or `json`), giving the intended
env-over-file-over-default precedence. Not every resolved setting
changes observable merge behavior yet — the Parallel section and the
`caching` feature flag do, most other config sections don't yet. See
[Configuration reference](../reference/config.md) for the complete,
field-by-field breakdown of what's wired and what isn't.

### array-marker-handling-diverges-under-skip-eval

**Resolved.** Array-merge markers (`append`, `prepend`, `replace`,
`merge`, `merge on <key>`, `insert`, `delete`) are now always resolved
during the merge step, regardless of `--skip-eval`, matching spruce:
`--skip-eval` only disables the scalar operator evaluator, never the
merger. The merge builder's marker-detection logic
(`needsLegacyMerger`) recognizes every marker family and routes any
array carrying one through the real merger unconditionally, so no
marker silently falls through to a plain inline replace anymore.

### null-rendering-parity-unverified

**Resolved.** Confirmed by running both `spruce merge` and `graft
merge` binaries over equivalent fixtures, and pinned by a test in
`pkg/graft/yaml_spruce_parity_test.go`: every null representation
(explicit `null`, `~`, or an empty scalar) marshals to the bare word
`null`, matching spruce byte-for-byte, and a string value that happens
to read `"null"` or `"~"` stays quoted rather than being rendered as an
unquoted null-like token.

### map-key-order-parity-unverified

**Resolved.** graft emits map keys in spruce's order on marshal,
regardless of the native Go map's undefined iteration order in memory.
An earlier version of this entry claimed both encoders sort
"alphabetically" — that was wrong on both sides: spruce's
`yaml.v2`-family encoder uses a two-tier numeric-then-natural sort
(digit runs compare numerically, non-letters sort before letters), and
graft's encoder used to be purely lexicographic, so key sets like
`item9`/`item10` or `int_val`/`int64_val` diverged. Since 1.32.0 graft
ports spruce's comparator (`pkg/graft/keysort.go`); order parity is
pinned by `TestMarshalYAML_SpruceKeyOrder` in
`pkg/graft/yaml_spruce_parity_test.go` and the byte-exact runner
`tests/spruce-compat/run-key-order.sh`. Residual typing/quoting/label
differences are tracked in
[mixed-key-type-map-encoding-order](#mixed-key-type-map-encoding-order).

### array-fallback-warning-parity-unverified

**Resolved.** Verified against the spruce binary and pinned by
`pkg/graft/merge_fallback_warning_test.go`: graft emits the same stderr
warning pair spruce does when the default array-merge fallback
(key-merge, then inline) triggers, for every document-merge step where
the fallback applies, including the first document merged into the
initial empty root. The warning text intentionally never matches the
`^ - \$\.` error-line pattern Genesis's stderr scraping depends on.

### parallel-eval-determinism-unverified

**Resolved.** `pkg/graft/parallel_determinism_test.go` runs a full
parallel merge 40 times over representative multi-document,
operator-heavy fixtures and asserts every run's marshaled YAML output is
byte-identical to the first. Output ordering (map keys and list
elements) is stable across repeated runs regardless of worker count, so
enabling or tuning parallel evaluation changes only how fast a merge
runs, never what it produces.

### default-concurrency-hardcoded

**Resolved.** The CLI no longer uses a fixed worker count for parallel
evaluation. `resolveConcurrency` derives the default from an explicit
`cfg.Parallel.MaxWorkers` (set via config file or
`GRAFT_PARALLEL_MAX_WORKERS`) when present, otherwise from
`runtime.NumCPU()` floored at `1`, so the worker pool now scales with
the host machine by default.

### sort-post-processing-silently-skips-two-error-cases

**Resolved in 1.32.0.** A queued `(( sort ... ))` marker whose path
fails to resolve after the merge (for example, because a prune removed
it), or resolves to a non-list value, now fails the merge with exit
code 2 and spruce's exact error text inside the standard
`N error(s) detected:` framing, instead of passing the document through
unsorted. Sort application also moved from the evaluator into the merge
builder's post-processing, after all pruning (including `--prune`
flags) and before cherry-picking, matching spruce's
prune-sort-cherry-pick order; `--skip-eval` runs the identical code
path, which also closed a third, undocumented case where a bad sort key
was silently ignored under `--skip-eval`. Pinned by
`pkg/graft/sort_postprocess_parity_test.go` and six binary-comparison
fixtures in the operator parity corpus. Note the deliberate behavior
change: documents that previously merged successfully with a dangling
or mistyped sort marker now fail, exactly as they do under spruce.

### raw-env-operator-missing

**Resolved in 1.32.0.** `(( raw_env $NAME ))` is now registered, with
spruce's exact semantics: it resolves a single environment-variable
argument to its raw string value, bypassing the YAML type coercion the
normal `grab $NAME` substitution applies (`PORT=8080` stays the string
`"8080"`), treats a set-but-empty variable as a valid empty string, and
errors with `environment variable $NAME is not set` for an unset one.
`(( raw_env $A || $B ))` keeps the raw-string behavior on either
side, while a non-environment-variable fallback such as a literal
still coerces normally. Pinned by `pkg/graft/operators/op_raw_env_test.go`
and seven binary-comparison fixtures in the operator parity corpus.
With this operator in place, no spruce operator is missing from graft.

### convenience-api-pending

**Resolved in 1.32.0.** The commented-out convenience functions are now
implemented: `graft.QuickMerge(yamls ...string)` and
`graft.QuickMergeFiles(paths ...string)` each build a default engine,
merge their inputs left to right with full operator evaluation, and
return the marshaled YAML bytes. Zero arguments yield `"{}\n"`.
Pinned by `pkg/graft/quick_merge_test.go` and runnable examples in
`pkg/graft/examples_doc_test.go`; documented in the
[engine API guide](../developer-guide/library-api/engine.md).

### wildcard-path-matching-stub

**Resolved in 1.32.0.** History path filters
(`graft.HistoryFilter.Path`) now match with the same wildcard grammar
the rest of graft uses instead of a literal string comparison: exact
paths, `*` (one segment), `**` (any depth), `[N]`/`[*]` index patterns
(matching the dotted-numeric form recorded paths use), and
`[key=value]` selectors, plus segment-aware prefix matching so a filter
of `db` still covers `db.host` without also matching `dbextra`.
Pinned by `pkg/graft/document_memory_path_filter_test.go`.

### no-cache-annotation-stub

**Resolved in 1.32.0.** The partially built annotation is now the
`:nocache` expression modifier, implemented end to end: the parser
accepts `(( name:nocache args ))` (unknown modifiers are parse
errors), the flag travels through `Opcall` and the evaluator to every
backend cache, and a modified vault/awsparam/awssecret/nats call
neither reads from nor writes to the shared per-run cache while plain
calls keep sharing it under unchanged keys. See
[Expression Modifiers](../reference/expression-modifiers.md) for the
grammar, semantics, and spruce-compatibility notes. Pinned by
`pkg/graft/nocache_test.go`, `pkg/graft/nocache_backend_test.go`, and
`pkg/graft/operators/operator_nocache_support_test.go`.

## Related documents

- [Merge semantics](merge-semantics.md)

- [YAML formatting differences](yaml-formatting.md)

- [Genesis compatibility contract](genesis-compat-contract.md)
