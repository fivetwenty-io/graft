# YAML Libraries in Graft

## Overview

Graft uses `gopkg.in/yaml.v3` as its sole YAML library for both parsing
and marshalling. Output marshalling uses a 2-space indent encoder
(`graft.MarshalYAML`) to produce readable YAML.

| Library | Import path | Role |
|---------|-------------|------|
| `gopkg.in/yaml.v3` | `gopkg.in/yaml.v3` | All YAML parsing and marshalling |

`gopkg.in/yaml.v2` appears in go.mod as an indirect dependency (via
`gonvenience/ytbx`, `homeport/dyff`, etc.) but is not imported by any
Graft source file.

---

## Why yaml.v3

yaml.v3 unmarshals mappings as `map[string]interface{}`, which is the
canonical in-memory type throughout Graft's architecture. yaml.v2 returns
`map[interface{}]interface{}`, which cannot be directly JSON-encoded and
requires conversion at every boundary.

## Where it is used

### Parsing (input path)

- `pkg/graft/engine.go` — Core document unmarshalling and evaluation
- `pkg/graft/document.go` — Document type construction
- `pkg/graft/expr_evaluation.go` — Expression result parsing
- `pkg/graft/json.go` — JSON conversion pipeline
- `pkg/graft/interfaces.go` — Literal value parsing
- `pkg/graft/operators/op_load.go` — `(( load ))` operator file loading
- `pkg/graft/operators/operator_helpers.go` — Shared operator utilities
- `cmd/graft/main.go` — CLI input parsing and go-patch definition parsing
- `internal/config/loader.go` — Configuration file loading
- `internal/backends/vault/client.go` — Vault token file parsing
- `internal/backends/nats/client.go` — NATS KV/object-store value parsing
- `pkg/graft/operators/op_aws.go` — AWS secret value parsing

### Marshalling (output path)

- `pkg/graft/yaml.go` — `MarshalYAML()` helper (2-space indent encoder)
- `cmd/graft/main.go` — Final merged tree serialisation, vault reference output
- `pkg/graft/diff.go` — YAML rendering in diff output
- `pkg/graft/operators/op_stringify.go` — `(( stringify ))` operator output
- `pkg/graft/document.go` — `ToYAML()` method

### YAML 1.1 Boolean Compatibility (input path)

- `pkg/graft/yaml_compat.go` — Converts YAML 1.1 boolean strings
  (`yes`/`no`/`on`/`off`) to Go `bool` values during parsing

---

## Output Format

Graft uses yaml.v3's `Encoder` with `SetIndent(2)` for output marshalling
(via the `graft.MarshalYAML()` helper). This produces YAML with:

- 2-space indentation for mappings
- Sequences indented under their parent key (yaml.v3 standard style)
- Full float64 precision for numeric values

---

## Migration History

### Phase 1: simpleyaml removal (completed)

- Removed `github.com/geofffranks/simpleyaml` entirely
- Replaced all `simpleyaml.BytesToYaml` / `SimpleYaml.Get*` usage with
  yaml.v3 direct unmarshal into `map[string]interface{}`

### Phase 2: type system migration (completed)

- Migrated the `tree`, `merger`, `evaluator`, `engine`, `document`, and all
  operator packages from `map[interface{}]interface{}` to
  `map[string]interface{}`
- Removed `convertStringMapToInterfaceMap` helper (no longer needed)
- Consolidated type handlers using Go generics

### Phase 3: geofffranks/yaml removal (completed 2026-03-27)

- Removed `github.com/geofffranks/yaml` (unmaintained 2016-era fork of
  yaml.v2 with cherry-picked fixes from PRs #133 and #195)
- Replaced all 12 callsites (6 Marshal, 6 Unmarshal) with yaml.v3
- Removed `gopkg.in/yaml.v2` as a direct dependency
- Deleted migration documentation test files
  (`yaml_migration_baseline_test.go`, `yaml_v2_v3_compatibility_test.go`)

**Why the fork was originally kept:** The fork incorporated two upstream
bug fixes never merged into official go-yaml, and produced YAML 1.1
compatible output. Investigation revealed that (a) the PR #133 edge case
(empty-string struct tags) did not affect any Graft struct, (b) Graft
already converted YAML 1.1 booleans to Go bools on input so output was
always `true`/`false` regardless of library, and (c) downstream consumers
(Genesis, BOSH) accepted `true`/`false` without issue.
