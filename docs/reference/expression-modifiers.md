# Expression Modifiers

An expression modifier is a suffix written directly on an operator name
that changes how that one call executes, without changing its arguments
or its result value. The syntax is:

```
(( name:modifier args... ))
```

The modifier must be glued to the operator name — no whitespace on either
side of the colon. One modifier is currently defined: `nocache`.

```yaml
password: (( vault:nocache "secret/db:password" ))
```

An unknown modifier is a parse error, not a silent no-op:

```
unknown operator modifier ':bogus' for grab operator (only :nocache is supported)
```

## Combining with @target

A modifier composes with [target syntax](operators.md#external-sources) in
one fixed order: modifier first, target second.

```yaml
# Valid: bypass the cache for a lookup against the production target
password: (( vault:nocache@production "secret/db:password" ))
```

The reverse order, `(( vault@production:nocache ... ))`, is rejected at
parse time — the modifier must directly follow the operator name.

## The nocache modifier

External-source operators cache what they fetch for the duration of a
merge: two references to the same (target, path) produce one backend
request, with concurrent references coalesced (see the caching sections
of the [Vault](../user-guide/secrets/vault.md#caching),
[AWS SSM](../user-guide/secrets/aws-ssm.md#caching),
[AWS Secrets Manager](../user-guide/secrets/aws-secrets-manager.md#caching),
and [NATS](../user-guide/secrets/nats.md#caching) guides).

`:nocache` exempts a single call from that cache, in both directions:

- The call never reads a cached value — it always reaches the backend,
  even when an earlier plain call already fetched the same path.

- The call never writes to the cache — a `:nocache` fetch neither
  seeds a fresh entry nor refreshes an existing one, so it cannot
  poison later plain calls with a value fetched under different timing.

Other references to the same path in the same merge are unaffected:
plain calls still share the cache with each other, under the same cache
keys they always used.

```yaml
# One backend request for these two...
host: (( vault "secret/db:host" ))
host_again: (( vault "secret/db:host" ))

# ...and a separate, guaranteed-fresh request for this one
host_fresh: (( vault:nocache "secret/db:host" ))
```

### Operators that honor nocache

| Operator | Cache bypassed |
|----------|----------------|
| `vault` / `vault-try` | Per-run Vault secret cache |
| `awsparam` | Per-run Parameter Store cache |
| `awssecret` | Per-run Secrets Manager cache |
| `nats` | Per-run KV / Object store cache |

Custom backends registered through the library's backend registry honor
it as well: the registry's caching wrapper skips its cache read and
write for a `:nocache` call.

On every other operator the modifier parses and is inert —
`(( grab:nocache meta.name ))` behaves exactly like
`(( grab meta.name ))`, since `grab` has no backend cache to bypass.

### When to use it

Within one `graft merge` run, backend values almost never change, so the
default caching is what you want. `:nocache` is for the exceptions:

- A secret endpoint that returns a different value per read (a
  one-time-password or token-issuing endpoint), where two references
  must each produce their own value.

- Forcing a re-read of a value another part of the run may have
  observed at a different time.

## Spruce compatibility

Spruce has no modifier syntax. It treats `(( name:anything ... ))` as
literal text and passes it through unevaluated; graft has always
rejected such expressions as errors. The modifier grammar therefore only
assigns meaning to inputs that already failed in graft, and documents
that previously passed through spruce unevaluated now either evaluate
(`:nocache`) or fail with the unknown-modifier parse error above.
