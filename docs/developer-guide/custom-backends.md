# Creating Custom Backends

Graft's backend registry lets a library consumer plug a custom secret/parameter
source into the `vault`, `awsparam`, `awssecret`, and `nats` operators, so
`(( vault "..." ))` and friends can resolve against something other than a real
Vault/AWS/NATS instance.

This is gated behind a feature flag, off by default. With the flag off, graft's
behavior is unchanged: the four operators above resolve exclusively through
`internal/backends` (the built-in Vault/AWS/NATS clients), exactly as they did
before this registry existed.

## Enabling the registry

```go
engine, err := graft.NewEngine(
    graft.WithBackendRegistry(true),
)
```

`WithBackendRegistry` is the only way to toggle the flag from outside this
module (it wraps `internal/features.FeatureBackendRegistry`, which is not
importable outside `github.com/fivetwenty-io/graft`). The equivalent
environment variable is `GRAFT_FEATURE_BACKEND_REGISTRY=true`, read by the
`graft` CLI at startup; library callers that construct their own engine should
use `WithBackendRegistry` instead.

With the flag on and no custom backend registered, behavior is still
unchanged: each operator falls back to `internal/backends` when it finds
nothing registered under its own name.

## The `Backend` interface

```go
type Backend interface {
    // Name returns the registry identifier. The vault/awsparam/awssecret/nats
    // operators look up a backend under their own operator name; a backend
    // registered under any other name is never consulted by them.
    Name() string

    // Get retrieves the value at path, passed exactly as it appears in the
    // operator call (e.g. the full "secret/db:password" string for
    // `(( vault "secret/db:password" ))`) - the backend, not graft, owns how
    // to interpret it.
    Get(ctx context.Context, path string) (interface{}, error)

    // GetBatch retrieves multiple paths. No graft operator calls this today
    // - see "Batching" below.
    GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error)

    // Health reports whether the backend is reachable. Not called by any
    // operator; it exists for callers that hold an Engine and want to check
    // connectivity via Engine.GetBackend directly.
    Health(ctx context.Context) error

    // Close releases backend resources. Not called automatically by
    // UnregisterBackend or engine teardown - callers that construct a
    // Backend with resources to release own its Close call.
    Close() error
}
```

A missing path is reported by returning an error for which
`errors.Is(err, graft.ErrBackendNotFound)` is true (returning
`graft.ErrBackendNotFound` itself, or a wrapped copy, both work). The vault
operator maps that into the exact `secret <path> not found` text the built-in
Vault backend produces, so the Genesis compatibility contract's not-found
detection keeps working for custom backends too. `awsparam`/`awssecret`/`nats`
have no equivalent pinned not-found shape - a not-found result from a custom
backend registered under those names surfaces the same way any other backend
failure does (see "Errors" below).

### Optional: `@target` support

```go
type TargetedBackend interface {
    Backend
    GetWithTarget(ctx context.Context, target, path string) (interface{}, error)
}
```

Implement this on top of `Backend` (a type assertion, `if tb, ok :=
backend.(graft.TargetedBackend); ok`) to support `(( vault@production
"secret/path:key" ))`-style calls. A backend that does not implement
`TargetedBackend` and receives a call with a non-empty target fails with a
configuration error ("backend %q does not support @target selection") rather
than silently using the wrong instance or silently ignoring the target.

## A minimal implementation

```go
package backends

import (
    "context"

    "github.com/redis/go-redis/v9"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

// RedisBackend implements graft.Backend for Redis.
type RedisBackend struct {
    client *redis.Client
    prefix string
}

func NewRedisBackend(addr, prefix string) *RedisBackend {
    return &RedisBackend{
        client: redis.NewClient(&redis.Options{Addr: addr}),
        prefix: prefix,
    }
}

func (b *RedisBackend) Name() string { return "vault" } // registered as the vault operator's backend

func (b *RedisBackend) Get(ctx context.Context, path string) (interface{}, error) {
    val, err := b.client.Get(ctx, b.prefix+path).Result()
    if err == redis.Nil {
        return nil, graft.ErrBackendNotFound
    }
    if err != nil {
        return nil, err // wrapped by the operator into *graft.BackendError; see "Errors"
    }
    return val, nil
}

func (b *RedisBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
    // No graft operator calls GetBatch - see "Batching" below. A loop over
    // Get, via graft.SequentialGetBatch, is the recommended default.
    return graft.SequentialGetBatch(ctx, paths, b.Get)
}

func (b *RedisBackend) Health(ctx context.Context) error { return b.client.Ping(ctx).Err() }
func (b *RedisBackend) Close() error                     { return b.client.Close() }
```

## Registering a backend

At engine construction:

```go
engine, err := graft.NewEngine(
    graft.WithBackendRegistry(true),
    graft.WithBackend(NewRedisBackend("localhost:6379", "config:")),
)
```

