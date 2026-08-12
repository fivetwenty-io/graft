# Vault Path Construction

This document previously described a "paren `()` and bar `|` sub-operator" syntax for the vault operator (grouping and OR-choice directly inside a vault path expression, e.g. `vault ("secret/" ("prod" | "dev") ":key")`). That syntax does not exist in the parser; every form of it is a parse error (`expected ')' to close parenthesized expression`, `expected ')', got (`, and similar).

The real ways to build a vault path dynamically are:

- **String concatenation**: space-separated string/reference arguments, or the `concat` operator.
- **`||` for a single default**: `(( vault "secret/app:password" || "default" ))`.
- **`vault-try` for ordered fallback across multiple paths**: tries each path in order and returns the first one that resolves. See `examples/vault-try/`.

## Examples

```yaml
# Concatenation (existing, real syntax)
basic: (( vault "secret/app:password" ))
concat: (( vault "secret/" env "/app:password" ))
with_default: (( vault "secret/app:password" || "default" ))
nested_op: (( vault "secret/" (grab env) "/app:password" ))
semicolon: (( vault "secret/app:password;secret/fallback:password" ))

# vault-try: real path-fallback support
password: (( vault-try "secret/app:password" "secret/fallback:password" "default" ))

# Dynamic path built with concat, then tried with a shared fallback
tenant_secret: (( vault-try (concat "secret/tenants/" tenant ":api_key") "secret/shared:default_api_key" ))
```

See `sub-operators.yml` for the runnable version of these examples.
