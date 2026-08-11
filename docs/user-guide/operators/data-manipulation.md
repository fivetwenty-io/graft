# Data Manipulation Operators

Operators for referencing and transforming data within your configuration.

Every output block below is what `graft merge` actually prints: the whole
merged document, map keys sorted alphabetically, and list items at the same
indentation as their parent key. Where an example would otherwise be buried in
the values it reads from, the `--prune` flag that removed them is shown with
the command.

An expression must stay on one YAML line — splitting `(( ... ))` across lines
is a YAML parse error, not a graft error.

## grab

Reference a value from elsewhere in the document.

**Syntax:**

```yaml
value: (( grab path.to.value ))
```

**Examples:**

```yaml
# server.yml
defaults:
  timeout: 30
  host: localhost

server:
  timeout: (( grab defaults.timeout ))
  host: (( grab defaults.host ))
```

**Output** (`graft merge server.yml`):

```yaml
defaults:
  host: localhost
  timeout: 30
server:
  host: localhost
  timeout: 30
```

**Array Access:**

A list entry can be reached by index, or by a predicate that matches a field
inside the entry. Both the dotted spelling (`servers.name=primary`) and the
bracketed spelling (`servers[name=secondary]`) work in expressions.

```yaml
# servers.yml
servers:
  - name: primary
    host: primary.example.com
  - name: secondary
    host: secondary.example.com

first_host: (( grab servers.0.host ))
primary_host: (( grab servers.name=primary.host ))
secondary_host: (( grab servers[name=secondary].host ))
```

**Output** (`graft merge servers.yml --prune servers`):

```yaml
first_host: primary.example.com
primary_host: primary.example.com
secondary_host: secondary.example.com
```

The two spellings are **not** interchangeable on the command line. `--cherry-pick`
and `--prune` accept the dotted spelling only:

```
$ graft merge servers.yml --cherry-pick 'servers.name=primary'
servers:
- host: primary.example.com
  name: primary

$ graft merge servers.yml --cherry-pick 'servers[name=primary]'
Merge failed: validation_error: key not found: servers[name=primary] (missing segment 'servers[name=primary]')
```

**With Default:**

```yaml
# Use default if path doesn't exist
host: (( grab config.host || "localhost" ))
```

## concat

Concatenate strings and values. `concat` requires at least two arguments.

**Syntax:**

```yaml
value: (( concat arg1 arg2 ... ))
```

**Examples:**

```yaml
# app.yml
name: my-app
env: production
host: api.example.com
port: 8080

full_name: (( concat name "-" env ))
url: (( concat "https://" (grab host) ":" (grab port) ))
```

**Output** (`graft merge app.yml`):

```yaml
env: production
full_name: my-app-production
host: api.example.com
name: my-app
port: 8080
url: https://api.example.com:8080
```

Long expressions still have to fit on one line. Breaking a `concat` across
several YAML lines fails before graft ever sees it:

```
db.yml: parse_error: failed to parse YAML: [14:1] non-map value is specified
> 14 | ))
       ^
```

When a line gets unwieldy, compute the pieces into their own keys and
concatenate those.

## join

Join array elements with a delimiter. The delimiter comes first and must be a
literal string.

**Syntax:**

```yaml
value: (( join delimiter array ))
```

**Examples:**

```yaml
# hosts.yml
hosts:
  - server1.example.com
  - server2.example.com
  - server3.example.com

path_parts:
  - /usr
  - local
  - bin

host_list: (( join ", " hosts ))
full_path: (( join "/" path_parts ))
```

**Output** (`graft merge hosts.yml --prune hosts --prune path_parts`):

```yaml
full_path: /usr/local/bin
host_list: server1.example.com, server2.example.com, server3.example.com
```

## split

Split a string into an array. Exactly two arguments: the delimiter, then the
string.

**Syntax:**

```yaml
value: (( split delimiter string ))
```

A delimiter that starts with `/` is treated as a PCRE pattern, with the leading
slash stripped; anything else is a literal separator. An empty delimiter splits
into individual characters.

**Examples:**

```yaml
# split.yml
csv_line: "apple,banana,cherry"
text: "one  two   three"

fruits: (( split "," csv_line ))
words: (( split "/\\s+" text ))
```

**Output** (`graft merge split.yml --prune csv_line --prune text`):

```yaml
fruits:
- apple
- banana
- cherry
words:
- one
- two
- three
```

