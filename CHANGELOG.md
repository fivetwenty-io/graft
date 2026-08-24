# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.38.0] - 2026-08-23

A follow-on to the theming release. The debugger REPL now colorizes the
line as it is typed, themes can be set from a config file as well as
from `--theme` and `GRAFT_THEME`, and the escape stripper that sanitizes
error text before it reaches the terminal now removes every escape byte
rather than only well-formed sequences.

### Added

- The debugger's `--theme`/`GRAFT_THEME` resolution gained a config-file
  tier: a `ui.theme` key in the first of `./graft.yaml`,
  `$HOME/.graft/config.yaml`, or `/etc/graft/config.yaml` sets the
  `graft debug`/`graft merge --interactive` theme when neither `--theme`
  nor `GRAFT_THEME` is set. Precedence is now flag, then env, then
  config file, then the `auto` default. This is a standalone,
  theme-only reader; it does not go through `--config` or activate any
  other configuration field. See [`graft debug`'s Colors and Themes
  section](docs/user-guide/cli/debug.md#colors-and-themes) for the full
  behavior.

- The `graft debug` REPL now colorizes the command line as it is typed,
  in a new `roleInput` style distinct from the `graft>` prompt's own
  style. `config theme <name>` restyles the in-progress line along with
  the prompt, so both switch palettes together.

### Fixed

- `ansi.StripEscapes` (used to sanitize error text such as `(( param ))`
  messages before it reaches the debugger's terminal output) now
  removes every ANSI/ECMA-48 escape byte, including ones that do not
  begin a recognized, properly terminated CSI/OSC/DCS/SOS/PM/APC
  sequence. Previously an unrecognized escape byte was left in place,
  which a crafted document value could exploit two ways: a doubled
  `ESC ESC \` let the second `ESC \` be consumed as an ordinary two-byte
  escape, leaving the first, unrecognized `ESC` to land directly against
  trailing OSC- or CSI-shaped text and form a live, terminal-honored
  escape sequence that was never a complete sequence in the source
  document; and a bare, unterminated introducer such as `ESC ]` reached
  the terminal unchanged, where it would swallow output up to the next
  BEL or string terminator as a title string. The escape byte is now
  always dropped; any text that followed it is kept as plain text, which
  has no terminal meaning without a genuine escape byte in front of it.

## [1.37.0] - 2026-08-23

A theming release. The debugger REPL gained themeable colorized
output: every category of session output now carries its own color,
with `dark`, `light`, `mono`, and an `auto`-detecting default that
reads the terminal's background. Color-off sessions (piped,
redirected, or `--no-color`) are guaranteed escape-free.

### Added

- `graft debug` and `graft merge --interactive` colorize every category
  of session output (paths, values, successes, warnings, errors, YAML
  dumps, and more), with the `graft>` prompt in a style reserved for it
  alone so no output line can be mistaken for the command line. A new
  root `--theme` flag (also settable with `GRAFT_THEME`) picks `dark`,
  `light`, `mono`, or the default `auto`, which detects the terminal's
  background and falls back to `dark`; `config theme [name]` switches
  the palette mid-session. `config` output never colors a value, so a
  live `vault.token` is never the only unstyled text on its line. See
  [`graft debug`'s Colors and Themes
  section](docs/user-guide/cli/debug.md#colors-and-themes) for the full
  behavior.

### Fixed

- `graft debug ... > out.txt`, run from a terminal, no longer leaks
  ANSI escape codes from engine-rendered error messages into the
  redirected file. Those errors carried their own coloring baked in
  before this release; the debugger now strips it at the boundary
  before writing to its own output, so a color-off session (piped,
  redirected, or explicitly `--no-color`) always emits zero escape
  bytes. Layout, wording, and ordering of color-off output are
  otherwise unchanged.

## [1.36.0] - 2026-08-23

A debugger release. The new `tree` command draws the document, or any
subtree of it, as a colorized box-drawing tree at the session's current
step, so structure is readable without paging through YAML. Annotation
flags fold provenance into the same view: which file set a value, and
what it looked like at each earlier step.

### Added

- The debugger gained a `tree` command: a colorized box-drawing tree of
  the current document at a path, with `--annotate`/`--history` showing
  per-path provenance up to the session's current step. Depth is capped
  with `--depth`, keys alone are listed with `--keys`, and `--no-color`
  drops the ANSI styling.

## [1.35.0] - 2026-08-21

A resilience release. A merge no longer has to be all-or-nothing:
`--defer-on-error` retries around failing operators, the `--skip-*`
flags defer whole secrets backends, and both leave re-mergeable
expressions in the output with the new exit code 3 marking a partial
document. The debugger gains the same adaptive loop plus prune
awareness, and several long-standing merge bugs (null overrides,
prune history, composed vault paths) are fixed.

### Added

- A global `--no-color` flag, available on every command, that disables
  ANSI color outright. It always wins, including over an explicit
  `--color`. The `--color` flag itself now takes `on`, `off`, or `auto`
  (bare `--color` means `on`), accepts the value with either `=` or a
  space, and lives on the root command, so any subcommand understands
  it. When neither flag is given, color resolves automatically: the
  `NO_COLOR` and `TERM=dumb` conventions are honored first, then
  whether the output stream is a terminal — stderr for diagnostics, and
  stdout for `graft diff`'s rendered output, so a redirected diff stays
  free of escape codes even from an interactive shell.

- `graft vaultinfo --resolve` performs live Vault lookups instead of
  the default offline scan, so paths composed from other vault-sourced
  values are reported fully concrete when a Vault is reachable. The
  flag is opt-in; without it vaultinfo never contacts Vault, even when
  one is configured and reachable.

- `graft debug` now accepts `--prune` and `--cherry-pick`, matching
  `merge --interactive`. While stepping, `output`, `export`, and
  `history` all show the document before those flags are applied, and
  a new `prune-report` REPL command lists what they would remove once
  the session is fully evaluated, so the views no longer disagree.

- A new `autodefer` command in `graft debug` runs the same adaptive
  defer-on-error loop from the current session state, folding each
  newly deferred path into the session's defer set with its
  root-cause reason, so `output`, `export`, `history`, and later
  stepping stay consistent. Manually deferred paths are respected and
  never re-attempted, a true cycle remains a hard failure with its
  original error, and `inspect` now lists the session's deferred
  paths with their reasons.

- `graft merge --defer-on-error` (alias `--adaptive`) retries a
  failing merge by wrapping failing operator expressions in
  `(( defer ))` and re-running until the document merges or stops
  making progress, so one unreachable backend no longer fails the
  whole document. Each deferred key is reported as a YAML comment in
  the output itself, attributed to its root-cause error rather than
  the cascade it triggered; `--report-deferred=beginning|inline|end|none`
  controls placement, defaulting to a block at the top. A data-flow
  cycle remains a hard failure with its original error, and a merge
  with nothing to defer produces byte-identical output to a plain
  merge. Any merge whose output contains deferred expressions,
  whether from `--defer-on-error` or the skip-backend flags below,
  exits with the new code 3, so scripts can tell a partial document
  from a complete one even with reporting silenced; `REDACT=1` still
  exits 0. One known limitation, shared with `(( defer ))` itself: a
  value that embeds a deferred expression mid-string, such as a
  `(( concat ))` of one, is emitted as plain text and is not
  re-evaluable on a later merge.

- `graft merge` accepts `--skip-vault`, `--skip-aws`, and
  `--skip-nats`, one flag per secrets backend and freely combinable.
  A skipped backend's operator calls — `(( vault ))`, `(( vault-try ))`,
  `(( awsparam ))`, `(( awssecret ))`, and `(( nats ))` — are left
  intact in the output, exactly as if wrapped in `(( defer ))`, with
  `:nocache` and `@target` modifiers preserved, so the document can be
  merged again once the backend is reachable. Values composed from a
  deferred lookup, such as a `(( grab ))` of a skipped vault value,
  defer along with it instead of erroring. OpenBao speaks the Vault
  API through the same operator, so `--skip-vault` covers it. The
  `REDACT` environment variable is unchanged and still substitutes
  `REDACTED`; when both are given, `REDACT` wins. A skip-flag merge
  that actually deferred something exits 3 and participates in
  `--report-deferred`, with the skip flag named as the reason; one
  with nothing to skip exits 0 as usual. Library callers get
  the same choice through the new `WithRedact` engine option, which
  pairs with the existing `WithSkipVault`, `WithSkipAws`, and
  `WithSkipNats` options to pick redaction over deferral.

### Changed

- `graft merge` now begins its output with a `---` document-start
  line, so the result can be inlined into or concatenated with other
  YAML documents without hand-editing. The same applies to the files
  `graft fan --output-dir` writes; fan's stdout already carried the
  marker per document. This is a graft addition, not spruce parity —
  spruce's `merge` emits bare YAML — and it is invisible to anything
  that re-parses the output, including every Genesis pipeline in the
  compat contract. `graft json`, `graft vaultinfo`, and `graft diff`
  are unchanged, and the output cache never replays entries written
  before this change.

- Deleting a list entry that is not there is no longer an error. A
  `(( delete "name" ))` or `(( delete <value> ))` whose target is
  absent from the base list now merges through silently, so an overlay
  can say "make sure `cflinuxfs2` is gone" without caring whether it
  was ever present. This is a deliberate divergence from spruce, which
  rejects the merge. Deleting from an empty list is likewise a no-op,
  and an overlay list containing only delete markers no longer
  materializes the key at all when the base never had it — a base
  without `features:` stays without it, while a base with
  `features: []` keeps its empty list. `(( insert ))` with a missing
  anchor and out-of-range index operations still error as before.

- `(( insert after "x" ))` and `(( insert before "x" ))` now work on
  simple lists, anchoring on the entry whose value is `x`; on lists of
  maps the anchor still matches the `name` key (or the key given
  explicitly). Comparison is by string, the first match wins, and a
  missing anchor is still an error, since an insertion point is a
  positional claim. Spruce rejects the named form on scalar lists
  outright with a key-merge error, so this is a deliberate divergence,
  companion to delete-by-value. Alongside it, the duplicate check for
  named inserts now covers the first list entry: an inserted entry
  colliding with it used to slip through and duplicate silently, a
  bug shared with spruce.

### Fixed

- An argument-bearing `(( delete ... ))` marker outside a list is now
  rejected with `inappropriate use of (( delete )) operator outside of
  a list` instead of surviving the merge as literal text. The marker
  only has meaning as a list entry; used as a map value over a scalar,
  map, or absent key, both merge paths used to copy the marker string
  into the output as data. Both paths now produce the identical error,
  whichever document position carries the marker, including the first
  file. Spruce rejects the same input too, from its evaluation phase
  with different wording, so no valid spruce document is affected. The
  bare `(( delete ))` form keeps its spruce-parity passthrough as
  literal text.

- An unregistered operator call with arguments now passes through the
  merge intact. The passthrough rendered every non-literal argument as
  `...`, so `(( bogus foo ))` came out as `(( bogus ... ))`, corrupting
  the expression for any later merge that does know the operator.
  Arguments now round-trip as their real source text: references by
  path, environment variables as `$NAME`, literals quoted, nested
  calls and `||` alternatives recursed.

- A key explicitly overridden to null is kept as a null, matching
  spruce. The simple-merge fast path used to read a nil overlay value
  as "delete this key", so `b: null` in an overlay silently removed
  `b` — but only for an existing map key, and only when the documents
  happened to route through the fast path; new keys, list elements,
  and any merge involving an array operator, prune, or sort already
  preserved the null. Both merge paths now agree, and `--history`
  credits the overlay file instead of rendering the change as
  `<pruned>`. Anything that relied on `key: null` as an undocumented
  delete idiom will now see the key survive with a null value; the
  supported way to remove a key remains `--prune` or `(( prune ))`.

- Merge history now reports keys removed by a `(( prune ))` operator.
  The operator queues its path inside the engine, so history's
  per-file replays only saw the key vanish as a nil value, rendering
  `~` — indistinguishable from a key legitimately set to null. History
  entries now carry an explicit removed flag: `Final` prints
  `<pruned>`, `--show-changes` counts the key as removed rather than
  changed, and `--trace-path` shows the marker at the step where it
  arrived. A key set to an explicit YAML null still renders `~`. Also
  fixed alongside: a post-phase history entry was labeled `<pruned>`
  even when it was a parent map that merely shrank; only genuinely
  removed paths carry the label now.

- `graft vaultinfo` no longer reports corrupted keys when a vault path
  is composed from another vault-sourced value. The offline scan skips
  lookups and used to substitute the literal `REDACTED`, which then
  leaked into composed paths as `secret/paths:REDACTED`, a key that
  does not exist. A skipped lookup now leaves a symbolic reference in
  its place, so the same document reports
  `secret/paths:<secret/paths:root>`, naming the lookup the segment
  comes from. This covers `vault-try` too. Document values themselves
  are unchanged: `REDACT=1 graft merge` output still reads `REDACTED`,
  and fixtures without composed paths are byte-identical. The symbolic
  form follows the value through `(( grab ))` as well: an alias of a
  redacted lookup, a chain of aliases, or an inline `(grab ...)`
  argument all report the composed key symbolically instead of
  corrupting it with the flat text. A path built with `(( concat ))`
  remains the known limitation and still shows `REDACTED`.

- `NO_COLOR` and `TERM=dumb` are honored again. The environment checks
  ran at startup but the command-line color handling then overwrote
  their result, so graft colored its diagnostics anyway. The precedence
  is now: an explicit `--color`/`--no-color` first, then the
  environment, then terminal detection.

### Documentation

- Documented building a Vault path from another Vault lookup — a
  `(( vault ))` whose path embeds a `(( grab ))` of a value that is
  itself fetched from Vault — with a regression test and fixture
  (`assets/vault/self-reference.yml`) pinning the resolution order.
  `vaultinfo` reports such composed paths with symbolic references,
  per the fix above.

- Added a Delete-if-Present section to the array merging guide covering
  the scalar-list delete semantics above, with the empty-list,
  absent-key, and mixed-overlay cases spelled out.

## [1.34.1] - 2026-08-20

### Fixed

- A `(( prune ))` that overwrote a value an earlier document had set left
  the marker in place instead of queueing the path and keeping the value.
  Anything reading that path, such as `(( grab ))`, then read the literal
  `(( prune ))` text rather than the value, and under `--skip-eval` the
  marker reached the output. Multi-document files felt this most, since
  each document is an overlay on the ones before it. Spruce queues the
  path and leaves the earlier value in place for the rest of the merge;
  graft now does the same, including for a `(( prune ))` merged in by
  `(( inject ))`.

## [1.34.0] - 2026-08-19

### Added

- `graft debug` now runs its prompt through a real line editor when it is
  attached to a terminal. Up and Down recall earlier commands, Ctrl+R
  searches them, and Tab completes command names, document paths from the
  tree at the current step, set breakpoints for `unbreak`, and the known
  keys for `config`. Recall persists in `~/.graft/debug_history`, created
  readable only by its owner, and a line that sets a secret such as
  `config vault.token` is kept out of it. Piped and redirected input is
  read exactly as before.

### Fixed

- `graft debug`'s `history` command now applies the session's deferred
  paths, as `step` and `continue` already did. Previously an operator the
  session could not resolve, such as an unreachable Vault path or an
  unfilled `(( param ))`, aborted the recompute that history tracking
  depends on, so `history` reported that operator's error for every path
  asked about, including unrelated ones. Deferring the offending path had
  no effect because the command rebuilt the merge without consulting the
  deferred set.

### Documentation

- Added [Inspecting a Merge](docs/examples/inspecting-a-merge.md), a
  walkthrough that debugs a failing merge from its first symptom to a
  full answer, with runnable fixtures in `examples/inspecting-a-merge/`.

- Documented how `REDACT` lets a whole debug session tolerate unreachable
  Vault paths, and how `graft diff` decides whether to color its output.

## [1.33.0] - 2026-08-17

A performance release. Output is byte-for-byte identical to 1.32.2
across the full compatibility corpus — every change below was gated in
CI on producing the exact stdout, stderr, and exit code of the previous
release — but merges are much faster: a heavy Genesis-style merge (one
large manifest plus 45 overlays) drops from 1.77 s to 0.23 s, and to
about 30 ms on a repeat run with the new persistent cache enabled.
The same merge takes spruce about 1.25 s.

### Added

- Persistent merge cache (opt-in)

  Set `GRAFT_CACHE_L2_ENABLED=true` (or `cache.l2_enabled: true`) and
  graft caches work across invocations on disk, in two layers. The
  output layer replays a previous run's exact stdout and stderr when a
  merge is repeated with byte-identical inputs, identical flags, and
  the same graft version — only *pure* invocations are stored, so any
  operator that consults an external system (`vault`, `awsparam`,
  `awssecret`, `nats`), the filesystem (`file`, `load`), the
  environment (`raw_env`, `$VAR`), or randomness (`shuffle`), and any
  control-flow document, disqualifies a run from output caching. The
  parse layer stores each document's parsed tree keyed by its content
  hash, so runs that miss the output layer still skip re-parsing
  unchanged documents. Keys hash content, never paths or mtimes, so
  Genesis-style temp files hit across runs and any edit misses.

  A cached result is guaranteed byte-identical to an uncached run; a
  new CI gate (`scripts/cache-identity.sh`) proves this across the
  whole example corpus on every push. Debug and trace runs bypass the
  cache, and cache trouble (unwritable directory, corrupt entry) is
  never an error. Entries live under `GRAFT_CACHE_L2_PATH`, defaulting
  to the OS user cache directory (`~/.cache/graft` on Linux,
  `~/Library/Caches/graft` on macOS) — an empty `cache.l2_path` no
  longer fails validation. Entries expire after seven days as
  housekeeping. See [Caching](docs/features/extras.md#caching).

- `graft cache stats` and `graft cache clear` subcommands

  Report per-layer entry counts and sizes for the persistent cache,
  and drop all stored entries.

### Changed

- The merge pipeline is roughly 7.5× faster on operator- and
  document-heavy workloads, independent of any caching: merge-phase
  regexes are compiled once, the two post-parse compatibility walks are
  fused into one, string screening happens at the byte level before
  expensive quote and array probing, overlays merge into the base
  in-place instead of deep-copying it, parse fan-out is bounded to the
  available cores, and costly debug dumps are gated on the debug flag.
  With this release a plain uncached `graft merge` outruns spruce
  about 5× on heavy inputs.

- Parallel evaluation hardening: an explicit
  `GRAFT_FEATURE_PARALLEL=false` now disables parallel evaluation even
  though the config default is `true` (either kill switch set to
  `false` wins), warning suppression is race-free under concurrent
  workers, and the scheduler panics loudly if its single-threaded
  dependency computation is ever entered concurrently instead of
  silently corrupting dependency edges.

### Changed

- **Breaking:** top-level bare references are no longer implicitly
  grabbed. `x: (( meta.name ))` now passes through the merge verbatim
  as a BOSH/CredHub placeholder, matching spruce; write
  `x: (( grab meta.name ))` to resolve it. Bare references in operand
  position (`(( env == "production" ))`) still evaluate. Bundled
  examples were updated to use explicit `grab`.

### Fixed

- BOSH/CredHub variable placeholders now survive a merge byte-for-byte:
  tight placeholders (`((cf_admin_password))`) are no longer re-spaced,
  and unparseable placeholder text such as
  `((genesis-entombed/uaa_ssl--key--fe75a2d0))` and
  `((/dns_healthcheck_tls.ca))` passes through untouched instead of
  failing the merge. Expressions starting with a registered operator
  still report parse errors.
- The vault backend now detects KV v2 mounts via
  `sys/internal/ui/mounts` (with per-mount caching), inserting the
  `data/` path segment v2 reads require and unwrapping the v2 response
  envelope, matching spruce's vaultkv behavior. Reads against
  KV v2-mounted secrets engines previously failed with
  "Invalid path for a versioned K/V secrets engine".
- Strings that cannot be written as plain YAML scalars for syntax
  reasons (e.g. `*.uaa.((system_domain))`) are now emitted
  single-quoted like spruce, instead of double-quoted. Type-lookalike
  strings (`"1.0"`, `"yes"`, `"null"`) keep double quotes, also
  matching spruce.
- Reference paths accept `+` inside a segment when followed by an
  identifier character (`meta.__vaultified.haproxy_ssl+certificate`),
  as produced by Genesis's vaultified manifests; `+` before a digit
  remains arithmetic.

## [1.32.1] - 2026-08-14

### Fixed

- `(( file ... ))` now fails with spruce's exact error text when the
  file cannot be read (`tried to read file <path>: could not be read -
  <os error>`) and when the argument resolves to a map or list
  (`tried to read file <arg>, which is not a string scalar`), instead
  of surfacing the raw Go error or trying to open the stringified
  collection as a filename.

## [1.32.0] - 2026-08-13

Closes the last five tracked entries in the
[known-gaps register](docs/spruce/known-gaps.md) and the ordering
component of the sixth: map keys now encode in spruce's order. Three
open entries remain in the register — the narrowed
mixed-key-type-map-encoding-order entry (key typing, quoting, and
label differences only) and two newly recorded divergences
(y-n-boolean-values-not-coerced, stringify-block-scalar-style).

### Added

- `raw_env` operator

  `(( raw_env $NAME ))` resolves an environment variable to its raw
  string value, bypassing the YAML type coercion normal `$NAME`
  substitution applies: `PORT=8080` stays the string `"8080"`. A
  set-but-empty variable is a valid empty string; an unset one errors.
  Semantics and error strings match spruce byte for byte. This was the
  last spruce operator missing from graft.

- `:nocache` expression modifier

  `(( vault:nocache "secret/db:password" ))` makes that single call
  bypass the per-run backend cache in both directions — it never reads
  a cached value and never writes one — while plain calls keep sharing
  the cache under unchanged keys. Honored by `vault`/`vault-try`,
  `awsparam`, `awssecret`, `nats`, and registry-registered custom
  backends; inert on operators without a backend cache. Composes with
  targets as `(( vault:nocache@prod ... ))`. An unknown modifier is a
  parse error. See
  [Expression Modifiers](docs/reference/expression-modifiers.md).

- `graft.QuickMerge` and `graft.QuickMergeFiles` library functions

  One-call conveniences that merge YAML strings or files left to right
  with full operator evaluation and return the marshaled YAML output.

- Wildcard history path filters

  `HistoryFilter.Path` now matches with graft's wildcard grammar
  (`*`, `**`, `[N]`, `[*]`, `[key=value]`) and segment-aware prefix
  matching, instead of literal string comparison.

### Changed

- Map keys are ordered like spruce on every YAML emit

  Behavior change: graft used to sort map keys purely
  lexicographically on encode (`item10` before `item9`, `10` before
  `2`). It now uses a port of spruce's comparator
  (`pkg/graft/keysort.go`): numeric-looking keys sort first,
  numerically, followed by string keys in spruce's natural order —
  digit runs compare numerically, non-letters sort before letters,
  uppercase before lowercase. String-only key sets are byte-identical
  to spruce; bare-numeric key sets match position-for-position (graft
  keys stay quoted strings, spruce's stay bare and typed). Pinned by
  the byte-exact runner `tests/spruce-compat/run-key-order.sh`.
  Residual divergences are documented in
  [known gaps](docs/spruce/known-gaps.md#mixed-key-type-map-encoding-order).

- `Document.ToYAML` and `DefaultEngine.ToYAML` route through
  `MarshalYAML`

  Library change: both now produce the same bytes as the CLI for the
  same tree, gaining the spruce-compatible key ordering and the
  special-float quoting guard (a string value like `".nan"` used to
  leave the library surface unquoted and silently re-parse as a
  float). History YAML (`History().ToYAML`) intentionally keeps
  goccy's lexicographic ordering: it is a graft-only diagnostic
  surface with no spruce counterpart, and its DTO structs hold map
  fields a tree-walk cannot reach.

- `(( stringify ))` serializes through the shared marshal

  Stringified subtrees carry the same spruce-compatible key order as
  every other YAML emit. The outer scalar's presentation still
  differs from spruce (quoted flow scalar vs literal block; see
  [known gaps](docs/spruce/known-gaps.md#stringify-block-scalar-style)).

### Fixed

- Dangling or mistyped `(( sort ... ))` markers now fail the merge

  A queued sort whose path no longer resolves after the merge (for
  example, because a prune removed it) or resolves to a non-list value
  now fails with exit code 2 and spruce's exact error text, instead of
  silently passing the document through unsorted. Sorting also now runs
  after all pruning (including `--prune` flags) and before
  cherry-picking, matching spruce's post-processing order, and
  `--skip-eval` follows the identical path. Behavior change: documents
  that previously merged successfully with a dangling or mistyped sort
  marker now fail, exactly as they do under spruce.

## [1.31.1] - 2026-08-13

Release-engineering release: no code changes to the CLI or library beyond
the version string.

### Added

- Homebrew tap distribution

  `brew install --cask fivetwenty-io/tap/graft` installs the binary and
  shell completions. The cask in
  [fivetwenty-io/homebrew-tap](https://github.com/fivetwenty-io/homebrew-tap)
  is generated and pushed automatically on each release.

- Signed and notarized macOS binaries

  The darwin release binaries carry a FiveTwenty Inc. Developer ID
  signature and Apple notarization, so Gatekeeper accepts them on first
  launch (previously the quarantined ad-hoc-signed binaries were killed
  outright on Apple Silicon).

- Debian and RPM packages, FreeBSD builds (amd64/arm64), and bash, zsh,
  and fish completions in every archive.

### Changed

- Releases are built and published by GoReleaser. Archive names changed
  from `graft-<version>-<os>-<arch>` to `graft_<version>_<os>_<arch>`,
  and the checksum file from `graft-<version>-checksums.sha256` to
  `graft_<version>_SHA256SUMS`.

## [1.31.0] - 2026-08-12

The library API release: `pkg/graft` is now a first-class Go library. The
CLI surface and the genesis/spruce stderr contract are unchanged and
byte-identical to 1.30.0, except for the `-v` dispatch fix noted under
Fixed.

### Added

- Parsing and merging entry points

  `Engine.ParseFile`, `ParseReader`, `ParseMultiDocFile`, and
  `ParseGoPatch` (with `DetectArrayRoot`, `RootIsArrayError`,
  `NewRootIsArrayError`, `IsArrayError`) replace the former stubs.
  `MergeFiles` and `MergeReaders` return a builder that carries load
  errors to `Execute()` instead of a nil builder that panicked.
  `MergeBuilder.Base`, `Overlay`, and `OverlayFile` compose document
  sources onto a chain.

- Document conveniences and sentinel errors

  Checked getters `String`, `Int`, `Int64`, `Float64`, `Bool`, plus
  `Has`, `Paths`, `SortKeys`, and `ToJSONIndent` on `Document`.
  `ErrNotFound`, `ErrTypeMismatch`, and `ErrInvalidPath` sentinels work
  with `errors.Is` against getter, `Set`, and `Delete` failures
  (`NewValidationErrorWithCause` carries the chain; `Error()` strings
  are unchanged). `MultiError` gained `Unwrap() []error`, so `errors.Is`
  and `errors.As` see through aggregated evaluation errors.

- Diff API

  `DiffResult`, `Change`, `ChangeType`, `DiffOptions` (including
  `IgnoreArrayOrder` and `IgnoreWhitespace`), `DiffDocuments`, renderers
  `WriteSideBySide`, `WriteUnified`, `WriteChangeList`, `WriteMergeTree`,
  and `Engine.Diff`/`DiffWithOptions`.

- Engine options and runtime reconfiguration

  `Option` alias, `WithCacheSize`, `WithCacheTTL`, `WithCacheDisabled`,
  `WithOperators`, `WithTraceOutput`, `WithTraceLevel` (with
  `TraceLevel`), and `DefaultEngine.Configure` for applying an option
  delta to a live engine with validate-before-mutate semantics.
  `WithLogger`, `WithDebugLogging`, and `WithYAMLCompat` are now
  functional. `Engine.ToYAML`, `ToJSON`, and `ToJSONIndent` evaluate and
  serialize instead of returning a not-implemented error; they resolve
  the document in place (pass `doc.Clone()` to keep the original).

- Post-processors

  The open `PostProcessor` interface, `WithPostProcessors` (engine-wide
  or per builder), built-ins via `NewPruner`, `NewCherryPicker`,
  `NewKeySorter`, and `NewSecurityRedactor`; processors run at the tail
  of `Execute()` after evaluation, pruning, and cherry-picking.

- Merge history

  `History`, `HistoryEntry`, `HistoryConfig`, `MergeBuilder.TrackHistory`,
  `Document.History`, `WithHistoryTracking`, `WithHistoryConfig`, and
  `HistoryFilter.Limit`. History is engine-scoped, off by default, and
  near-free when off. List-element mutations and the interior of newly
  added nested subtrees are not recorded; the docs state every gap.

- Custom backends (behind a feature flag)

  `Backend` and `TargetedBackend` with per-engine registration
  (`RegisterBackend`, `GetBackend`, `ListBackends`, `UnregisterBackend`,
  `WithBackend`), retry/cache/audit wrapping (`RetryConfig`, `TLSConfig`,
  `BackendCache`, `AuditLogger`), `BackendError`, `ErrBackendNotFound`,
  and `SequentialGetBatch`. Gated by `GRAFT_FEATURE_BACKEND_REGISTRY`
  (default off; behavior with the flag off is byte-identical). The
  vault, vault-try, awsparam, awssecret, and nats operators consult the
  registry when enabled, falling back to the built-in backends.
  `WithVault`/`WithVaultTarget` and `WithAWS`/`WithAWSTarget` register
  real SDK-backed implementations from a config struct.

- Testing support

  `NewMockEngine` (seeded in-memory vault/awsparam/awssecret/nats
  lookups with call recording), `OperatorFunc`, and `NewTestEvaluator`.

- Dependency graph and expression traversal

  `DependencyGraph`, `OperatorRef`, `EvalWave`, `BuildEvalPlan`,
  `DefaultEngine.BuildDependencyGraph` and `EvaluateParallel` (with
  `ErrNoWorkerPool`, `ErrInvalidEvalPlan`, `ErrDependencyCycle`) as a
  read-only projection of the live evaluation orderings; `Walk`,
  `Visitor`, and `Accept` over `Expr` with a `VisitOther` catch-all for
  forward compatibility.

- `EngineOf` nil-safe accessor for evaluator-attached engines, and
  `WithBackendRegistry` to toggle the backend feature flag without
  importing internal packages.

### Changed

- `Document.Prune` is variadic: `Prune(keys ...string)` (was
  `Prune(key string)`). Single-argument call sites compile unchanged.
- `NewEngine()` and `CreateDefaultEngine()` share one default
  configuration: 10000-entry cache, 4 max concurrent workers,
  alphabetical dataflow order (previously 1000/10 on one path).
- Engine-local operator registration is real: `RegisterOperator` on an
  engine affects that engine's evaluation everywhere, including
  control-flow expansion and nested dependency analysis. The exported
  `ControlFlowExpander` hook now receives the engine.
- Merge history records changes under full dotted paths (`meta.key`),
  not bare immediate keys.
- A zero-document merge runs post-processors and history attachment;
  with `WithCherryPick` it now returns the same error a non-empty merge
  does instead of an empty document.
- `Document`, `Engine`, `MergeBuilder`, and `DiffResult` are documented
  as closed interfaces: methods may be added in minor releases;
  implement `PostProcessor`, `Backend`, and `Visitor` instead.

### Deprecated

- `WithMaxWorkers` — use `WithConcurrency` (functionally identical).
- `WithVaultClient`, `WithVaultConfig`, `WithVaultSkipTLS`, and the
  `VaultClient` interface — never had an effect; use `WithVault` or
  environment variables.
- `WithAWSConfig`, `WithAWSProfile`, `WithAWSRegion` — never had an
  effect; use `WithAWS` or environment variables.
- `WithMemoryPools` — sets a feature flag nothing reads.

### Removed

- The never-wired copy-on-write tree types and their helpers:
  `COWNode`, `COWTree`, `COWEvaluator`, `EnhancedMigrationHelper`,
  `COWTreeFactory`, `COWPerformanceMonitor`, `COWTreeComparator`, their
  constructors, and the `ThreadSafeTree`/`TreeTransaction` interfaces
  and `WorkerPool` type they were the only implementors of. Nothing in
  graft outside their own tests ever constructed them; they were not
  the mechanism behind parallel evaluation. These symbols predate
  `pkg/graft` being a documented library surface (this release is the
  first to declare one), which is why their removal ships in a minor
  version.

### Fixed

- Pre-verb version flag precedence

  A version flag placed before the verb now wins over the subcommand,
  matching spruce: `graft -v merge ...` prints the version and exits 0
  before dispatch (previously the flag was silently ignored and the
  subcommand ran). Placed after the verb, the flag is still ignored
  and the verb runs; spruce instead treats a post-verb `-v` as a
  filename and exits 2. A pre-verb `-v` also skips `--color` and
  `--config` validation, so it now exits 0 where a bad value
  previously exited 1. The version line has always echoed the invoked
  name (`os.Args[0]`), so a spruce-named symlink or copy reports
  itself as `spruce` to genesis's version gate; that behavior is now
  pinned by tests.

## [1.30.0] - 2026-08-11

### Added

- Spruce drop-in parity

  The CLI surface, flags, exit codes, and stderr contract match spruce closely
  enough for graft to replace a `spruce` binary on `$PATH`, including under
  Genesis. Parity is covered by the `spruce-compat` test harnesses: a
  golden-output suite, an operator matrix, and an end-to-end Genesis drop-in
  check. Remaining known divergences are tracked in
  [docs/spruce/known-gaps.md](docs/spruce/known-gaps.md).

- YAML 1.1 compatibility layer

  Normalizes the YAML 1.1 behaviors that spruce relied on, so documents
  written for spruce parse and render the same way under graft's YAML 1.2
  parser.

- Configuration via `GRAFT_*` environment variables and a config file

  A `--config` flag loads a YAML configuration file; `GRAFT_*` environment
  variables override its values. Covers engine, cache, parallelism, logging,
  and metrics settings.

### Changed

- Parallel operator evaluation is enabled by default

  Operators are scheduled in dependency waves; within a wave,
  order-sensitive operators (such as `static_ips`) run one at a time,
  the rest run their work (including Vault/AWS/NATS calls)
  concurrently, and results are applied to the document tree serially
  in a fixed order. Set `GRAFT_PARALLEL_ENABLED=false` to fall back to
  serial evaluation.

[1.38.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.38.0
[1.37.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.37.0
[1.36.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.36.0
[1.35.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.35.0
[1.34.1]: https://github.com/fivetwenty-io/graft/releases/tag/v1.34.1
[1.34.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.34.0
[1.33.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.33.0
[1.32.2]: https://github.com/fivetwenty-io/graft/releases/tag/v1.32.2
[1.32.1]: https://github.com/fivetwenty-io/graft/releases/tag/v1.32.1
[1.32.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.32.0
[1.31.1]: https://github.com/fivetwenty-io/graft/releases/tag/v1.31.1
[1.31.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.31.0
[1.30.0]: https://github.com/fivetwenty-io/graft/releases/tag/v1.30.0
