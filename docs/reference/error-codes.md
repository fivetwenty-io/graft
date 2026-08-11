# Error Codes and Troubleshooting

This reference covers Graft error types, codes, and troubleshooting guidance.

## Error Categories

| Category | Code Range | Description |
|----------|------------|-------------|
| Parse | 100-199 | YAML/JSON parsing errors |
| Evaluation | 200-299 | Operator evaluation errors |
| Merge | 300-399 | Document merge errors |
| Backend | 400-499 | External service errors |
| Validation | 500-599 | Post-processing validation errors |
| System | 900-999 | Internal system errors |

## Parse Errors (100-199)

### E101: Invalid YAML Syntax

```
Error E101: Invalid YAML syntax
  File: config.yml
  Line: 15, Column: 3

  database:
    host: localhost
    port: 5432
      timeout: 30  # Invalid indentation
      ^^^^

  Hint: Check indentation. YAML uses consistent spaces (not tabs).
```

**Causes:**

- Mixed tabs and spaces

- Inconsistent indentation

- Missing colons after keys

- Unclosed quotes

**Resolution:**

- Use spaces consistently (2 or 4)

- Ensure proper key: value syntax

- Close all quotes and brackets

### E102: Invalid Operator Syntax

```
Error E102: Invalid operator syntax
  File: config.yml
  Line: 12, Column: 15

  password: (( vault "secret/db:password" ||
                                          ^
  Expected: expression after '||' operator
  Found: end of line

  Hint: The '||' operator requires a default value.
  Example: (( vault "path:key" || "default" ))
```

**Causes:**

- Incomplete operator expression

- Missing arguments

- Unbalanced parentheses

**Resolution:**

- Complete the expression

- Add required arguments

- Balance `((` and `))`

### E103: Unterminated String

```
Error E103: Unterminated string
  File: config.yml
  Line: 8, Column: 20

  message: (( concat "Hello, World ))
                     ^
  Expected: closing quote

  Hint: Ensure all strings are properly quoted.
```

### E104: Invalid Reference Path

```
Error E104: Invalid reference path
  File: config.yml
  Line: 10, Column: 15

  value: (( grab ..invalid..path ))
               ^
  Invalid path syntax: consecutive dots not allowed
```

## Evaluation Errors (200-299)

### E201: Undefined Reference

```
Error E201: Undefined reference
  File: config.yml
  Line: 15, Column: 18

  url: (( concat host ":" port ))
                 ^^^^
  Reference 'host' not found in document

  Available paths:
    - database.host
    - server.host

  Hint: Did you mean 'database.host'?
```

**Causes:**

- Typo in path name

- Missing required data

- Reference to pruned key

**Resolution:**

- Check path spelling

- Ensure source data exists

- Verify prune/cherry-pick order

### E202: Type Mismatch

```
Error E202: Type mismatch
  File: config.yml
  Line: 20, Column: 12

  total: (( base + "10" ))
                   ^^^^
  Cannot add int and string

  Left operand: 100 (int)
  Right operand: "10" (string)

  Hint: Use numeric types for arithmetic.
  Example: (( base + 10 ))
```

**Causes:**

- Arithmetic on non-numbers

- Boolean operation on wrong type

- Array operation on non-array

**Resolution:**

- Check operand types

- Cast values if needed

- Use appropriate operators

### E203: Circular Reference

```
Error E203: Circular reference detected
  File: config.yml

  Cycle detected:
    foo → bar → baz → foo

  foo: (( grab bar ))   # line 5
  bar: (( grab baz ))   # line 6
  baz: (( grab foo ))   # line 7

  Hint: Remove or restructure one of the circular dependencies.
```

### E204: Required Parameter Missing

```
Error E204: Required parameter missing
  File: config.yml
  Line: 12, Column: 15

  password: (( param "Database password is required" ))
            ^
  This parameter must be provided.

  Hint: Provide a value in an overlay file or environment-specific config.
```

### E205: Operator Not Found

```
Error E205: Unknown operator
  File: config.yml
  Line: 8, Column: 12

  value: (( unknown_op "arg" ))
            ^^^^^^^^^^
  Operator 'unknown_op' is not registered

  Similar operators:
    - vault
    - concat

  Hint: Check operator spelling or register custom operator.
```

### E206: Invalid Argument Count

```
Error E206: Invalid argument count
  File: config.yml
  Line: 10, Column: 12

  result: (( join "," ))
             ^^^^
  Operator 'join' requires 2 arguments, got 1

  Usage: (( join delimiter array ))
  Example: (( join "," items ))
```

### E207: Division by Zero

```
Error E207: Division by zero
  File: config.yml
  Line: 15, Column: 18

  ratio: (( total / count ))
                  ^
  Cannot divide by zero

  total = 100
  count = 0

  Hint: Add a condition to check for zero before division.
  Example: '(( count > 0 ? total / count : 0 ))'
```

## Merge Errors (300-399)

### E301: Incompatible Types

```
Error E301: Cannot merge incompatible types
  File: overlay.yml
  Line: 5

  Path: database.config
  Base type: map
  Overlay type: string

  Base value:
    database:
      config:
        host: localhost
        port: 5432

  Overlay value:
    database:
      config: "connection-string"

  Hint: Use (( replace )) to replace the entire structure.
```

### E302: Array Merge Conflict

```
Error E302: Array merge conflict
  File: overlay.yml
  Line: 10

  Path: users
  Cannot merge arrays: no common key found

  Base array: [{name: alice}, {name: bob}]
  Overlay array: [{id: 1, data: ...}, {id: 2, data: ...}]

  Hint: Specify merge key: (( merge on id ))
  Or use: (( append )), (( prepend )), (( replace ))
```

