# Data Manipulation Operators

Operators for referencing and transforming data within your configuration.

## grab

Reference a value from elsewhere in the document.

**Syntax:**
```yaml
value: (( grab path.to.value ))
```

**Examples:**
```yaml
defaults:
  timeout: 30
  host: localhost

server:
  timeout: (( grab defaults.timeout ))    # 30
  host: (( grab defaults.host ))          # "localhost"
```

**Array Access:**
```yaml
servers:
  - name: primary
    host: primary.example.com
  - name: secondary
    host: secondary.example.com

# By index
first_host: (( grab servers.0.host ))     # "primary.example.com"

# By key match
primary: (( grab servers.name=primary.host ))  # "primary.example.com"
```

**With Default:**
```yaml
# Use default if path doesn't exist
host: (( grab config.host || "localhost" ))
```

## concat

Concatenate strings and values.

**Syntax:**
```yaml
value: (( concat arg1 arg2 ... ))
```

**Examples:**
```yaml
name: my-app
env: production

# Simple concatenation
full_name: (( concat name "-" env ))      # "my-app-production"

# With grab
url: (( concat "https://" (grab host) ":" (grab port) ))

# Building complex strings
connection: (( concat
    "postgres://"
    (grab db.user) ":"
    (grab db.password) "@"
    (grab db.host) ":"
    (grab db.port) "/"
    (grab db.name)
))
```

## join

Join array elements with a delimiter.

**Syntax:**
```yaml
value: (( join delimiter array ))
```

**Examples:**
```yaml
hosts:
  - server1.example.com
  - server2.example.com
  - server3.example.com

# Join with comma
host_list: (( join ", " hosts ))
# "server1.example.com, server2.example.com, server3.example.com"

# Join paths
path_parts:
  - /usr
  - local
  - bin
full_path: (( join "/" path_parts ))      # "/usr/local/bin"
```

## split

Split a string into an array.

**Syntax:**
```yaml
value: (( split delimiter string ))
```

**Examples:**
```yaml
csv_line: "apple,banana,cherry"

fruits: (( split "," csv_line ))
# ["apple", "banana", "cherry"]

# With regex (PCRE)
text: "one  two   three"
words: (( split "\\s+" text ))            # ["one", "two", "three"]
```

## stringify

Convert any value to its YAML string representation.

**Syntax:**
```yaml
value: (( stringify data ))
```

**Examples:**
```yaml
config:
  database:
    host: localhost
    port: 5432

# Convert to string
config_string: (( stringify config ))
# "database:\n  host: localhost\n  port: 5432\n"

# Useful for embedding as text
configmap:
  data:
    config.yml: (( stringify app_config ))
```

## keys

Extract keys from a map as an array.

**Syntax:**
```yaml
value: (( keys map ))
```

**Examples:**
```yaml
database:
  host: localhost
  port: 5432
  name: myapp

db_keys: (( keys database ))              # ["host", "name", "port"]

# Iterate over keys
(( for key in keys database ))
- (( grab key ))
(( done ))
```

## base64

Base64 encode a string.

**Syntax:**
```yaml
value: (( base64 string ))
```

**Examples:**
```yaml
secret: my-secret-value

encoded: (( base64 secret ))              # "bXktc2VjcmV0LXZhbHVl"

# Kubernetes secret
apiVersion: v1
kind: Secret
data:
  password: (( base64 (grab password) ))
```

## base64-decode

Decode a base64 string.

**Syntax:**
```yaml
value: (( base64-decode encoded_string ))
```

**Examples:**
```yaml
encoded: "SGVsbG8gV29ybGQ="

decoded: (( base64-decode encoded ))      # "Hello World"
```

## empty

Check if a value is empty, and optionally clear the parent key.

**Syntax:**
```yaml
value: (( empty value ))
```

An value is considered empty if it's:

- `null` or `~`
- Empty string `""`
- Empty array `[]`
- Empty map `{}`

**Examples:**
```yaml
optional: ""

# Check if empty
is_empty: (( empty optional ))            # true

# Conditionally include
(( if ! empty optional ))
setting: (( grab optional ))
(( fi ))
```

**Clear Parent:**

When `empty` is the only value for a key and evaluates to true, the entire key is removed from output:

```yaml
optional: ""

# This key will be removed if optional is empty
setting: (( empty optional ))
```

## type

Get the type of a value as a string.

**Syntax:**
```yaml
value: (( type value ))
```

**Returns:** `"string"`, `"int"`, `"float"`, `"bool"`, `"array"`, `"map"`, or `"null"`

**Examples:**
```yaml
str: "hello"
num: 42
flag: true
list: [1, 2, 3]
obj:
  key: value

types:
  str_type: (( type str ))                # "string"
  num_type: (( type num ))                # "int"
  flag_type: (( type flag ))              # "bool"
  list_type: (( type list ))              # "array"
  obj_type: (( type obj ))                # "map"
```

## null

Represent a null/nil value.

**Syntax:**
```yaml
value: (( null ))
```

**Examples:**
```yaml
# Explicitly set null
optional_setting: (( null ))

# Output:
# optional_setting: null
```

## param

Mark a required parameter that must be provided.

**Syntax:**
```yaml
value: (( param "error message" ))
```

If this operator is not overwritten during merge, Graft will exit with an error.

**Examples:**
```yaml
# base.yml
database:
  host: (( param "database.host is required" ))
  port: (( param "database.port is required" ))

# This merge will fail:
# graft merge base.yml  # Error: database.host is required

# This merge will succeed:
# graft merge base.yml overlay.yml  # where overlay provides values
```

## prune

Mark a key for removal from the final output.

**Syntax:**
```yaml
key: (( prune ))
```

**Examples:**
```yaml
# Internal values used during merge
_internal:
  version: 1
  defaults:
    timeout: 30

# Use internal values
server:
  timeout: (( grab _internal.defaults.timeout ))

# Remove internal values from output
_internal: (( prune ))
```

## inject

Inject the contents of a map at the current level.

**Syntax:**
```yaml
key: (( inject reference ))
```

**Examples:**
```yaml
common_labels:
  app: my-app
  version: "1.0"
  team: platform

# Inject labels into metadata
metadata:
  name: my-service
  labels:
    (( inject common_labels ))
    environment: production

# Result:
# metadata:
#   name: my-service
#   labels:
#     app: my-app
#     version: "1.0"
#     team: platform
#     environment: production
```

## defer

Defer evaluation of an operator (for template generation).

**Syntax:**
```yaml
value: (( defer operator args ))
```

**Examples:**
```yaml
# Generate a template with operators intact
template:
  database:
    password: (( defer vault "secret/db:password" ))

# Output:
# template:
#   database:
#     password: (( vault "secret/db:password" ))
```

## Combining Operators

Operators can be combined for powerful transformations:

```yaml
# Build dynamic paths
env: production
settings: (( grab (concat "environments." env ".config") ))

# Conditional with default
debug: (( grab config.debug || false ))

# Complex URL building
api_url: (( concat
    (grab config.use_ssl ? "https" : "http")
    "://"
    (grab config.host)
    ":"
    (grab config.port)
    (grab config.base_path || "/api")
))

# Transform and validate
hosts_csv: (( join "," (grab allowed_hosts || []) ))
```

## See Also

- [Operators Overview](index.md) - All operators
- [Control Flow](control-flow.md) - Conditionals and loops
- [Array Operations](array-operations.md) - Array manipulation
