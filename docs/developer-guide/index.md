# Developer Guide

This guide provides comprehensive documentation for developers working with Graft as a Go library. Whether you're embedding Graft in your application, creating custom operators, or building specialized backends, this guide covers all aspects of programmatic Graft usage.

## Overview

Graft is designed as a **library-first** project. The CLI is built on top of a clean, well-documented Go API. This means all functionality is available programmatically, enabling seamless integration into your Go applications.

## Guide Contents

### Library API Reference

Complete reference documentation for all Graft interfaces and types:

- [API Overview](library-api/index.md)

  Architecture, design principles, and quick start guide

- [Engine Interface](library-api/engine.md)

  The main entry point for all Graft operations

- [Document Interface](library-api/document.md)

  Working with parsed YAML/JSON documents

- [MergeBuilder API](library-api/merge-builder.md)

  Fluent API for document merging

- [Diff Interface](library-api/diff-api.md)

  Comparing documents and tracking changes

- [History Interface](library-api/history-api.md)

  Tracking document evolution through operations

- [Configuration Options](library-api/options.md)

  Functional options for engine configuration

### Extending Graft

Guides for extending Graft with custom functionality:

- [Custom Operators](custom-operators.md)

  Creating new operators for document transformation

- [Custom Backends](custom-backends.md)

  Building backends for external data sources

- [Post-Processors](custom-post-processors.md)

  Creating post-processing pipelines

### Integration

Guides for integrating Graft into your applications:

- [Embedding Graft](embedding.md)

  Best practices for embedding in applications

- [Testing with Graft](testing.md)

  Using the mock engine for testing

## Quick Start

Here's a minimal example to get started with the Graft library:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/fivetwenty/graft"
)

func main() {
    // Create a new engine
    engine, err := graft.NewEngine()
    if err != nil {
        log.Fatal(err)
    }

    // Parse documents
    base, err := engine.ParseYAML([]byte(`
name: myapp
database:
  host: localhost
  port: 5432
`))
    if err != nil {
        log.Fatal(err)
    }

    overlay, err := engine.ParseYAML([]byte(`
database:
  host: (( grab $PROD_DB_HOST || "prod.db.example.com" ))
  password: (( vault "secret/db:password" ))
`))
    if err != nil {
        log.Fatal(err)
    }

    // Merge documents
    result, err := engine.Merge(context.Background(), base, overlay).Execute()
    if err != nil {
        log.Fatal(err)
    }

    // Output result
    yaml, _ := result.ToYAML()
    fmt.Println(string(yaml))
}
```

## Design Principles

The Graft library is built on these core principles:

### Clean Public API

All public interfaces are defined in `pkg/graft` with clear documentation and stable APIs. Internal implementation details are hidden in internal packages.

### Functional Options

Configuration uses the functional options pattern for flexible, extensible configuration:

```go
engine, err := graft.NewEngine(
    graft.WithCacheSize(500),
    graft.WithCacheTTL(5 * time.Minute),
    graft.WithVault(vaultConfig),
)
```

### Thread Safety

The `Engine` interface is safe for concurrent use. Multiple goroutines can safely:

- Parse documents
- Execute merges
- Perform diffs
- Evaluate documents

Note that `Document` instances are NOT safe for concurrent modification. Use `Clone()` when you need to work with documents concurrently.

### No CLI Dependencies

The core library has zero dependencies on terminal or CLI packages. This ensures clean embedding in web services, serverless functions, and other non-CLI environments.

## Package Structure

```
pkg/graft/
├── api.go              # Public interfaces
├── engine.go           # Engine implementation
├── document.go         # Document implementation
├── merge.go            # MergeBuilder implementation
├── diff.go             # Diff implementation
├── history.go          # History implementation
├── options.go          # Functional options
├── errors.go           # Error types
├── mock.go             # Testing support
├── interfaces/         # Internal interfaces
├── operators/          # Operator implementations
└── backends/           # Backend implementations
```

## Getting Help

- For API reference, see the [Library API](library-api/index.md) section

- For extending Graft, see [Custom Operators](custom-operators.md), [Custom Backends](custom-backends.md), and [Post-Processors](custom-post-processors.md)

- For integration patterns, see [Embedding](embedding.md) and [Testing](testing.md)
