# Operator Reference

Full reference for the 47 operators graft registers in `pkg/graft/operators`.
Every operator implements `Setup`, `Run`, `Dependencies`, and `Phase`
(`pkg/graft/interfaces.go`); `Setup` is a no-op for every operator in this
package — argument validation happens inside `Run`. The **Phase** column
below states when each operator runs relative to the rest of the document
(see [Merge and evaluation phases](../architecture/engine-overview.md#merge-and-evaluation-phases)):

- **MergePhase**: runs while documents are being combined, before any `EvalPhase` operator runs.

- **ParamPhase**: runs before `EvalPhase`; an unresolved `param` aborts the run before `EvalPhase` starts.

- **EvalPhase**: runs during the main evaluation pass, where the large majority of operators, including all external-backend lookups, execute.

## Operator index

| Operator(s) | Phase | Category |
|---|---|---|
| `grab` | EvalPhase | Reference |
| `concat`, `join`, `split`, `stringify`, `base64`, `base64-decode` | EvalPhase | String |
| `keys`, `negate`, `null`, `empty`, `type` | EvalPhase | Data |
| `param` | ParamPhase | Validation |
| `inject` | MergePhase | Merge structure |
| `prune` | EvalPhase | Merge structure |
| `defer` | EvalPhase | Template generation |
| `+`, `-`, `*`, `/`, `%` | EvalPhase | Arithmetic |
| `calc` | EvalPhase | Arithmetic |
| `==`, `!=`, `<`, `>`, `<=`, `>=` | EvalPhase | Comparison |
| `&&`, `\|\|`, `!` | EvalPhase | Boolean |
| `?:` | EvalPhase | Conditional |
| `sort` | MergePhase | Array |
| `shuffle` | EvalPhase | Array |
| `cartesian-product`, `cartesian` | EvalPhase | Array |
| `flatten`, `uniq` | EvalPhase | Array |
| `vault`, `vault-try` | EvalPhase | External source |
| `awsparam`, `awssecret` | EvalPhase | External source |
| `nats` | EvalPhase | External source |
| `file` | EvalPhase | External source |
| `load` | EvalPhase | External source |
| `ips` | EvalPhase | IP arithmetic |
| `static_ips` | EvalPhase | IP arithmetic |

The array-merge markers (`append`, `prepend`, `replace`, `inline`, `merge`,
`merge on <key>`, `(( delete ... ))`) are handled by `pkg/graft/merger`
during the merge step rather than through the operator-registration
mechanism above; they are not counted in the 47 registered operators.

The control-flow keywords (`if`/`elif`/`else`/`fi`, `for`/`done`,
`while`/`done`, `case`/`when`/`default`/`esac`, and `range`) are not
operators either. They are expanded by a source-to-source preprocessor that
runs on each input file before YAML parsing; see
[Control flow](../user-guide/operators/control-flow.md).

There is no `or` operator name registered in graft. `||` maps to a
registered implementation (`OrElseOperator`); see
[Boolean operators](#boolean-operators) below.

## Reference

### grab

`(( grab path.to.value ))`

Resolves one or more references and returns the resolved value. With a
single argument, the resolved value is returned as-is (including `nil`,
which is a valid result). With more than one argument, `grab` flattens any
top-level list results into a single combined list — this is how
`(( grab list_a list_b ))` produces one list from two array references. Called with zero arguments, it errors with "no arguments
specified to `(( grab ... ))`".

```yaml
value: (( grab path.to.value ))
combined: (( grab list_a list_b ))
```

### String operators

| Operator | Syntax | Notes |
|---|---|---|
| `concat` | `(( concat "a" b "c" ))` | Requires at least two arguments — `"concat operator requires at least two arguments"` otherwise. Non-string arguments are stringified; list arguments are joined element-wise with no separator before being appended. |
| `join` | `(( join "," array ))` | First argument is the separator and must be a literal string. Requires at least two total arguments ("no arguments specified"/"too few arguments supplied to `(( join ... ))`" otherwise). |
| `split` | `(( split "," csv_string ))` | Requires exactly two arguments (separator, then the string to split) — errors on 0, 1, or more than 2 arguments. |
| `stringify` | `(( stringify some_map ))` | Requires exactly one argument; renders it as a YAML string. |
| `base64` | `(( base64 "secret data" ))` | Requires exactly one argument; errors if it resolves to `nil`. |
| `base64-decode` | `(( base64-decode encoded_string ))` | Requires exactly one argument. |

### Data operators

| Operator | Syntax | Notes |
|---|---|---|
| `keys` | `(( keys some_map ))` | Extracts and sorts the keys of one or more maps into a single array. Errors with "no arguments specified to `(( keys ... ))`" if called with zero arguments. |
| `negate` | `(( negate enabled ))` | Requires exactly one argument. Boolean-negates using the same truthiness rules as `!`: `nil`, `false`, `0`/`0.0`, `""`, and empty lists/maps are truthy-false (so `negate` returns `true`); anything else returns `false`. |
| `null` | `(( null ))` / `(( null some_value ))` | With no arguments, returns `nil`. With one argument, returns `true` if that value is `nil`, `false` otherwise. Errors ("null operator takes at most one argument") if given more than one argument. |
| `empty` | `(( empty some_value ))` | Dual-purpose: `(( empty map ))`/`(( empty list ))` on an unresolvable type-name reference construct an empty `{}`/`[]`; on a resolvable value it reports whether that value is empty. Requires exactly one argument. |
| `type` | `(( type some_value ))` | Returns the argument's type name as a string, exactly one of `string`, `int`, `float`, `bool`, `array`, `map`, or `null`. Requires exactly one argument — `"type operator requires exactly one argument, got <n>"` otherwise. |

Because `type` is a registered operator, a bare `(( type ))` with no
argument is an error rather than literal text that survives the run. Genesis
templates that emitted `(( type ... ))` expecting graft to pass it through to
a later pass must wrap it in `(( defer ))`.

### param

`(( param "message" ))`

Marks a key as a required parameter. Runs in **ParamPhase**, before
`EvalPhase`. Requires exactly one argument — "param operator only expects
one argument" otherwise. `param` always returns an error containing the
resolved argument text (or its literal string form if it can't be
evaluated in ParamPhase); a document with any unresolved `(( param ))` left
in place fails the run before evaluation of any `EvalPhase` operator, so
`param` failures and `EvalPhase` failures never appear together in the
same run.

```yaml
password: (( param "Database password is required" ))
```

### inject

`(( inject some_map ))`

Runs in **MergePhase**. Deep-merges one or more resolved maps into the
parent structure at the current location (`Response.Type = Inject`, not
`Replace`). Each argument must resolve to a `map[string]interface{}`;
a non-map argument errors with "inject operator argument must resolve to a
map" (or "`<ref>` is not a map" for a direct reference that resolves to a
non-map). A `nil` argument errors with "inject operator argument cannot be
nil". Called with no resolvable maps, it errors with "no arguments
specified to `(( inject ... ))`".

```yaml
settings:
  (( inject common_settings ))
  custom: value
```

### prune

`(( prune ))`

Marks the current path for removal from the final output. It does not
delete the value immediately: it records the path via the engine's
`AddKeyToPrune` state and returns the current value unchanged, so other
operators can still reference the value before it is dropped in the
post-processing pass. The `graft merge --prune <key>` CLI flag adds
additional paths to the same removal list.

```yaml
internal:
  temp: (( prune ))
```

### defer

`(( defer grab runtime.value ))`

Preserves an expression as literal text instead of evaluating it,
intended for generating templates whose operators should be evaluated by a
later graft pass rather than the current one. Requires at least one
argument — "defer has no arguments - what are you deferring?" otherwise.

### Arithmetic operators

| Operator(s) | Syntax | Notes |
|---|---|---|
| `+`, `-`, `*`, `/`, `%` | `(( a + b ))` | Type-aware binary arithmetic, implemented on a shared `ArithmeticOperatorBase` that dispatches by operand type via the type-handler registry. |
| `calc` | `(( calc "base * rate + offset" ))` | Requires exactly one argument: the expression. Supports `min`, `max`, `mod`, `pow` (all binary), and `sqrt`, `floor`, `ceil` (all unary) as built-in functions inside the expression. Zero arguments errors with "calc operator only expects one argument containing the expression". |

```yaml
sum: (( 1 + 2 ))
result: (( calc "max(0, min(100, value))" ))
rounded: (( calc "floor(price * 100) / 100" ))
```

`calc` accepts the expression in three forms:

- **Quoted** — `(( calc "a + b" ))`. The full form; the only one that accepts
  function calls.

- **Raw** — `(( calc a + b ))`, `(( calc (a + b) * 2 ))`. Infix arithmetic and
  parenthesised grouping, written without quotes. Function calls are **not**
  available in this form: `(( calc max(a, b) ))` fails to parse, because the
  parentheses are read as expression grouping.

- **Leading operator** — `(( calc * 2 ))`, and likewise `+`, `-`, `/`, `%`.
  Modifies the value the same path held in an earlier file of the same merge.

Bare names inside the expression are resolved against the document: first as a
sibling of the key being computed, then from the document root. A name that
resolves nowhere errors with "calc operator does not support named variables
in expression: `<name>`"; one that resolves to `nil` or to a non-numeric type
errors with "path `<name>` references a nil value, which cannot be used in
calculations" and "path `<name>` is of type `<type>`, which cannot be used in
calculations" respectively. `**` is exponentiation; `^` is bitwise XOR, not a
power operator. Both require the quoted form; unquoted, neither tokenizes.
There is no `pi` constant — use `pow(a, b)` and a document key.

The leading-operator form reads the prior value out of the merge, so it spans
exactly one merge step: `graft merge base.yml overlay.yml` with `timeout: 30`
in the base and `timeout: (( calc * 2 ))` in the overlay yields `60`. Chaining
a second overlay that also modifies `timeout` yields `0`, because the prior
value recorded for the third file is the second file's unevaluated expression
text rather than a number. With no prior value at all, the form falls back to
`0`.

### Comparison operators

`==`, `!=`, `<`, `>`, `<=`, `>=` — each registered as a `TypeAware*Operator`
built on a shared `ComparisonOperator`/type-registry base. Each requires
exactly two arguments; calling one with any other count errors with
`"<op> operator requires exactly 2 arguments, got <n>"`.

```yaml
is_prod: (( env == "production" ))
in_range: (( count >= 5 && count <= 100 ))
```

### Boolean operators

| Operator | Registered implementation | Notes |
|---|---|---|
| `&&` | `TypeAwareAndOperator` (`boolean_base.go`) | Short-circuiting logical AND with type-aware truthiness. |
| `!` | `TypeAwareNotOperator` (`boolean_base.go`) | Unary logical NOT with type-aware truthiness. |
| `\|\|` | `OrElseOperator` (`op_boolean.go`) | **Not** a true boolean OR — evaluates the left argument and returns it if evaluation succeeds and the value is non-`nil`; otherwise evaluates and returns the right argument. This "coalesce" behavior is what `(( vault "path" \|\| "default" ))` relies on. Requires exactly two arguments. |

```yaml
allowed: (( is_admin || has_permission ))
ready: (( initialized && !maintenance ))
port: (( grab config.port || 8080 ))
```

### ?: (ternary)

`(( condition ? true_value : false_value ))`

Registered as `TypeAwareTernaryOperator`. Requires exactly three arguments
(condition, true branch, false branch) — `"?: operator requires exactly 3
arguments (condition, true_value, false_value), got <n>"` otherwise.
Evaluation short-circuits: only the branch selected by the condition's
type-aware truthiness is evaluated, so a failing expression in the unused
branch does not cause an error.

```yaml
size: '(( large ? "8Gi" : "2Gi" ))'
```

### Array operators

| Operator | Phase | Syntax | Notes |
|---|---|---|---|
| `sort` | MergePhase | `(( sort ))` / `(( sort by name ))` | `Run` returns the current value unchanged during evaluation; the sort is applied as a post-processing step (`AddToSortListIfNecessaryWithEngine`, `sortList`). Sorting requires a homogeneous list — mixed element types, or nested lists, produce a `tree.TypeMismatchError`. For a list of maps, the sort key defaults to `name` if `sort by <key>` is not given, and every map in the list must contain that key. |
| `shuffle` | EvalPhase | `(( shuffle array ))` | Randomly reorders elements using `crypto/rand`. Each argument must resolve to a list or scalar (list elements are flattened together) — a `nil` argument errors with "shuffle operator argument cannot be nil", and a map argument errors with "shuffle only accepts arrays and scalar values". |
| `cartesian-product` / `cartesian` | EvalPhase | `(( cartesian-product sizes colors ))` | Both names register the same `CartesianProductOperator`. Requires at least one argument — "no arguments specified to `(( cartesian-product ... ))`" otherwise. |
| `flatten` | EvalPhase | `(( flatten nested ))` | Flattens a list recursively — nested lists at every depth are spliced into a single flat list. Requires exactly one argument — `"flatten operator requires exactly one argument, got <n>"` otherwise — and that argument must be a list (`"flatten operator requires a list argument, got <type>"`). There is no depth argument. |
| `uniq` | EvalPhase | `(( uniq with_dupes ))` | Removes duplicate elements, keeping the first occurrence of each and preserving the input order. It never sorts. Comparison is by value and type, so `1` and `"1"` are distinct. Requires exactly one argument — `"uniq operator requires exactly one argument, got <n>"` otherwise — and that argument must be a list (`"uniq operator requires a list argument, got <type>"`). |

### External sources

#### vault / vault-try

```yaml
password: (( vault "secret/db:password" ))
value: (( vault "secret/v2:key; secret/v1:key" ))          # semicolon-separated fallback paths
value: (( vault "secret/key" || "default" ))                # || fallback to a literal default
```

`vault` requires at least one argument. Each vault path/key pair must be in
the form `path/to/secret:key`; an argument that doesn't parse into both
parts errors with `invalid argument <value>; must be in the form
path/to/secret:key`. A missing secret from the backend surfaces as `secret
<key> not found`. A path string containing semicolons is split into
multiple candidate paths, tried in order; this is the current, preferred
way to express "try several vault paths." `vault-try` (`(( vault-try
path1:key path2:key "default" ))`, minimum 2 arguments: one or more vault
paths followed by a trailing default) predates the semicolon syntax and is
deprecated in favor of it, though it remains registered and usable.

Target syntax selects a named backend configuration. It is written on the
operator name, `(( vault@production "path:key" ))`, and the target is carried
through to `Run`, which resolves it against the corresponding client pool.
Each target is configured through per-target environment variables —
`VAULT_<TARGET>_ADDR` and `VAULT_<TARGET>_TOKEN` for `vault`,
`AWS_<TARGET>_REGION` and friends for `awsparam`/`awssecret`, and
`NATS_<TARGET>_URL` for `nats`. An unconfigured target errors at evaluation
time naming the variables it expected.

The spruce spelling that puts the target on the path,
`(( vault production@"path:key" ))`, is rejected outright:

```
vault target must be written as (( vault@<target> "path:key" )), not (( vault <target>@"path:key" ))
```

`REDACT` (any non-empty value) puts vault, AWS, and NATS lookups into a
skipped state for the run. `vault`/`vault-try`, `nats`, and
`awsparam`/`awssecret` all return the literal string `"REDACTED"` in that
state, without making a backend call.

#### awsparam / awssecret

```yaml
host: (( awsparam "/app/prod/db_host" ))
port: (( awsparam "/app/config?key=database.port" ))
pass: (( awssecret "prod/db?key=password" ))
```

Both names register the same `AwsOperator`, distinguished by its `variant`
field (`"awsparam"` for AWS Systems Manager Parameter Store, `"awssecret"`
for AWS Secrets Manager). Requires at least one argument. The key is split
on the first `?`; everything after it is parsed as a standard URL query
string (`url.ParseQuery`), and an unparseable query string errors with
"invalid argument string: `<err>`". A JSON secret value can be narrowed
with `?key=<field>` as shown above.

#### nats

```yaml
config: (( nats "kv:bucket/key" ))
template: (( nats "obj:assets/template.yml" ))
```

Reads from a NATS JetStream KV or Object store, selected by the
`kv:`/`obj:` prefix on the path argument. Requires at least one argument.

#### file

```yaml
cert: (( file "certs/server.pem" ))
combined: (( file base_path "server.pem" ))
```

Reads a file's contents as a string. Accepts one argument (a path) or two
arguments (a base path and a filename, joined with `filepath.Join`); any
other argument count errors with "file operator requires one or two string
arguments". A `nil`-resolving argument errors accordingly.

#### load

```yaml
external: (( load "extra-config.yml" ))
```

Loads and parses a YAML/JSON file (local path or, per the `net/http`
import in `op_load.go`, potentially a URL) and returns its parsed content.
Requires exactly one argument; a map or list argument is rejected as "only
string scalars are supported" for the location.

### IP operators

#### ips

```yaml
gateway: (( ips "10.0.0.0/24" 1 ))
range: (( ips "10.0.0.0/24" 10 5 ))
```

Computes IP addresses from a CIDR block plus one or two numeric offsets.
Requires at least two arguments.

#### static_ips

```yaml
ips: (( static_ips 0 1 2 ))
```

BOSH-style static IP allocation: each numeric argument is an offset into
the job's network's static IP pool(s), optionally prefixed with an
availability zone (`<az>:<number>`). Must be evaluated inside a job
definition block, or it errors with "not currently inside of a job
definition block." Verified error conditions include: a non-numeric `instances:`
value for the current job; a negative `instances:` value; a malformed
network AZ pool entry, invalid IP, or an IP range where the end address
precedes the start; an `azs:` list containing non-string entries; fewer
offset arguments supplied than the job has instances ("not enough static
IPs requested for job of N instances (only asked for M)"); an argument not
matching `<az>:<number>` or `<number>`; a specified AZ absent from the
job's `azs:` list or from the network's IP pool; a numeric offset that is
negative or out of bounds for the pool size; and an IP already claimed by
another job in the same run ("tried to use IP '`<ip>`', but that address
is already allocated to `<job>`").

## Related documentation

- [Engine overview](../architecture/engine-overview.md)

  Package layout and the merge/eval/param phase model these operators run within.

- [CLI reference](cli.md)

  How `graft merge`/`fan`/`vaultinfo` invoke the engine that runs these operators.

- [Known gaps](../spruce/known-gaps.md)

  Tracked follow-up items, including the vault/AWS/NATS target-extraction placeholder noted above.
