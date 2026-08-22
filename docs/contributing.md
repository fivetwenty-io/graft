# Contributing to Graft

Thank you for your interest in contributing to Graft! This guide covers the contribution process, code style, and development setup.

## Getting Started

### Prerequisites

- Go 1.26 or later

- Git

- Make (optional, for convenience commands)

### Development Setup

```bash
# Clone the repository
git clone https://github.com/fivetwenty-io/graft.git
cd graft

# Install dependencies
go mod download

# Run tests
go test ./...

# Build
go build ./cmd/graft
```

### Project Structure

```
graft/
├── cmd/
│   └── graft/          # CLI entry point
├── pkg/
│   └── graft/          # Public library API
│       ├── engine.go
│       ├── document.go
│       ├── merge.go
│       ├── operators/  # Operator implementations
│       ├── backends/   # Backend implementations
│       └── parser/     # Parser implementation
├── internal/           # Private implementation
├── docs/               # Documentation
└── testdata/           # Test fixtures
```

## Ways to Contribute

### Report Bugs

Open an issue with:

- Clear description of the problem

- Steps to reproduce

- Expected vs actual behavior

- Graft version and environment

- Sample YAML/JSON files if applicable

### Suggest Features

Open an issue with:

- Use case description

- Proposed solution

- Examples of how it would work

### Submit Pull Requests

1. Fork the repository

2. Create a feature branch

3. Make your changes

4. Write tests

5. Submit a pull request

## Code Style

### Go Code

Follow standard Go conventions:

```go
// Package comment
package graft

// Exported types have doc comments
// Engine is the main entry point for graft operations.
type Engine interface {
    // ParseYAML parses YAML bytes into a Document.
    ParseYAML(data []byte) (Document, error)
}

// Unexported helpers don't need doc comments
func parseInternal(data []byte) (*node, error) {
    // Implementation
}
```

### Formatting

```bash
# Format code
go fmt ./...

# Lint (runs golangci-lint at CI's pinned version and toolchain)
make golangci
```

### Naming Conventions

- Use descriptive names

- Avoid abbreviations except common ones (ctx, err, etc.)

- Interface names describe behavior: `Reader`, `Evaluator`, `MergeBuilder`

- Avoid stuttering: `graft.Engine` not `graft.GraftEngine`

### Error Handling

```go
// Use structured errors
return &graft.EvaluationError{
    Operator: "vault",
    Path:     ctx.Path(),
    Message:  "connection failed",
    Cause:    err,
    Hint:     "Check VAULT_ADDR and VAULT_TOKEN",
}

// Wrap errors with context
return fmt.Errorf("parsing %s: %w", path, err)

// Check errors, don't ignore
if err != nil {
    return err
}
```

### Testing

```go
// Table-driven tests
func TestOperator(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    interface{}
        wantErr bool
    }{
        {
            name:  "basic case",
            input: `value: (( grab foo ))`,
            want:  "bar",
        },
        {
            name:    "error case",
            input:   `value: (( grab missing ))`,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}

// Use testify assertions
import "github.com/stretchr/testify/assert"

assert.Equal(t, expected, actual)
assert.NoError(t, err)
assert.ErrorIs(t, err, graft.ErrNotFound)
```

## Pull Request Process

### Before Submitting

1. **Run tests**

   ```bash
   go test ./...
   ```

2. **Run linter**

   ```bash
   make golangci
   ```

   Better yet, install the checked-in git hooks once with `make hooks`;
   the pre-push hook then runs the same lint, security, and test gates
   CI runs, so a push cannot fail CI without failing locally first.

3. **Update documentation** if adding features

4. **Add tests** for new functionality

5. **Update CHANGELOG** if applicable

### PR Guidelines

- Keep PRs focused on a single change

- Write clear commit messages

- Reference related issues

- Respond to review feedback

### Commit Messages

Follow conventional commit format:

```
type(scope): description

[optional body]

[optional footer]
```

**Types:**

- `feat`: New feature

- `fix`: Bug fix

- `docs`: Documentation

- `style`: Formatting

- `refactor`: Code restructuring

- `test`: Adding tests

- `chore`: Maintenance

**Examples:**

```
feat(operators): add jsonpath operator

Adds support for JSONPath expressions in grab operator.
Supports standard JSONPath syntax with array filters.

Closes #123
```

```
fix(vault): handle connection timeout

Previously, vault connections would hang indefinitely.
Now applies configured timeout (default 30s).
```

## Adding Features

### New Operator

1. Create operator file in `pkg/graft/operators/`

2. Implement `Operator` interface

3. Register in operator registry

4. Add tests

5. Add documentation

```go
// pkg/graft/operators/myop.go
package operators

type MyOperator struct{}

func (o *MyOperator) Evaluate(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
    // Implementation
}

func (o *MyOperator) Info() graft.OperatorInfo {
    return graft.OperatorInfo{
        Name:        "myop",
        Description: "Does something useful",
        MinArgs:     1,
        MaxArgs:     2,
        Examples:    []string{`(( myop "arg" ))`},
    }
}
```

### New Backend

1. Create backend file in `pkg/graft/backends/`

2. Implement `Backend` interface

3. Add functional options

4. Create corresponding operator

5. Add tests and documentation

### New Post-Processor

1. Create processor file

2. Implement `PostProcessor` interface

3. Register as option

4. Add tests and documentation

## Testing Guidelines

### Unit Tests

- Test individual functions in isolation

- Use mocks for external dependencies

- Cover edge cases and error conditions

### Integration Tests

- Test component interactions

- Use `// +build integration` tag

- May require external services

```go
// +build integration

func TestVaultIntegration(t *testing.T) {
    if os.Getenv("VAULT_ADDR") == "" {
        t.Skip("VAULT_ADDR required")
    }
    // Test with real Vault
}
```

### Benchmark Tests

```go
func BenchmarkMerge(b *testing.B) {
    engine, _ := graft.NewEngine()
    base, _ := engine.ParseFile("testdata/base.yml")
    overlay, _ := engine.ParseFile("testdata/overlay.yml")

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        engine.Merge(context.Background(), base, overlay).Execute()
    }
}
```

Run benchmarks:

```bash
go test -bench=. -benchmem ./...
```

## Documentation

### Code Documentation

- All exported types, functions, and methods need doc comments

- Use complete sentences

- Include examples where helpful

```go
// ParseYAML parses YAML data into a Document.
// It returns an error if the YAML is malformed or contains
// invalid operator expressions.
//
// Example:
//
//	doc, err := engine.ParseYAML([]byte(`
//	  database:
//	    host: (( grab config.host ))
//	`))
func (e *engine) ParseYAML(data []byte) (Document, error)
```

### User Documentation

- Located in `docs/`

- Use Markdown with Mermaid diagrams

- Follow existing style and structure

- Include practical examples

## Release Process

1. Update version in code

2. Update CHANGELOG

3. Create release branch

4. Run full test suite

5. Tag release

6. Build and publish binaries

## Getting Help

- Open a GitHub issue for bugs or features

- Use discussions for questions

- Check existing issues before creating new ones

## Code of Conduct

- Be respectful and inclusive

- Focus on constructive feedback

- Welcome newcomers

- Assume good intentions

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

## Recognition

Contributors are recognized in:

- CONTRIBUTORS file

- Release notes

- Project documentation

Thank you for contributing to Graft!