At runtime:

```go
engine, err := graft.NewEngine(graft.WithBackendRegistry(true))
// ...
err = engine.RegisterBackend(NewRedisBackend("localhost:6379", "config:"))

backend, exists := engine.GetBackend("vault")
names := engine.ListBackends()
err = engine.UnregisterBackend("vault")
```

Re-registering under an already-used name silently replaces the previous
backend (no error) - useful for test setup that reseeds between cases.

## Which name to register under

The four built-in operators each consult exactly one registry name:

| Operator call                | Registry name |
|-------------------------------|---------------|
| `(( vault "..." ))`           | `"vault"`     |
| `(( awsparam "..." ))`        | `"awsparam"`  |
| `(( awssecret "..." ))`       | `"awssecret"` |
| `(( nats "..." ))`            | `"nats"`      |

Registering under any other name makes the backend reachable via
`Engine.GetBackend` (for a custom operator you write yourself), but none of
the built-in operators will consult it.

Writing your own `"vault"`/`"awsparam"`/`"awssecret"` `Backend` (as the rest
of this page does) is one way to fill those names. `graft.WithVault`/
`graft.WithVaultTarget` and `graft.WithAWS`/`graft.WithAWSTarget`
(`docs/developer-guide/library-api/options.md#backend-configuration-options`)
are a ready-made alternative: they register real Vault/AWS-backed
implementations built directly on `github.com/hashicorp/vault/api` and
`github.com/aws/aws-sdk-go` from a config struct, so most callers who just
want per-engine Vault/AWS configuration - as opposed to routing to an
entirely different secret store - never need to implement `Backend`
themselves for those three names. There is no `WithNATS` equivalent yet; a
custom `"nats"` `Backend` still has to be hand-written.

`awsparam` and `awssecret` are looked up separately: a backend registered as
`"awssecret"` is never consulted by `(( awsparam ... ))`, and vice versa.

A `(( awsparam ... ))`/`(( awssecret ... ))` call's argument can carry a
`?stage=...&version=...&key=...` query suffix (e.g.
`(( awssecret "prod/db?stage=AWSPREVIOUS&key=password" ))`). The operator
strips that suffix before calling your backend's `Get`/`GetWithTarget`, so
`path` receives only `"prod/db"` - `stage`/`version` are never passed to a
custom `"awssecret"` backend at all (the built-in path passes them to the
AWS SDK directly; there is no equivalent parameter on `Backend.Get` for a
custom backend to receive them through). `?key=...` subkey extraction runs
afterward, uniformly, against whatever value your `Get` returned -
`fmt.Sprintf("%v", ...)`'d, then `yaml.Unmarshal`'d as a map, then indexed
by `key` - regardless of whether that value came from a custom backend or
the built-in path.

`(( vault ... ))` performs no equivalent stripping: the full
`"secret/path:key"` string, colon subkey included, reaches your backend's
`Get`/`GetWithTarget` unchanged (see `Backend.Get`'s doc comment).

`(( nats "kv:path" {"url": "..."} ))`'s optional second argument is
`internal/backends/nats`'s own connection configuration (URL, timeout,
TLS, ...); a `Backend` has no equivalent parameter to receive it through
(`Get`/`GetWithTarget` take only a context and path). Calling `(( nats
... ))` with a second argument against a registered `"nats"` backend is a
configuration error rather than a silently ignored argument.

The `vault`/`awsparam`/`awssecret` operators pass the value your `Get`
implementation returns through `fmt.Sprintf("%v", ...)` if it is not already a
`string` (matching the built-in Vault/AWS paths, which always resolve to a
string). The `nats` operator does not: it returns whatever `interface{}` your
backend produced unchanged, matching the built-in NATS KV/Object paths, which
can return maps and other structured values.

## Errors

A non-not-found error returned from `Get`/`GetWithTarget` is wrapped as a
`*graft.GraftError{Type: graft.ExternalError}` carrying a `*graft.BackendError`
as its `Cause`, reachable with `errors.As`:

```go
var backendErr *graft.BackendError
if errors.As(err, &backendErr) {
    fmt.Println(backendErr.Backend, backendErr.Target, backendErr.Path, backendErr.Message)
}
```

`*graft.BackendError` is never the outermost error an operator returns - only
reachable by unwrapping. `BackendError.Error()` is not part of any
compatibility contract and may change; only the vault not-found string
described above is pinned.

```go
type BackendError struct {
    Backend string
    Target  string
    Path    string
    Message string
    Cause   error
}
```

## Generic retry, caching, and audit logging

The registry can wrap a registered backend's `Get`/`GetWithTarget` calls with
retry, caching, and audit-logging behavior, without the backend implementing
any of that itself:

