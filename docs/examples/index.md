# Examples

This section provides practical, runnable examples demonstrating Graft's capabilities across common use cases.

## Example Categories

### [Basic Merging](basic-merging.md)

Step-by-step examples of merging YAML files, from simple overlays to complex multi-file scenarios.

- Simple key overrides
- Deep merge behavior
- Array merging strategies
- Using `grab` to reference values
- Computed values with operators

### [Inspecting a Merge](inspecting-a-merge.md)

Debugging a merge that fails, using `graft debug` and `graft diff` on fixtures shipped in the repository.

- Stepping through a merge one file at a time
- Working past an operator you cannot resolve
- Breakpoints on a suspicious path
- Tracing which file set which value
- Comparing two rendered environments

### [Conditional Configurations](conditional-configs.md)

Dynamic configurations using control flow operators.

- `if/elif/else/fi` conditionals
- `for/done` iteration
- `while/done` loops
- `case/when/esac` pattern matching
- Nested control flow

### [Multi-Environment Setups](multi-environment.md)

Managing configurations across development, staging, and production environments.

- Shared base configurations
- Environment-specific overlays
- Environment detection patterns
- Resource scaling by environment
- Feature flags per environment

### [Secrets Management](secrets-management.md)

Integrating with external secrets backends.

- HashiCorp Vault integration
- AWS Parameter Store usage
- AWS Secrets Manager usage
- NATS JetStream KV/Object stores
- Multi-target configurations
- Fallback and default values

### [CI/CD Integration](ci-cd-integration.md)

Using Graft in continuous integration and deployment pipelines.

- GitHub Actions workflows
- GitLab CI pipelines
- Jenkins integration
- Dynamic configuration generation
- Environment variable injection
- Validation in pipelines

### [Web Service Embedding](web-service-embedding.md)

Embedding Graft as a Go library in web services.

- Basic library usage
- Building a configuration API
- Caching strategies
- Custom operator registration
- Testing with mocks

## Running the Examples

All examples in this section are designed to be runnable. To follow along:

1. Create the YAML files shown in each example
2. Run the `graft` commands as shown
3. Observe the output and compare with expected results

### Quick Setup

```sh
# Create a working directory
mkdir graft-examples && cd graft-examples

# Verify graft is installed
graft --version
```

## Example Conventions

Throughout these examples, you will see:

- **Input files** shown with their filename as a comment or header
- **Commands** in code blocks with `sh` syntax
- **Output** clearly marked and formatted
- **Explanations** describing what each example demonstrates

### File Naming Convention

```
base.yml          # Base configuration
dev.yml           # Development overrides
staging.yml       # Staging overrides
production.yml    # Production overrides
secrets.yml       # Secrets (operators only, no real secrets)
```

## Tips for Learning

1. **Start with Basic Merging**

   Understanding merge behavior is fundamental to using Graft effectively.

2. **Experiment with Operators**

   Try modifying the examples to see how different operators behave.

3. **Use --history**

   The `--history` flag helps understand where values come from:
   ```sh
   graft merge --history base.yml overlay.yml
   ```

4. **Use --skip-eval**

   See raw operators before evaluation:
   ```sh
   graft merge --skip-eval config.yml
   ```

5. **Check the Reference**

   For complete operator syntax, see the [Operator Quick Reference](../reference/operator-quick-reference.md).

## See Also

- [Quick Start](../getting-started/quick-start.md) - Get started in 5 minutes
- [User Guide](../user-guide/) - Comprehensive documentation
- [Reference](../reference/) - Quick reference tables