Without the leading `/`, `"\\s+"` is a literal four-character separator and the
string comes back in one piece.

## stringify

Convert any value to its YAML string representation. Exactly one argument.

**Syntax:**

```yaml
value: (( stringify data ))
```

**Examples:**

```yaml
# stringify.yml
config:
  database:
    host: localhost
    port: 5432

config_string: (( stringify config ))
```

**Output** (`graft merge stringify.yml --prune config`):

```yaml
config_string: "database:\n  host: localhost\n  port: 5432\n"
```

This is the usual way to embed a config file inside a Kubernetes ConfigMap or a
BOSH property that expects text.

## keys

Extract keys from a map as an array. The keys come back sorted.

**Syntax:**

```yaml
value: (( keys map ))
```

**Examples:**

```yaml
# keys.yml
database:
  host: localhost
  port: 5432
  name: myapp

db_keys: (( keys database ))

each_key:
(( for key in keys database ))
- (( grab key ))
(( done ))
```

**Output** (`graft merge keys.yml --prune database`):

```yaml
db_keys:
- host
- name
- port
each_key:
- host
- name
- port
```

## base64

Base64 encode a string. Exactly one argument.

**Syntax:**

```yaml
value: (( base64 string ))
```

## base64-decode

Decode a base64 string. Exactly one argument.

**Syntax:**

```yaml
value: (( base64-decode encoded_string ))
```

**Examples:**

```yaml
# secret.yml
secret: my-secret-value
encoded: (( base64 secret ))
decoded: (( base64-decode encoded ))
```

**Output** (`graft merge secret.yml`):

```yaml
decoded: my-secret-value
encoded: bXktc2VjcmV0LXZhbHVl
secret: my-secret-value
```

## empty

`empty` does two different jobs depending on its argument.

Given a value, it reports whether that value is empty and returns a boolean. A
value is empty if it is `null`/`~`, `""`, `[]`, or `{}`.

Given the bare type name `map` or `list` — a name that does not resolve to
anything in the document — it constructs an empty `{}` or `[]`.

**Syntax:**

```yaml
value: (( empty value ))
```

**Examples:**

```yaml
# empty.yml
optional: ""
present: hello

optional_is_empty: (( empty optional ))
present_is_empty: (( empty present ))
blank_map: (( empty map ))
blank_list: (( empty list ))
```

**Output** (`graft merge empty.yml`):

```yaml
blank_list: []
blank_map: {}
optional: ""
optional_is_empty: true
present: hello
present_is_empty: false
```

`empty` never removes its own key — `optional_is_empty` is still there, holding
`true`. To drop a key, use `(( prune ))` or the `--prune` flag. To leave it out
of the document in the first place, guard it with control flow:

```yaml
(( if ! empty optional ))
setting: (( grab optional ))
(( fi ))
```

## type

Get the type of a value as a string. Exactly one argument.

**Syntax:**

```yaml
value: (( type value ))
```

**Returns** exactly one of `string`, `int`, `float`, `bool`, `array`, `map`, or
`null`.

**Examples:**

```yaml
# types.yml
str: "hello"
num: 42
ratio: 4.5
flag: true
list: [1, 2, 3]
obj:
  key: value
nothing: ~

types:
  str_type: (( type str ))
  num_type: (( type num ))
  ratio_type: (( type ratio ))
  flag_type: (( type flag ))
  list_type: (( type list ))
  obj_type: (( type obj ))
  nothing_type: (( type nothing ))
```

**Output** (`graft merge types.yml --prune str --prune num --prune ratio --prune flag --prune list --prune obj --prune nothing`):

```yaml
types:
  flag_type: bool
  list_type: array
  nothing_type: "null"
  num_type: int
  obj_type: map
  ratio_type: float
  str_type: string
```

`nothing_type` comes out quoted because its value is the *string* `null`, not a
YAML null.

Called with anything other than one argument, `type` reports
`type operator requires exactly one argument, got <n>`.

## null

Represent a null value. `null` takes no arguments.

**Syntax:**

```yaml
value: (( null ))
```

**Examples:**

```yaml
# null.yml
optional_setting: (( null ))
```

**Output** (`graft merge null.yml`):

```yaml
optional_setting: null
```

To test whether something is null, compare its type: `(( (type x) == "null" ))`.

## param

Mark a required parameter that must be provided.

**Syntax:**

```yaml
value: (( param "error message" ))
```