### E303: Invalid Array Index

```
Error E303: Invalid array index
  File: config.yml
  Line: 12, Column: 22

  item: (( grab items[10] ))
                     ^^
  Array index 10 out of bounds (array length: 3)

  Valid indices: 0, 1, 2
```

## Backend Errors (400-499)

### E401: Vault Connection Failed

```
Error E401: Vault connection failed
  Target: default
  Address: https://vault.example.com

  Connection refused: dial tcp 10.0.0.1:8200: connection refused

  Checklist:
    - [ ] VAULT_ADDR is set correctly
    - [ ] Vault server is running
    - [ ] Network connectivity to Vault
    - [ ] Firewall allows port 8200

  Current configuration:
    VAULT_ADDR: https://vault.example.com
    VAULT_TOKEN: s.xxxxx (set)
```

### E402: Vault Authentication Failed

```
Error E402: Vault authentication failed
  Target: default
  Address: https://vault.example.com

  Error: permission denied

  Checklist:
    - [ ] VAULT_TOKEN is valid and not expired
    - [ ] Token has permissions for the requested path
    - [ ] Token namespace matches VAULT_NAMESPACE

  Hint: Verify token with: vault token lookup
```

### E403: Secret Not Found

```
Error E403: Secret not found
  Target: default
  Path: secret/db:password

  Secret path 'secret/db' does not exist or field 'password' not found

  Checklist:
    - [ ] Path exists in Vault
    - [ ] Field name is correct
    - [ ] Token has read permission

  Hint: List available fields with: vault kv get secret/db
```

### E410: AWS Authentication Failed

```
Error E410: AWS authentication failed
  Service: Parameter Store
  Region: us-west-2

  Error: NoCredentialProviders

  Checklist:
    - [ ] AWS_REGION is set
    - [ ] AWS credentials configured (profile, env vars, or IAM role)
    - [ ] Credentials have required permissions

  Expected permissions:
    - ssm:GetParameter
    - ssm:GetParameters
```

### E411: AWS Parameter Not Found

```
Error E411: AWS parameter not found
  Service: Parameter Store
  Parameter: /app/prod/db_host
  Region: us-west-2

  Error: ParameterNotFound

  Hint: Verify parameter exists:
    aws ssm get-parameter --name "/app/prod/db_host"
```

### E420: NATS Connection Failed

```
Error E420: NATS connection failed
  Target: default
  URL: nats://nats.example.com:4222

  Error: connection refused

  Checklist:
    - [ ] NATS_URL is correct
    - [ ] NATS server is running
    - [ ] Network connectivity
    - [ ] TLS configuration if required
```

### E430: Backend Timeout

```
Error E430: Backend timeout
  Backend: vault
  Target: default
  Operation: get secret/db:password
  Timeout: 30s

  The request timed out after 30 seconds

  Possible causes:
    - Network latency
    - Backend under load
    - Request rate limiting

  Hint: Increase timeout with VAULT_TIMEOUT or retry
```

## Validation Errors (500-599)

### E501: Schema Validation Failed

```
Error E501: Schema validation failed
  Validator: schema-validator

  Errors:
    - database.port: expected integer, got string
    - server.host: required field missing
    - features.flags[0]: invalid enum value "unknown"

  Schema: config-schema.json
```

### E502: Required Fields Missing

```
Error E502: Required fields missing
  Validator: required-fields

  Missing fields:
    - app.name
    - app.version
    - database.host

  Hint: Add missing fields or provide them in an overlay.
```

### E503: Post-Processor Failed

```
Error E503: Post-processor failed
  Processor: custom-validator

  Error: validation rule 'max-replicas' failed
  Path: deployment.replicas
  Value: 100
  Maximum: 50
```

## System Errors (900-999)

### E901: Out of Memory

```
Error E901: Out of memory
  Operation: parsing
  File: large-config.yml
  File size: 500MB

  Hint: Split large files or increase memory limits.
```

### E902: File Not Found

```
Error E902: File not found
  Path: /path/to/missing.yml

  Error: open /path/to/missing.yml: no such file or directory

  Hint: Check path and file permissions.
```

### E903: Permission Denied

```
Error E903: Permission denied
  Path: /etc/secrets/config.yml

  Error: open /etc/secrets/config.yml: permission denied

  Hint: Check file permissions and ownership.
```

## CLI Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error |
| 2 | Parse error (E1xx) |
| 3 | Evaluation error (E2xx) |
| 4 | Backend error (E4xx) |
| 5 | Validation error (E5xx) |
| 126 | Command not executable |
| 127 | Command not found |

## Debugging Tips

### Enable Debug Output

```bash
# Debug logging
export GRAFT_DEBUG=true
graft merge base.yml overlay.yml

# Trace logging (verbose)
export GRAFT_TRACE=true
graft merge base.yml overlay.yml

# Or with flags
graft merge -D base.yml overlay.yml
graft merge -T base.yml overlay.yml
```

### Use Interactive Debugger

```bash
graft debug base.yml overlay.yml

graft> step
graft> inspect database
graft> history database.host
```

### Skip Evaluation for Parsing Issues

```bash
graft merge --skip-eval base.yml overlay.yml
```

### Test Backend Connectivity

```bash
# Vault
vault status
vault kv get secret/test

# AWS
aws sts get-caller-identity
aws ssm get-parameter --name "/test"

# NATS
nats account info
```

## See Also

- [CLI Quick Reference](cli-quick-reference.md) - Debug flags

- [Environment Variables](environment-variables.md) - Configuration

- [Troubleshooting Guide](../user-guide/configuration.md) - Common issues
