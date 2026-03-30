# YAML Libraries in Graft

## Overview

Graft uses `github.com/goccy/go-yaml` as its sole YAML library for both
parsing and marshalling. Output marshalling uses a 2-space indent encoder
(`graft.MarshalYAML`) to produce readable YAML.

| Library | Import path | Role |
|---------|-------------|------|
| `github.com/goccy/go-yaml` | `github.com/goccy/go-yaml` | All YAML parsing and marshalling |

`gopkg.in/yaml.v3` and `gopkg.in/yaml.v2` appear in go.mod as indirect
dependencies (via `gonvenience/ytbx`, `homeport/dyff`, etc.) but are not
imported by any Graft source file.

---

## Why goccy/go-yaml

goccy/go-yaml is an actively maintained, YAML 1.2 compliant library with
better error messages and performance. It unmarshals mappings as
`map[string]interface{}`, which is the canonical in-memory type throughout
Graft's architecture. It replaced `gopkg.in/yaml.v3`, which is no longer
actively maintained.

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

Graft uses goccy/go-yaml's `Encoder` with `yaml.Indent(2)` option for
output marshalling (via the `graft.MarshalYAML()` helper). This produces
YAML with:

- 2-space indentation for mappings

- Sequences indented under their parent key

- Full float64 precision for numeric values

- YAML 1.1 boolean strings (`yes`/`no`/`on`/`off`) quoted in output for
  safety

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

### Phase 4: goccy/go-yaml migration (completed 2026-03-30)

- Replaced `gopkg.in/yaml.v3` with `github.com/goccy/go-yaml` (v1.19.2)
  across all 26 source files (17 production, 9 test)

- Moved `gopkg.in/yaml.v3` to indirect-only dependency

- Three API changes required:

  1. **Encoder option:** `enc.SetIndent(2)` → `yaml.NewEncoder(w, yaml.Indent(2))`
     in `yaml.go`, `op_stringify.go`, `main_test.go`

  2. **Integer types:** goccy returns `uint64`/`int64` instead of `int`.
     Extended `normalizeValue()` and `convertAny()` in `yaml_compat.go` to
     convert these to `int` in the post-unmarshal pipeline

  3. **Inject key quoting:** goccy rejects Graft's `<<<:` inject key
     (interprets `<<<` as a variant of YAML merge key `<<`). Added
     `QuoteInjectKeys()` pre-processor in `yaml_compat.go` that quotes
     `<<<:` keys before parsing

- YAML 1.1 boolean compatibility (`yes`/`no`/`on`/`off` → `bool`)
  continues to work via the existing `YAMLCompat.ConvertMapValues()` layer,
  which became essential rather than redundant after this migration (goccy
  follows YAML 1.2 and treats these as strings)

- Test fixture updates: replaced literal tab/newline characters in
  double-quoted YAML strings with `\t`/`\n` escape sequences for strict
  YAML 1.2 compliance

**Why goccy/go-yaml:** The `gopkg.in/yaml.v3` library is no longer
actively maintained. goccy/go-yaml is actively maintained, fully YAML 1.2
compliant, provides better error messages, and has compatible API
signatures for the three patterns Graft uses (`Marshal`, `Unmarshal`,
`Encoder`). The SHIELD backup platform completed the same migration
successfully, providing reference patterns.
