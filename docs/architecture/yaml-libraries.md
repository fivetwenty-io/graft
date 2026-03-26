# YAML Libraries in Graft

## Overview

Graft uses two YAML libraries for distinct, non-overlapping purposes.
The third library (`gopkg.in/yaml.v2`) is present only in test files that
document historical behavior.

| Library | Import path | Role |
|---------|-------------|------|
| `gopkg.in/yaml.v3` | `gopkg.in/yaml.v3` | Primary parse / unmarshal library |
| `geofffranks/yaml` | `github.com/geofffranks/yaml` | Output marshalling (YAML v1.1 format) |
| `gopkg.in/yaml.v2` | `gopkg.in/yaml.v2` | Test-only — documents pre-migration behavior |

---

## `gopkg.in/yaml.v3` — Primary Parse Library

yaml.v3 is the primary library for all YAML parsing and document loading.

### Why yaml.v3

yaml.v3 unmarshals mappings as `map[string]interface{}`, which is the
canonical in-memory type throughout Graft's v2 architecture. yaml.v2 returns
`map[interface{}]interface{}`, which cannot be directly JSON-encoded and
requires conversion at every boundary.

### Where it is used

- `pkg/graft/engine.go` — Core document unmarshalling and evaluation
- `pkg/graft/document.go` — Document type construction
- `pkg/graft/expr_evaluation.go` — Expression result parsing
- `pkg/graft/json.go` — JSON conversion pipeline
- `pkg/graft/operators/op_load.go` — `(( load ))` operator file loading
- `pkg/graft/operators/operator_helpers.go` — Shared operator utilities
- `pkg/graft/yaml.go` — Blank import that anchors the dependency in go.mod
- `cmd/graft/main.go` — CLI input parsing (`parseYAML`)
- `internal/config/loader.go` — Configuration file loading

---

## `github.com/geofffranks/yaml` — Output Marshalling Library

The geofffranks fork of `gopkg.in/yaml.v2` is retained exclusively for
marshalling output back to YAML text. It must not be used for input parsing.

### Why the fork is kept (not replaced by yaml.v3)

The geofffranks fork incorporates two upstream bug fixes that were never
merged into the official yaml.v2 line, and that yaml.v3 handles differently
or not at all:

- **PR #133** — Fixes marshalling of struct fields whose tag value is an empty
  string. Without this fix, such fields are silently dropped from output,
  which breaks round-trip fidelity for Vault token file serialisation.

- **PR #195** — Fixes marshalling of `map[interface{}]interface{}` keys.
  Although Graft's v2 architecture uses `map[string]interface{}` throughout,
  certain external data (Vault token structs, go-patch op definitions) is
  still marshalled via tagged structs where the fork's behaviour is tested and
  stable.

Additionally, yaml.v2 / the geofffranks fork produces YAML 1.1-compatible
output, which is required for interoperability with BOSH and other Cloud
Foundry platform components that consume Graft's output. yaml.v3 produces
YAML 1.2 output, which differs in boolean handling (`y`/`n`/`on`/`off` are
not booleans in YAML 1.2) and can break downstream consumers.

### Where it is used

All usages are marshalling-only (output path):

- `cmd/graft/main.go` — Final merged tree serialisation, go-patch definition
  parsing, vault reference output
- `pkg/graft/diff.go` — YAML rendering in diff output
- `pkg/graft/operators/op_stringify.go` — `(( stringify ))` operator output
- `pkg/graft/operators/op_vault.go` — Vault token file (`.svtoken`) unmarshalling
- `pkg/graft/operators/op_aws.go` — AWS secret value unmarshalling when the
  `?key=` subkey parameter is present
- `pkg/graft/operators/op_nats.go` — NATS KV / object-store value unmarshalling

Note: `op_vault.go`, `op_aws.go`, and `op_nats.go` call `yaml.Unmarshal` via
the fork rather than yaml.v3. These callsites parse small, bounded payloads
(credential files, secret values) into typed structs. The fork is safe here
because the target types are concrete Go structs, not `interface{}` maps, so
the `map[interface{}]interface{}` issue does not arise.

---

## `gopkg.in/yaml.v2` — Test-Only Documentation Dependency

yaml.v2 is imported in two test files that document the pre-migration baseline
and cross-version compatibility. These tests are intentionally retained as
living documentation of why the migration was necessary.

### Where it is used (tests only)

- `pkg/graft/yaml_migration_baseline_test.go` — Documents yaml.v2's
  `map[interface{}]interface{}` output type and JSON-incompatibility
- `pkg/graft/yaml_v2_v3_compatibility_test.go` — Side-by-side comparison of
  yaml.v2 and yaml.v3 parse results

These tests do not exercise production code paths. They exist to preserve
institutional knowledge about the v1→v2 type-system migration.

---

## Migration Status

### Completed (Tasks 1.1–1.10)

- Removed `github.com/geofffranks/simpleyaml` entirely
- Replaced all `simpleyaml.BytesToYaml` / `SimpleYaml.Get*` usage with
  yaml.v3 direct unmarshal into `map[string]interface{}`
- Migrated the `tree`, `merger`, `evaluator`, `engine`, `document`, and all
  operator packages from `map[interface{}]interface{}` to
  `map[string]interface{}`
- Removed `convertStringMapToInterfaceMap` helper (no longer needed)
- Consolidated type handlers using Go generics

### Remaining

- `github.com/geofffranks/yaml` — kept intentionally; see rationale above
- `gopkg.in/yaml.v2` — test-only; kept for documentation value
- `gopkg.in/yaml.v3` — primary library; not a migration target

---

## Future Direction

The long-term goal is to eliminate the `geofffranks/yaml` fork:

1. **Output marshalling** — Replace `yaml.Marshal` callsites in
   `cmd/graft/main.go` and `pkg/graft/diff.go` with a yaml.v3 encoder
   configured with YAML 1.1 boolean rules (via a custom `yaml.Encoder` with
   appropriate style settings). This is non-trivial because yaml.v3 does not
   directly expose 1.1 compatibility mode.

2. **Struct unmarshalling in operators** — The three operator callsites
   (`op_vault`, `op_aws`, `op_nats`) can be migrated to yaml.v3 without risk
   once the output path is confirmed stable.

3. **Test cleanup** — Once the migration window has passed and yaml.v3 is the
   sole library, the `yaml_migration_baseline_test.go` and
   `yaml_v2_v3_compatibility_test.go` files may be removed or archived.

Removing the fork should not be rushed. BOSH and Cloud Foundry ecosystem
tooling relies on YAML 1.1 boolean semantics in Graft output, and the risk
of silent regressions is high.