If the key is not overwritten by a later document, graft exits 2 and reports
every unfilled parameter at once.

**Examples:**

```yaml
# base.yml
database:
  host: (( param "database.host is required" ))
  port: (( param "database.port is required" ))
```

```
$ graft merge base.yml
2 error(s) detected:
 - $.database.host: database.host is required
 - $.database.port: database.port is required
```

```yaml
# overlay.yml
database:
  host: db.example.com
  port: 5432
```

**Output** (`graft merge base.yml overlay.yml`):

```yaml
database:
  host: db.example.com
  port: 5432
```

## prune

Mark a key for removal from the final output.

**Syntax:**

```yaml
key: (( prune ))
```

Scaffolding is usually structure, and a document cannot define the same
top-level key twice — `_internal:` as a map and `_internal: (( prune ))` in one
file is a duplicate-mapping-key parse error. Put the marker in a later
document, or use the `--prune` flag.

**Examples:**

```yaml
# base.yml
_internal:
  version: 1
  defaults:
    timeout: 30

server:
  timeout: (( grab _internal.defaults.timeout ))
```

```yaml
# cleanup.yml
_internal: (( prune ))
```

**Output** (`graft merge base.yml cleanup.yml`, and identically
`graft merge base.yml --prune _internal`):

```yaml
server:
  timeout: 30
```

Inside a single document, `(( prune ))` still works for a key whose own value
you want dropped, such as a scratch value computed for a sibling.

## inject

Deep-merge the contents of one or more maps into the map that contains it.

`inject` occupies a key of its own; graft removes that key once the merge is
done. `<<<` is the conventional name for it.

**Syntax:**

```yaml
key:
  <<<: (( inject reference ))
```

**Examples:**

```yaml
# labels.yml
common_labels:
  app: my-app
  version: "1.0"
  team: platform

metadata:
  name: my-service
  labels:
    <<<: (( inject common_labels ))
    environment: production
```

**Output** (`graft merge labels.yml --prune common_labels`):

```yaml
metadata:
  labels:
    app: my-app
    environment: production
    team: platform
    version: "1.0"
  name: my-service
```

Every argument must resolve to a map. A non-map errors with `inject operator
argument must resolve to a map`, and a nil one with `inject operator argument
cannot be nil`.

## defer

Emit an operator call as literal text instead of evaluating it, for generating
templates that another graft run will evaluate later.

**Syntax:**

```yaml
value: (( defer operator args ))
```

**Examples:**

```yaml
# template.yml
template:
  database:
    password: (( defer vault "secret/db:password" ))
```

**Output** (`graft merge template.yml`):

```yaml
template:
  database:
    password: (( vault "secret/db:password" ))
```

## Combining Operators

A parenthesized operator call can appear anywhere an argument can, so operators
compose:

```yaml
# combined.yml
env: production
environments:
  production:
    config:
      replicas: 5
config:
  use_ssl: true
  host: api.example.com
  port: 8080
allowed_hosts:
  - a.example.com
  - b.example.com

settings: (( grab (concat "environments." env ".config") ))
debug: (( grab config.debug || false ))
scheme: '(( config.use_ssl ? "https" : "http" ))'
api_url: (( concat scheme "://" config.host ":" config.port ))
hosts_csv: (( join "," allowed_hosts ))
```

**Output** (`graft merge combined.yml --prune environments --prune config --prune allowed_hosts --prune scheme`):

```yaml
api_url: https://api.example.com:8080
debug: false
env: production
hosts_csv: a.example.com,b.example.com
settings:
  replicas: 5
```

Two things in that example are deliberate:

- The ternary is quoted and lives in its own key. A parenthesized ternary in
  the **first** argument of a call — `(( concat (use_ssl ? "https" : "http")
  "://" ... ))` — fails with `expected '))' at end of operator expression, got
  STRING`. Giving it a name sidesteps that and reads better.

- The fallback for `hosts_csv` is a reference, not a literal `[]`. List
  literals are not part of the expression language; `(( join "," (grab
  allowed_hosts || []) ))` fails with `unexpected token: [`. Point `||` at a
  key holding an empty list instead.

## See Also

- [Operators Overview](index.md) - All operators

- [Control Flow](control-flow.md) - Conditionals and loops

- [Array Operations](array-operations.md) - Array manipulation

- [Operator reference](../../reference/operators.md) - Arity, types, and error text
