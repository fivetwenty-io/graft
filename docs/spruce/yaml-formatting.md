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
| Map key ordering | Sorts keys on encode in two tiers: typed numeric keys (a bare `10:`) sort numerically among themselves and all precede string keys; string keys use a natural comparison in which digit runs compare numerically (`z2b` before `z10a`, `item2` before `item10`), non-letters sort before letters by raw rune (`_x`, `\|p`, `~t`, then `Zx`, `ax`), and uppercase precedes lowercase. Not alphabetical: `item10` sorts *after* `item9`, and `int_val` before `int64_val` (a non-digit rune counts as an empty, zero-valued digit run). | Graft's tree is a native Go `map[string]interface{}` with no defined iteration order in memory; on marshal, graft sorts keys with a port of spruce's comparator (`pkg/graft/keysort.go`) — numeric-looking keys first, numerically, then strings in the same natural order. | Matches spruce's key order for string-only key sets (byte-identical output) and for bare-numeric key sets (position-for-position; spruce renders those keys bare and typed — `10:`, `1000:` for `1e3:` — while graft's coerced keys stay quoted strings with the source spelling, `"10":`). Pinned by `TestMarshalYAML_SpruceKeyOrder` in `pkg/graft/yaml_spruce_parity_test.go` and the byte-exact runner `tests/spruce-compat/run-key-order.sh`. Residual divergences — key typing/quoting labels, maps mixing *quoted*-numeric keys with other keys, and exotic typed keys (hex/octal/bool/null) — are documented in [known gaps](known-gaps.md#mixed-key-type-map-encoding-order). |
| Special-float-lookalike strings | A string value like `.nan`, `.inf`, `+.inf`, or `-.inf` is double-quoted on output, because spruce's YAML library would read the bare word back as a float. | Graft decides with the same resolver rules spruce's library uses (`yamlV2ResolvesToString` in `pkg/graft/yaml_scalar.go`), so these strings come out double-quoted exactly as spruce writes them and round-trip as strings. | Matches. Pinned by `TestMarshalYAML_QuotesSpecialFloatLookalikeStrings` and `TestMarshalYAML_PlusInfLookalikeQuotedLikeSpruce` in `pkg/graft/yaml_spruce_parity_test.go`. |
| Indentation | 2-space, standard `yaml.v2`-family style. | 2-space, explicit `yaml.Indent(2)` encoder option (`pkg/graft/yaml.go`). | Matches. |
| Scalar style of strings | spruce's libyaml-derived emitter picks each string's style. It writes a string plain when it can, single-quotes one that can't be plain for syntax reasons (`'*.uaa.((system_domain))'`, `'?'`, `'---'`), double-quotes a type lookalike (`"1.0"`, `"yes"`, `"null"`) or anything that needs an escape such as a tab (`"\t"`), and writes a multi-line string as a literal block. Plain and quoted scalars fold at a space past column 80. | Graft ports that emitter's scalar analysis, style selection, and writers (`pkg/graft/yaml_scalar.go`). goccy still lays out the mappings and sequences, and graft writes each string that goccy would render differently in spruce's style at the column where it lands (`scalarEncoder` in `pkg/graft/yaml.go`). Keys and values both go through it. | Matches byte for byte across 16,275 generated short strings as values and as keys, and across multi-line, long, and unicode cases. Single quotes matter to Genesis, whose Credhub entombment step replaces `((...))` with `""` in the rendered manifest and re-parses it. That stays valid YAML inside single quotes (`'*.uaa.""'`) but not inside double quotes (`"*.uaa."""`). Pinned by `TestMarshalYAML_SingleQuoteStyle` in `pkg/graft/yaml_spruce_parity_test.go` and the tables in `pkg/graft/yaml_scalar_style_test.go`. |
| Multi-line operator wrapping | Long `(( ... ))` operator expressions can be wrapped by the marshaller such that a lone `))` ends up alone on its own output line. Genesis has a dedicated post-processing step that rejoins these lines (see below). | Graft folds long strings at the same spaces spruce does, so it wraps these expressions the same way. | Matches. |

### The lone `))` line quirk

Genesis calls `spruce merge --skip-eval` and then post-processes the
output to rejoin any line that consists solely of `))`, because the
spruce-side YAML library can wrap a long operator expression across
lines when marshaling it back out as a scalar, leaving a trailing `))`
by itself. This is purely a spruce output quirk being compensated for
downstream; the rejoin logic does nothing if the input it receives
never contains an isolated `))` line. Graft folds long strings at the same spaces spruce does, so it produces the same lines, and Genesis's rejoin step treats graft's output exactly as it treats spruce's.

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
`interface{}`. On output, graft double-quotes any string value that reads as one of these words, the way spruce does, whether it came from a quoted source scalar or just happens to equal one of the words. So the round trip never silently turns a string into a boolean.

## Related documents

- [Merge semantics](merge-semantics.md)

- [Known gaps](known-gaps.md)
