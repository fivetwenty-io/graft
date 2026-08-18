# Extras Beyond Spruce

Graft is a spruce-compatible merge engine, but it also carries a set of capabilities spruce does not have: caching, metrics, parallel evaluation, memory pools, and secrets backends beyond Vault. This page documents those extras, what they do, and (since not all of them are configurable through the CLI yet) which ones you can actually turn on or off today.

None of the capabilities on this page are required for spruce-compatible merges. A plain `graft merge base.yml overlay.yml` run behaves like spruce whether or not these extras are active; they're additive.

## Caching

Graft has two independent caching layers that are easy to conflate because both affect the same kind of workload (repeated secret lookups), but only one of them is configurable.

**Backend secret/parameter caching** is what makes a document that references the same `vault`, `awsparam`, `awssecret`, or `nats` path twice make one backend call instead of two. It lives inside each backend package (`internal/backends/vault`, `internal/backends/aws`, `internal/backends/nats`), keyed by target name plus path so the same path against two different Vault instances, AWS accounts, or NATS clusters never collides. It is unconditional: there is no config field or environment variable that turns it off, and it holds every distinct key it sees for the life of the process (or, for `nats`, until that key's TTL expires) rather than a fixed entry count.

Under parallel evaluation, concurrent requests for the *same* target-namespaced key are also coalesced into a single backend call rather than each independently missing the cache and fetching: if a wave contains ten operators all resolving `(( vault "secret/db:password" ))`, the first one to run triggers the fetch and the other nine wait on and share its result. Requests for *different* targets or different paths are never coalesced together — each is its own backend call, dispatched concurrently with the others in the same wave (see [Parallel Execution Model](../architecture/parallelism.md)).

**General operator-result caching** is a separate, general-purpose in-memory cache (`internal/cache`, sharded across 16 internal shards, up to 1,000 entries by default) that the engine constructs on every run. `GRAFT_FEATURE_CACHE=false` disables it. Today, though, no CLI command's operator evaluation path reads from or writes to it — `vault`, `awsparam`, `awssecret`, and `nats` lookups go through their own backend-level caches described above, not this one. Setting `GRAFT_FEATURE_CACHE` therefore has no observable effect on `graft merge`, `graft fan`, or `graft vaultinfo` today. The `cache.enabled`, `cache.max_size`, and `cache.ttl` config fields and their `GRAFT_CACHE_*` environment variables are parsed and validated but likewise not wired into the CLI; see [Which Settings Actually Affect a Merge](../reference/config.md#which-settings-actually-affect-a-merge) for the full breakdown. `cache.l2_enabled` and `cache.l2_path` *are* wired: they control the persistent merge cache described next.

```bash
# Backend secret caching + request dedup is always on; there's nothing to
# toggle for it. GRAFT_FEATURE_CACHE only affects the separate, currently
# unused general operator-result cache.
graft merge base.yml overlay.yml
```

**Persistent merge cache** is an opt-in, disk-backed cache that carries work across graft invocations — the other two caches above live and die with a single process. Enable it with `GRAFT_CACHE_L2_ENABLED=true` (or `cache.l2_enabled: true` in the config file). Entries land under `GRAFT_CACHE_L2_PATH` if set, otherwise under the OS user cache directory (`~/.cache/graft` on Linux, `~/Library/Caches/graft` on macOS).

It has two layers:

- The **output cache** replays a previous run's exact stdout and stderr bytes when a merge is invoked again with byte-identical input documents, identical flags, and the same graft version — without parsing, merging, or evaluating anything. Only *pure* invocations are stored: any operator that consults an external system (`vault`, `awsparam`, `awssecret`, `nats`), the filesystem (`file`, `load`), the environment (`raw_env`, `$VAR` references), or randomness (`shuffle`), and any document with control-flow markers, disqualifies the run from being cached (it still merges normally, and still benefits from the parse layer for its pure documents).

- The **parse cache** stores each input document's parsed tree keyed by a hash of its bytes, so a repeat invocation that misses the output cache — a Genesis run where one overlay changed, say — still skips re-parsing every unchanged document.

Both layers are content-addressed: keys hash the input bytes themselves, never paths or mtimes, so Genesis-style temp files hit across runs and any edit misses. A cached result is guaranteed byte-identical to what a cache-off run would produce — CI enforces this across the whole example corpus (`scripts/cache-identity.sh`). Debug and trace runs (`--debug`, `--trace`) bypass the cache entirely so their diagnostics always come from a real merge, and cache trouble (unwritable directory, corrupt entry) is never an error — graft just merges without it. Entries expire after seven days purely as housekeeping.

Inspect or reset it with the `cache` subcommands:

```bash
GRAFT_CACHE_L2_ENABLED=true graft merge base.yml overlay.yml

graft cache stats   # per-layer entry counts and sizes
graft cache clear   # drop all stored entries
```

On a heavy Genesis-style merge (one large manifest plus 45 overlays), a warm output-cache hit turns a ~230 ms merge into ~30 ms.

## Metrics

Graft has an internal metrics package capable of collecting counters, gauges, and histograms covering operation counts, timing, and resource usage, and exporting them as Prometheus, OpenTelemetry, JSON, or plain text.

None of that is reachable from the CLI today. `graft merge`, `graft fan`, and `graft vaultinfo` never turn metrics collection on, so `metrics.enabled` (config file or `GRAFT_METRICS_ENABLED`) and the `metrics` feature flag (`GRAFT_FEATURE_METRICS`) currently have no observable effect. The values are accepted and validated, but nothing consumes them. See [Which Settings Actually Affect a Merge](../reference/config.md#which-settings-actually-affect-a-merge) for the full picture.

## Parallel Evaluation

Spruce evaluates operators one at a time. Graft evaluates independent operations concurrently: a dependency-graph scheduler groups operators with no unmet dependency into waves, and within a wave, every operator's own work — including any Vault, AWS, or NATS network call — runs on its own goroutine at the same time as the rest of the wave. Applying each operator's result to the document tree is a separate, always-serialized step, since a plain Go map is not safe for concurrent writes; it happens after the wave's concurrent work finishes, in a fixed order (sorted by path) so the final document and any recorded history never depend on which goroutine's network call happened to finish first. A handful of operators whose behavior depends on relative execution order rather than a data dependency — currently only `static_ips`, claiming from a shared address pool — opt out of concurrent dispatch and run one at a time within their wave, exactly as they did before parallel evaluation existed. See [Parallel Execution Model](../architecture/parallelism.md) for the full design.

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
