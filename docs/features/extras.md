# Extras Beyond Spruce

Graft is a spruce-compatible merge engine, but it also carries a set of capabilities spruce does not have: caching, metrics, parallel evaluation, memory pools, and secrets backends beyond Vault. This page documents those extras, what they do, and (since not all of them are configurable through the CLI yet) which ones you can actually turn on or off today.

None of the capabilities on this page are required for spruce-compatible merges. A plain `graft merge base.yml overlay.yml` run behaves like spruce whether or not these extras are active; they're additive.

## Caching

Graft caches the results of repeated operator lookups, most usefully repeated `vault`, `awsparam`, `awssecret`, and `nats` calls for the same key within a single run, so a document that references the same secret path twice makes one backend call instead of two.

Caching is enabled by default. Each run gets an in-memory cache, sharded across 16 internal shards to reduce lock contention under parallel evaluation, holding up to 1,000 entries with no expiration.

Turn caching off with `GRAFT_FEATURE_CACHE=false`. This is currently the only setting (config file or environment variable) that changes whether caching runs. The `cache.*` config file fields and their `GRAFT_CACHE_*` environment variables (`enabled`, `max_size`, `ttl`, `l2_enabled`, `l2_path`) are parsed and validated but not yet wired into the CLI, so setting them has no effect on cache size, expiration, or an L2 disk tier; see [Which Settings Actually Affect a Merge](../reference/config.md#which-settings-actually-affect-a-merge) for the full breakdown. The underlying cache package also supports disk-backed (L2) storage, cache warming, and hit-rate analytics, but the CLI doesn't expose those today.

```bash
# Default: caching on.
graft merge base.yml overlay.yml

# Disable caching for this run.
GRAFT_FEATURE_CACHE=false graft merge base.yml overlay.yml
```

## Metrics

Graft has an internal metrics package capable of collecting counters, gauges, and histograms covering operation counts, timing, and resource usage, and exporting them as Prometheus, OpenTelemetry, JSON, or plain text.

None of that is reachable from the CLI today. `graft merge`, `graft fan`, and `graft vaultinfo` never turn metrics collection on, so `metrics.enabled` (config file or `GRAFT_METRICS_ENABLED`) and the `metrics` feature flag (`GRAFT_FEATURE_METRICS`) currently have no observable effect. The values are accepted and validated, but nothing consumes them. See [Which Settings Actually Affect a Merge](../reference/config.md#which-settings-actually-affect-a-merge) for the full picture.

## Parallel Evaluation

Spruce evaluates operators one at a time. Graft can evaluate independent operations concurrently: a copy-on-write document tree means concurrent workers never share mutable state, and a dependency-graph scheduler runs each operation only once everything it depends on has finished.

Parallel evaluation is **enabled by default**. Graft derives the default worker count from the host's CPU count (`runtime.NumCPU()`) rather than using a fixed number, so it scales concurrency to the machine it runs on instead of applying a one-size-fits-all worker count.

```bash
# Uses a CPU-derived worker count by default.
graft merge base.yml overlay.yml

# Override the worker ceiling explicitly.
GRAFT_PARALLEL_MAX_WORKERS=4 graft merge base.yml overlay.yml

# Disable parallel evaluation entirely (sequential, spruce-like ordering of work).
GRAFT_PARALLEL_ENABLED=false graft merge base.yml overlay.yml
```

Output is deterministic regardless of worker count: map keys and list elements retain a stable order across repeated runs, so enabling or tuning parallel evaluation never changes what a merge produces, only how fast it produces it.

Configure parallel evaluation through the `parallel.*` fields in a config file or the matching `GRAFT_PARALLEL_*` environment variables. There's also a `parallel_evaluation` feature flag (`GRAFT_FEATURE_PARALLEL`), but it has no effect through the CLI: `parallel.enabled` always wins the on/off decision, regardless of what the feature flag is set to. Full field reference and precedence details: [Configuration Reference](../reference/config.md).

## Memory Pools

Graft reuses interned strings across a merge run instead of allocating fresh ones for every operation, cutting garbage-collector pressure on large documents or high-throughput embedding scenarios. This is transparent: it changes allocation behavior, not merge output.

This pooling is always on and isn't currently configurable. The `memory_pools` feature flag (`GRAFT_FEATURE_POOLS`) exists and resolves normally, but nothing in the CLI's code path reads it, so setting it has no observable effect either way.

## Secrets Backends Beyond Vault

Spruce resolves secrets from HashiCorp Vault only. Graft adds two more backends, each with its own operator:

| Backend | Operators | Purpose |
|---|---|---|
| AWS | `awsparam`, `awssecret` | Fetch values from AWS Systems Manager Parameter Store and AWS Secrets Manager, respectively. |
| NATS | `nats` | Fetch values from a NATS JetStream key-value bucket. |

Both backends support connection pooling and per-target configuration (multiple named AWS regions/profiles or NATS servers in the same document), the same way graft's `vault` operator supports named Vault targets. See [Secrets Management](../user-guide/secrets/index.md) for operator syntax and per-backend environment variables.

## See Also

- [Configuration Reference](../reference/config.md) — full field, environment-variable, and precedence reference for everything on this page

- [Secrets Management](../user-guide/secrets/index.md) — Vault, AWS, and NATS operator syntax and credentials

- [Architecture Overview](../architecture/index.md) — how caching, parallel evaluation, and backend pooling fit into graft's processing pipeline
