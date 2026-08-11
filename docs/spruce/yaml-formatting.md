# YAML Formatting: Graft vs. Spruce

Graft and spruce use different YAML libraries, and the output each one
produces differs in a handful of ways that matter for tools consuming
that output as text rather than as parsed data, most notably Genesis's
byte-oriented `))`-line rejoin hack (see below). This document lays out
the library difference and each known formatting divergence.

## Library difference

| | Spruce | Graft |
|---|---|---|
| Library | `github.com/geofffranks/yaml` | `github.com/goccy/go-yaml` |
| Basis | A 2016-era personal fork of `gopkg.in/yaml.v2`, carrying two cherry-picked upstream fixes, no longer maintained | Actively maintained, YAML 1.2 compliant |
| YAML version behavior | YAML 1.1-flavored (`yes`/`no`/`on`/`off` parse as booleans) | YAML 1.2 by default; graft adds a compatibility layer (`pkg/graft/yaml_compat.go`) that converts YAML 1.1 boolean strings to Go booleans on input, and quotes them back out on output |
| In-memory map type | `map[interface{}]interface{}` (via `geofffranks/simpleyaml`) | `map[string]interface{}` throughout |

Full detail on graft's YAML library and its migration history lives in
[YAML libraries in graft](../architecture/yaml-libraries.md).

## Known differences

| Aspect | Spruce | Graft | Status |
|---|---|---|---|
| Trailing newline | The CLI always appends one extra `\n` after the marshaled document (`fmt.Fprintf(os.Stdout, "%s\n", merged)`), on top of whatever the marshaller itself emits. | The CLI follows the identical pattern (`printStdOutf("%s\n", string(merged))`). | Matches. Verified byte-for-byte against built binaries across merge, json, `--skip-eval`, stdin, and diff invocations; see [known gaps](known-gaps.md#trailing-newline-byte-parity-unverified). |
| Null rendering | Renders absent/null values using the spruce-side library's YAML 1.1-family rules. | Renders absent/null values using goccy's YAML 1.2 rules. | Matches. Confirmed by running both `spruce merge` and `graft merge` binaries over equivalent fixtures: every null representation (explicit `null`, `~`, or an empty scalar) marshals to the bare word `null` in both tools, and a string value that happens to read `"null"` or `"~"` stays quoted rather than being rendered as an unquoted null-like token. Pinned by a test in `pkg/graft/yaml_spruce_parity_test.go`; see [known gaps](known-gaps.md#null-rendering-parity-unverified). |
| Map key ordering | Preserves insertion order in the merged tree. | Graft's tree is a native Go `map[string]interface{}`, which has no defined iteration order in memory, but the goccy encoder sorts map keys alphabetically on marshal. | Matches for string-only and integer-only key sets. Spruce's `yaml.v2`-family encoder also sorts map keys alphabetically on encode, so both tools produce alphabetically ordered output regardless of insertion order. Confirmed by the same spruce-vs-graft binary comparison and pinned by a test in `pkg/graft/yaml_spruce_parity_test.go`; see [known gaps](known-gaps.md#map-key-order-parity-unverified). Diverges for a map that mixes integer and string keys at the same level: graft coerces every key to a string on parse and sorts the result lexicographically, while spruce keeps numeric keys typed and orders them numerically before string keys. See [known gaps](known-gaps.md#mixed-key-type-map-encoding-order). |
| Special-float-lookalike strings | A string value like `.nan`, `.inf`, or `-.inf` stays quoted on output, so it round-trips as a string rather than a float. | Goccy reserves `.nan`, `.inf`, and `-.inf` (with case variants) as float keywords; graft's marshaller (`needsExplicitQuote` in `pkg/graft/yaml.go`) detects string values matching those keywords and forces a quote on output, so they round-trip as strings the same way spruce's do. A lookalike goccy does not reserve, such as `+.inf`, is left unquoted since it already round-trips as a string without help. | Matches. Pinned by `TestMarshalYAML_QuotesSpecialFloatLookalikeStrings` and `TestMarshalYAML_PlusInfLookalikeUnaffected` in `pkg/graft/yaml_spruce_parity_test.go`. |
| Indentation | 2-space, standard `yaml.v2`-family style. | 2-space, explicit `yaml.Indent(2)` encoder option (`pkg/graft/yaml.go`). | Matches. |
| Multi-line operator wrapping | Long `(( ... ))` operator expressions can be wrapped by the marshaller such that a lone `))` ends up alone on its own output line. Genesis has a dedicated post-processing step that rejoins these lines (see below). | Goccy's line-wrapping rules for long string values differ from the spruce-side library's; whether graft ever produces a lone `))` on its own line has not been characterized. | Genesis's rejoin step is a no-op if graft never produces this pattern, so this direction is safe even if unconfirmed either way. |

### The lone `))` line quirk

Genesis calls `spruce merge --skip-eval` and then post-processes the
output to rejoin any line that consists solely of `))`, because the
spruce-side YAML library can wrap a long operator expression across
lines when marshaling it back out as a scalar, leaving a trailing `))`
by itself. This is purely a spruce output quirk being compensated for
downstream; the rejoin logic does nothing if the input it receives
never contains an isolated `))` line. Since graft uses a different
encoder with different line-wrapping behavior, this quirk is not
expected to reproduce identically, and does not need to: the
compensating logic degrades gracefully to a no-op either way.

## Boolean coercion

Because goccy is YAML 1.2 compliant, it treats `yes`, `no`, `on`, and
`off` as plain strings rather than booleans on parsing. Graft's
compatibility layer (`pkg/graft/yaml_compat.go`) converts an *unquoted*
occurrence of one of these words to a Go boolean on input, matching
spruce's YAML 1.1-flavored parsing.

An explicitly quoted occurrence (`"yes"`, `'On'`, `"OFF"`, ...) is left
as a string instead: quoting is the author's request to keep the value
text, and spruce honors that request the same way. Graft parses the
document through goccy's AST first so it can tell, from the scalar's own
token type, whether it was quoted in the source -- a Go string built
from an already-decoded value has no such information left, so this
distinction has to be made before the compat layer ever sees a plain
`interface{}`. On output, goccy's own encoder quotes any string value
that reads as one of these words, whether it came from a quoted source
scalar or just happens to equal one of the words, so the round trip
never silently turns a string into a boolean.

## Related documents

- [Merge semantics](merge-semantics.md)

- [Known gaps](known-gaps.md)
