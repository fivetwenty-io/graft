# Operator inventory: graft vs. spruce

spruce registers 25 operator names. graft registers 48. This page lists both
sets, operator by operator, and calls out the operators that exist in one
tool but not the other.

## Operators present in both, with equivalent names

These operator names exist in both spruce and graft. Where a specific
behavior or error message was checked directly against source for this
comparison, it is noted; everything else should be assumed unverified for
exact byte-for-byte parity until confirmed (see [Known gaps](known-gaps.md)).

| Operator | spruce | graft | Notes |
|---|---|---|---|
| `grab` | Yes | Yes | Both resolve one or more literal/reference arguments; with more than one argument, both flatten top-level lists into a single combined list. |
| `concat` | Yes | Yes | String-joins literal and reference scalar arguments; both reject map/list operands. |
| `calc` | Yes | Yes | Arithmetic-expression evaluator operating on numeric path references and literals. |
| `static_ips` | Yes | Yes | Builds an IP pool from `networks.<name>.subnets.*.static` and assigns addresses to instances by availability zone. |
| `vault` | Yes | Yes | Fetches a secret at `path/to/secret:key`. Checked directly: both return the exact error text `secret <key> not found` for a missing secret, and both use the byte-identical malformed-key error text `invalid argument <key>; must be in the form path/to/secret:key`. |
| `awsparam`, `awssecret` | Yes | Yes | AWS Systems Manager Parameter Store and Secrets Manager lookups, both keyed off `AWS_PROFILE`/`AWS_REGION`/`AWS_ROLE`. |
| `base64`, `base64-decode` | Yes | Yes | Standard base64 encode/decode of a single string-scalar argument. |
| `file` | Yes | Yes | Reads a file's contents as a string; relative paths resolve against a base-path environment variable (`SPRUCE_FILE_BASE_PATH` for spruce, `GRAFT_FILE_BASE_PATH` for graft — see [CLI surface](cli-surface.md)). |
| `load` | Yes | Yes | Loads and YAML-parses a local file or, if the argument looks like a URI with a scheme, fetches it over HTTP. |
| `inject` | Yes | Yes | Merges one or more referenced maps into the parent map at the inject site. |
| `defer` | Yes | Yes | Re-emits its own arguments as literal, unevaluated `(( ... ))` operator syntax for a later evaluation pass. |
| `empty` | Yes | Yes | Produces an empty map, list, or string depending on the requested type name. |
| `ips` | Yes | Yes | Computes one or more IP addresses from a CIDR or bare IP plus a start index and optional count. |
| `join` | Yes | Yes | Concatenates literal or list-of-scalar arguments with a separator. |
| `keys` | Yes | Yes | Collects the keys of one or more referenced maps into a list. |
| `negate` | Yes | Yes | Boolean negation of a single argument. |
| `null` | Yes | Yes | Internal fallback operator for an unrecognized operator name; not meant to be invoked directly as `(( null ))`. |
| `param` | Yes | Yes | Fails evaluation with the given literal message unless a later merge document overwrites the value first. |
| `prune` | Yes | Yes | Marks a path for deletion after evaluation completes. |
| `sort` | Yes | Yes | Valid only as the first element of a list; sorts the list's remaining entries. |
| `shuffle` | Yes | Yes | Randomly reorders the combined elements of its arguments. |
| `stringify` | Yes | Yes | YAML-marshals a referenced value to a string, or passes a literal through unchanged. |
| `cartesian-product` | Yes | Yes | Combines multiple list/scalar arguments into the flat cartesian expansion of concatenated strings. graft additionally registers the shorter alias `cartesian` for the same implementation; spruce has no such alias. |
| `raw_env` | Yes | Yes | Resolves a single `$ENVVAR`-style argument to its raw string value, bypassing YAML type coercion: `(( raw_env $PORT ))` keeps `"8080"` a string where `(( grab $PORT ))` yields the integer 8080. A set-but-empty variable is a valid (empty string) value. Fallback branches after `\|\|` get normal coercing evaluation — that asymmetry matches spruce. Checked directly: error strings (`environment variable $X is not set`, arity, non-env argument) are byte-identical to spruce's. |

## Operators present only in graft

graft ships operator categories with no spruce equivalent:

| Operator | File | Notes |
|---|---|---|
| `vault-try` | `op_vault.go` | Tries a sequence of vault paths and returns the first successful lookup; spruce has no fallback-chain vault operator. |
| `nats` | `op_nats.go` | Reads from a NATS key-value backend; spruce has no NATS integration of any kind. |
| `split` | `op_split.go` | Splits a string on a literal separator or a PCRE-style regular expression (via the `regexp2` library), producing a list. |
| `?:` (ternary) | `op_ternary.go` | Three-argument conditional: `(( condition ? true_value : false_value ))`. Requires exactly three arguments. |
| `+`, `-`, `*`, `/`, `%` | `op_add.go`, `op_subtract.go`, `op_multiply.go`, `op_divide.go`, `op_modulo.go` | Type-aware arithmetic operators usable directly in expressions (`(( a + b ))`), not only inside a `calc` string. |
| `&&`, `\|\|`, `!` | `op_boolean.go` | Type-aware boolean AND, OR-else, and NOT operators. spruce supports `\|\|` only as parse-time expression sugar for a literal fallback value (e.g., `(( grab a.b \|\| "default" ))`) and has no registered boolean operator at all — spruce's `&&`, `!`, and general boolean logic do not exist as operators. graft's `\|\|` is a full registered operator (`OrElseOperator`) rather than parser-level sugar, but the `(( a \|\| b ))` fallback syntax still works the same way a spruce user would expect. |
| `==`, `!=`, `<`, `>`, `<=`, `>=` | `op_comparison.go` | Type-aware comparison operators; spruce has no comparison operators at all. |
| `type` | `op_type.go` | Reports the type of a single argument as one of `string`, `int`, `float`, `bool`, `map`, `array`, or `null`. Requires exactly one argument. spruce reports `(( type )) operator not defined`. |
| `flatten` | `op_flatten.go` | Flattens a list recursively at every depth into a single flat list; takes no depth argument. Requires exactly one list argument. spruce reports `(( flatten )) operator not defined`. |
| `uniq` | `op_uniq.go` | Removes duplicate entries from a list, keeping the first occurrence and preserving input order; it never sorts. Requires exactly one list argument. spruce reports `(( uniq )) operator not defined`. |

graft does not register a plain `or` operator name. The `||` operator above
already covers spruce-equivalent short-circuit fallback semantics, making a
separate `or` registration redundant.

## Operators present only in spruce

None. Every spruce operator name has a same-named, same-purpose registration
in graft as shown in the table above. (`raw_env`, the last holdout, was added
in graft 1.32.0.)

## Counting notes

spruce's 25 registrations are 25 distinct implementations, with no aliases.
graft's 48 registrations include two aliases pointing at one implementation
(`cartesian-product` / `cartesian`) and one operator, `vault`, whose file
(`op_vault.go`) also registers the separate `vault-try` implementation. Every
other graft registration is a distinct implementation.

## Related pages

- [Parity overview](README.md)
- [CLI surface](cli-surface.md)
- [Merge semantics](merge-semantics.md)
- [Known gaps](known-gaps.md)
- [Full operator reference](../reference/operators.md)