```go
engine, err := graft.NewEngine(
    graft.WithBackendRegistry(true),
    graft.WithBackend(NewRedisBackend("localhost:6379", "config:")),
    graft.WithBackendRetry("vault", graft.RetryConfig{
        MaxAttempts:     3,
        InitialInterval: 100 * time.Millisecond,
        Multiplier:      2,
    }),
    graft.WithBackendCache("vault", myCache), // implements graft.BackendCache
    graft.WithAuditLogger(myAuditLogger),     // implements graft.AuditLogger
)
```

- `WithBackendRetry(name, cfg)` retries a failed call up to `cfg.MaxAttempts`
  times. The delay between attempts starts at `cfg.InitialInterval` and is
  scaled by `cfg.Multiplier` after each attempt, capped at
  `cfg.MaxInterval` - the example above (`Multiplier: 2`) backs off
  exponentially; `cfg.Multiplier` defaults to `1` (a constant delay) when
  left at its zero value, not exponential backoff. A result for which
  `errors.Is(err, graft.ErrBackendNotFound)` is true is not retried by
  default (a "definite not found" won't change on retry); supply
  `cfg.RetryableErrors` to override this.
- `WithBackendCache(name, c)` caches successful results, keyed by `path` (or
  `"target\x00path"` for `GetWithTarget`), with a fixed
  `graft.DefaultBackendCacheTTL` (5 minutes). `c` must implement
  `graft.BackendCache` (`Get`/`Set`/`Delete`/`Clear`) - graft does not ship a
  default cache implementation.
- `WithAuditLogger(l)` calls `l.LogAccess(ctx, backendName, path, success,
  err)` once per `Get`/`GetWithTarget` call, cache hit or miss.

All three apply only to `Get`/`GetWithTarget` - not `GetBatch` (see
"Batching" below) - and only to backends actually registered in the registry.
They do not touch `internal/backends`' own, separate behavior (e.g.
`nats.Config.AuditLogging`, the NATS backend's native audit trail, or Vault's
built-in per-path caching): those remain exactly as before regardless of
these options, since built-in backends are never placed in the registry (see
"Design notes" below).

## Batching

`Backend.GetBatch` exists on the interface because a batch API is a
reasonable thing for a secret-store client to offer, but **no graft operator
calls it**: vault/awsparam/awssecret/nats each resolve one path per operator
call, and there is no batching call site anywhere in graft to design a real
batched fetch against. Implement it with `graft.SequentialGetBatch`, which
loops `Get` once per path and omits not-found paths from the result:

```go
func (b *RedisBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
    return graft.SequentialGetBatch(ctx, paths, b.Get)
}
```

If your own code calls `GetBatch` directly (graft never does), a real
batched implementation is fine too - just note it will not be consulted by
any built-in operator path.

## Testing

For unit tests that must not reach a real backend, prefer
[`graft.NewMockEngine`](testing.md) over hand-rolling a `Backend`: it already
registers seedable vault/awsparam/awssecret/nats backends and records every
call for `VaultCalls()`/`AWSCalls()`/`NATSCalls()`/`WasVaultCalled()`
assertions.

```go
engine := graft.NewMockEngine()
engine.MockVault("secret/db:password", "test-password")

doc, _ := engine.ParseYAML([]byte(`password: (( vault "secret/db:password" ))`))
result, err := engine.Evaluate(context.Background(), doc)
```

If you are testing your own `Backend` implementation directly rather than
through `MockEngine`, register it and evaluate a document the same way
`ExampleWithBackend` (`pkg/graft/examples_doc_test.go`) does.

## Design notes

- **The registry starts empty; there are no built-in adapters.**
  `internal/backends/{vault,aws,nats}` import `pkg/graft` (for `graft.Engine`
  and debug logging), so `pkg/graft` cannot import them back to construct
  wrapping `Backend` adapters without an import cycle. Instead, each operator
  consults the registry first and falls back to its existing
  `internal/backends` call when nothing is registered under its name -
  observably identical to "seeded with a pass-through adapter that was never
  overridden," with a simpler dependency graph.
- **`CacheManager`, cross-backend cache administration (stats,
  invalidate-by-prefix, clear-all), is not implemented.** Only per-backend
  `BackendCache` (above) is. If you need cross-backend cache administration,
  hold onto the `BackendCache` instances you pass to `WithBackendCache`
  yourself.
- **`aws.ClientPool.GetTargetConfig`/`nats.ClientPool.GetTargetConfig` do not
  take an engine parameter** (unlike `vault.ClientPool.getTargetConfig`,
  which already reserves one). Bringing the two into line with vault's
  engine-aware form is independent of this registry - nothing here
  constructs adapters that would need it - and is left for whoever
  eventually builds real engine-sourced target configuration for AWS/NATS.
