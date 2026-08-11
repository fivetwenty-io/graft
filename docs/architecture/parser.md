# Parser Design

Graft has two parsers, and keeping them apart explains most of the design.
The **control-flow preprocessor** rewrites whole lines of source text before
any YAML is parsed. The **expression parser** parses the contents of a single
`(( ... ))` scalar, and runs later, when the evaluator reaches that value.
Neither one parses YAML itself; that is delegated to a library.

## Design Rationale

### Why Not Fork the YAML Library?

- goccy/go-yaml is battle-tested and maintained

- The YAML spec is complex; a custom parser risks compliance bugs

- The real complexity is in operator parsing, not YAML parsing

- Delegating YAML keeps graft's own surface small

### Division of Labor

1. **Control-flow expansion**

   Rewrite `(( if ))`, `(( for ))`, `(( while ))`, and `(( case ))` blocks
   into plain YAML, per file, on the raw bytes

2. **YAML parsing**

   Hand the expanded bytes to goccy/go-yaml, which reports position
   information on failure

3. **Expression parsing**

   Parse each `(( ... ))` scalar with the unified recursive-descent parser
   when the evaluator reaches it

## Control-Flow Preprocessor

Control flow cannot be represented as a parsed expression. A marker occupies
a whole line rather than a value position, its body is raw YAML rather than
an expression, and two branches of the same `if` may legally define the same
key — none of which survives a YAML parse. So `pkg/graft/controlflow`
rewrites the source text instead, and everything downstream sees only the
expanded result.

The package registers itself with `pkg/graft` through a package-level hook
(`ControlFlowExpander`, in `pkg/graft/controlflow_hook.go`), so `pkg/graft`
never imports it. A consumer opts in with a blank import, exactly as it does
for `pkg/graft/operators`. With no consumer, the hook stays nil and parsing
behaves as it did before control flow existed.

### Line Classification

The scanner splits the source into lines and marks each one. A line is a
**marker** when its trimmed content is exactly one balanced `(( ... ))`
group, optionally followed by a YAML comment, and its first word is a
control-flow keyword. Every other line is body text, copied out verbatim.

Three details fall out of that rule:

- **Quoting and nesting are honored within the line.** Parentheses inside a
  quoted string do not close the group, and backslash escapes are respected.

- **Markers are single-line.** The classifier only ever sees one line's
  text, so a marker cannot span a line break.

- **Block scalars are skipped.** A line that opens a `|` or `>` block scalar
  puts the scanner into a pass-through state until the first non-blank line
  at or below the opening line's indentation, so a `(( if ))`-shaped string
  inside a script or another templating language is body text, not a marker.

Keyword aliases are resolved at this point: `elsif` to `elif`, `endif` to
`fi`, `endfor` and `endwhile` to `done`, `endcase` to `esac`.

### Block Parsing

A recursive-descent pass over the classified lines builds a tree of items,
where an item is either a run of verbatim body lines or a nested block. It
enforces the structural rules directly, and each diagnostic names the rule it
broke rather than failing generically:

- An unclosed block reports `unclosed block: expected (( elif )) or
  (( else )) or (( fi )), reached end of input`

- A stray closer reports `(( done )) at line 2 has no matching block start`

- `elif` after `else`, a duplicate `else`, and `when` after `default` each
  get their own message

- Nesting is bounded at 64 levels: `control flow block nesting too deep
  (max 64)`

Because expansion happens before parsing, all of these carry graft's
`parse_error` prefix and exit 2:

```
cf2.yml: parse_error: control flow expansion failed: control flow: (( done )) at line 2 has no matching block start
```

A failure while *evaluating* a condition or iterable, rather than while
parsing the block structure, gets a synthetic path naming the construct and
its source line instead:

```
loop.yml: parse_error: control flow expansion failed: $.controlflow.for.L2: unable to resolve `svcs`: `$.svcs` could not be found in the datastructure
```

See [Control Flow](../user-guide/operators/control-flow.md) for the
user-facing rules, including loop-variable binding and the `while` iteration
cap.

## Pre-Scanner

`pkg/graft/interfaces` also provides a standalone pre-scanner that extracts
every `(( ... ))` location and its raw content from a source string:

```go
type OperatorLocation struct {
    Start      Position // Start position of the opening ((
    End        Position // End position of the closing ))
    RawContent string   // The content between (( and )), excluding delimiters
}

func PreScan(source string) ([]OperatorLocation, error)
func PreScanWithFile(source, filename string) ([]OperatorLocation, error)
```

It handles nested parentheses, quoted strings containing `((` or `))`,
escaped quotes, and expressions spanning several lines, and it does **not**
validate what it finds — it reports locations and raw text only. Unbalanced
delimiters produce a `PreScanError` carrying the position.

This is a utility for tooling that needs to find expressions without
evaluating them. The merge pipeline does not use it: control-flow markers are
found by the preprocessor's own line classifier, and ordinary expressions are
parsed on demand by the evaluator.

## Tokenizer

The tokenizer performs lexical analysis on operator expressions, producing a stream of tokens with position information.

### Token Types

```go
type TokenType int

const (
    TokenEOF TokenType = iota
    TokenInvalid

    // Delimiters of the expression itself
    TokenOperatorStart  // ((
    TokenOperatorEnd    // ))

    // Literals
    TokenInteger        // 42, -17, 0xFF
    TokenFloat          // 3.14, -2.5, 1e10
    TokenString         // "hello"
    TokenRawString      // 'world'
    TokenBoolean        // true, false
    TokenNull           // null, nil, ~

    // Identifiers
    TokenIdentifier     // foo, bar_baz
    TokenReference      // meta.name, foo.bar[0]
    TokenEnvironment    // $ENV_VAR

    // Operators
    TokenPlus           // +
    TokenMinus          // -
    TokenStar           // *
    TokenSlash          // /
    TokenPercent        // %
    TokenEqual          // ==
    TokenNotEqual       // !=
    TokenLess           // <
    TokenGreater        // >
    TokenLessEqual      // <=
    TokenGreaterEqual   // >=
    TokenAnd            // &&
    TokenOr             // ||
    TokenNot            // !
    TokenQuestion       // ?
    TokenColon          // :

    // Delimiters
    TokenLeftParen      // (
    TokenRightParen     // )
    TokenLeftBracket    // [
    TokenRightBracket   // ]
    TokenLeftBrace      // {
    TokenRightBrace     // }
    TokenComma          // ,
    TokenDot            // .
    TokenAt             // @
    TokenPipe           // |

    // Keywords
    TokenIf
    TokenElif
    TokenElse
    TokenFi
    TokenFor
    TokenIn
    TokenDone
    TokenWhile
    TokenCase
    TokenWhen
    TokenDefault
    TokenEsac
    TokenOn      // merge on <key>
    TokenBefore  // insert before
    TokenAfter   // insert after
    TokenRange

    TokenOperatorName
)
```

The control-flow keyword tokens exist because the tokenizer is shared, but
the expression parser never builds a control-flow construct from them: those
blocks are already gone by the time any expression is parsed. An `(( if ))`
line reaching the expression parser means it was not recognized as a marker —
most often because it sits inside a block scalar, or because
`pkg/graft/controlflow` was not imported.

### Token Structure

```go
type Token struct {
    Type    TokenType   // The type of the token
    Value   interface{} // Parsed value for literals (int64, float64, string, bool, nil)
    Literal string      // The raw text as it appears in source
    Pos     Position    // Start position in source
    End     Position    // End position in source
    Error   string      // Error message for TokenInvalid
}

type Position struct {
    Line   int    // 1-based
    Column int    // 1-based
    Offset int    // 0-based byte offset
    File   string // Source file name (optional)
}
```

### Tokenizer Interface

`AdvancedTokenizer` is the implementation the parser drives:

```go
tok := interfaces.NewAdvancedTokenizer(input, interfaces.TokenizerOptions{
    RecognizeReferencePaths: true,  // parse "meta.name" as one token
    AllowEnvironmentVars:    true,  // parse "$VAR"
    TrackPositions:          true,
    AllowUnicode:            true,
})

for tok.HasMore() {
    t := tok.NextToken()
    // ...
}
```

`NextToken`, `PeekToken`, `HasMore`, `Position`, and `Reset` are the surface
the parser uses. `Position` returns a byte offset, and the parser asserts it
strictly increases between tokens: a scanner arm that produces a token
without consuming input would otherwise loop forever, which is exactly what
happened for a lone `=`, `&`, `|`, or `$` before that was fixed.

## Grammar Specification

### EBNF Grammar

The grammar below covers expressions only. Control-flow blocks are not part
of it — they are line-oriented, and they are gone before an expression is
ever parsed.

```ebnf
(* An operator expression occupies one YAML scalar *)
opcall         = "((" expression "))" ;

expression     = ternary ;
ternary        = logical_or ( "?" expression ":" expression )? ;
logical_or     = logical_and ( "||" logical_and )* ;
logical_and    = equality ( "&&" equality )* ;
equality       = comparison ( ( "==" | "!=" ) comparison )* ;
comparison     = additive ( ( "<" | ">" | "<=" | ">=" ) additive )* ;
additive       = multiplicative ( ( "+" | "-" ) multiplicative )* ;
multiplicative = unary ( ( "*" | "/" | "%" ) unary )* ;
unary          = ( "!" | "-" ) unary | primary ;
primary        = literal | reference | "(" expression ")" | operator_call ;
operator_call  = OPERATOR [ "@" IDENTIFIER ] arguments? ;
arguments      = expression ( expression )* ;
literal        = STRING | NUMBER | BOOLEAN | NULL ;
reference      = IDENTIFIER ( "." segment | "[" segment "]" )* ;
segment        = IDENTIFIER | NUMBER | IDENTIFIER "=" value ;
```

Arguments are separated by whitespace, not commas. `@` after an operator name
names a backend target: `(( vault@production "secret/db:password" ))`. A
`field=value` segment is a predicate, selecting the entry of a list whose
named field holds that value; the dotted and bracketed spellings are
equivalent.

### Operand Position

`primary` is where an identifier's meaning is decided, and the rule is a
two-token lookahead. An identifier opens an operator call only when it names
a registered operator **and** the token after it can begin an argument — that
is, it is not `)`, `))`, `,`, `:`, `.`, end of input, or an infix operator.

Both halves are load-bearing:

- Without the first, `(( environment == "production" ))` would fail. With it,
  an identifier naming no registered operator resolves as a reference, so
  `grab` is optional inside an expression.

- Without the second, a document key that happens to share an operator's name
  would break. `type`, `sort`, `keys`, and `empty` are all ordinary manifest
  keys, and `(( left == type ))` has to keep resolving `type` as a reference.

One case is deliberately preserved from before infix expressions existed: a
lone identifier, `(( a ))`, still parses as a call to an unregistered
operator and passes through as literal text. Multi-pass templating depends on
that.

### Nested Calls and Grouping

A parenthesized group gets its own operand position, so an operator call may
appear anywhere an argument may, at any depth:

```yaml
image:  (( base64 (file "logo.png") ))
config: (( file (concat "configs/" env ".conf") ))
secret: (( vault (concat "secret/" env "/db:password") ))
nested: (( grab (concat "environments." (grab env) ".settings") ))
```

Paren nesting is bounded at 64 levels; deeper reports `expression nesting too
deep (max 64)`.

One spacing rule applies throughout. `))` is a single token — the end of the
expression — so two closing parentheses may never sit next to each other
inside one. When a nested call ends where an enclosing group also ends, put a
space between them:

```yaml
# Parse error: the ")) " after db.user ends the expression early
bad:  (( base64 (concat "prefix-" (grab db.user)) ))

# Fine
good: (( base64 (concat "prefix-" (grab db.user) ) ))
```

Three shapes are not accepted:

- **A group wrapping the whole expression.** `(( (join "," (grab a)) ))`
  fails with `expected ')' to close parenthesized expression`. Write
  `(( join "," (grab a) ))`.

- **A parenthesized infix or ternary group as an operator's first argument.**
  `(( concat (a + b) "://" ))` fails with `expected '))' at end of operator
  expression, got STRING`. A parenthesized operator *call* first is fine —
  `(( concat (grab a) "://" ))` — and moving the group to a later argument is
  fine too: `'(( concat "://" (p ? "x" : "y") ))'`.

- **Two closing parentheses with nothing between them**, as above.

### Operator Precedence

| Precedence | Operators | Associativity |
|------------|-----------|---------------|
| 1 (lowest) | `?:` | Right |
| 2 | `\|\|` | Left |
| 3 | `&&` | Left |
| 4 | `==` `!=` | Left |
| 5 | `<` `>` `<=` `>=` | Left |
| 6 | `+` `-` | Left |
| 7 | `*` `/` `%` | Left |
| 8 | `!` `-` (unary) | Right |
| 9 | operator call | Left |
| 10 (highest) | literals, references, `( ... )` | Left |

## Unified Parser

### Structure

```go
type Parser struct {
    tokenizer tokenStream
    tokens    []*interfaces.Token
    pos       int
    input     string
    phase     OperatorPhase

    // Token index at which a bare identifier may open an operator call
    opcallPos int

    // Currently-open "(" / "((" groups
    nestingDepth int
}

const maxNestingDepth = 64
```

`opcallPos` is the mechanism behind operand position. `ParseOpcall` sets it
to 1 — the first token after `((` — and every parenthesized group saves it,
sets it to its own first token, and restores it on the way out. That is what
gives each group its own call-opening slot, however deeply nested.

### Core Methods

```go
// NewParser builds a parser over one expression's text
func NewParser(input string, phase OperatorPhase) *Parser

// ParseOpcall parses a complete "(( operator args ))"
func (p *Parser) ParseOpcall() (*Opcall, error)

// ParseOpcallWithParser is the package-level entry the evaluator calls
func ParseOpcallWithParser(phase OperatorPhase, src string) (*Opcall, error)
```

Parsing is driven by precedence climbing over the levels in the table above,
with the ternary handled as a right-associative special case that produces a
call to the registered `?:` operator.

## AST Node Types

The parser produces one node type, `Expr`, discriminated by `Type`:

```go
type Expr struct {
    Type      ExprType
    Operator  string
    Name      string
    Target    string // "production" in "vault@production"
    Left      *Expr
    Right     *Expr
    Literal   interface{}
    Reference *tree.Cursor

    // Which nodes of Reference.Nodes were written in bracket notation
    BracketedNodes []bool

    Call *Opcall
    Pos  Position
}
```

`ExprType` covers `Literal`, `Reference`, `List`, `Or`, `Negate`, the
arithmetic and comparison forms (`Addition` through `GreaterThanOrEqual`),
`LogicalAnd`, `LogicalOr`, `RegexpMatch`, `EnvVar`, `BoshVar`,
`OperatorCall`, and the two vault sub-expression forms `VaultGroup` and
`VaultChoice`.

An operator call is an `Opcall`, which carries the resolved operator, its
argument expressions, the name as written, and the `@target` if one was
given:

```go
type Opcall struct {
    src       string
    where     *tree.Cursor
    canonical *tree.Cursor
    op        Operator
    args      []*Expr
    name      string // as written, e.g. "vault"
    target    string // "prod" in "vault@prod"; "" if none
}
```

There are no control-flow node types, and no visitor interface. Control flow
never reaches the expression AST, and `Opcall.Run` dispatches directly on the
`Operator` interface — `Setup`, `Run`, `Dependencies`, `Phase` — rather than
through a traversal.

An operator that accepts a target implements `TargetAware`. `Opcall.Run`
consults it before running any call that carried one, so an operator that
cannot honor a target rejects it with a clear error instead of silently
reading from the default backend.

## Position Tracking

```go
type Position struct {
    Line   int    // 1-based
    Column int    // 1-based
    Offset int    // 0-based byte offset
    File   string // optional
}

type Range struct {
    Start Position
    End   Position
}
```

`PositionMapper` converts byte offsets back into line/column positions. It
precomputes the offset of every line start and binary-searches that table, so
a lookup is logarithmic in the number of lines rather than a rescan:

```go
pm := interfaces.NewPositionMapper(source, "config.yml")
pos := pm.PositionAt(offset)   // Position{Line, Column, Offset, File}
```

## Error Reporting

Expression errors are reported against the path being evaluated, and graft
aggregates them so one run reports every failure rather than stopping at the
first:

```
2 error(s) detected:
 - $.database.port: too few arguments supplied to (( split ... ))
 - $.database.url: concat operator requires at least two arguments
```

Failures from control-flow expansion happen earlier, before YAML parsing, so
they carry the `parse_error` prefix, the file name, and a synthetic path:

```
loop.yml: parse_error: control flow expansion failed: $.controlflow.for.L2: unable to resolve `svcs`: `$.svcs` could not be found in the datastructure
```

YAML errors keep the library's own line and column detail, which is usually
what a human needs to find an unquoted expression:

```
yerr.yml: parse_error: failed to parse YAML: [4:38] could not find end character of double-quoted text
   1 | application:
   2 |   name: my-app
   3 | computed:
>  4 |   full_name: (( concat "Application: " application.name ))
                                            ^
```

## Nested Expression Examples

All of these work:

```yaml
# Simple nesting
url: (( concat "https://" (grab host) ":" (grab port) ))

# Deep nesting
config: (( grab (concat "environments." (grab env) ".settings") ))

# Operators in operator arguments
password: (( vault (concat "secret/" (grab env) "/db:password") ))

# Arithmetic with references
total: (( (grab base) + (grab tax) * (grab quantity) ))

# Bare references, no grab needed
total: (( base + tax * quantity ))

# Ternary, quoted because the expression contains ": "
value: '(( (grab enabled) ? (grab primary) : (grab fallback) ))'

# Boolean expressions
allowed: (( (grab role) == "admin" || (grab permissions.write) ))

# A predicate instead of an index
primary_host: (( grab servers.name=primary.host ))

# Everything at once, on one line
db_url: '(( concat "postgres://" (grab db.user) ":" (grab pw) "@" (grab db.host) ":" (grab db.port) "/" (grab db.name) (db.ssl ? "?sslmode=require" : "") ))'
```

Two YAML rules constrain how these are written, and neither is graft's doing.
An expression containing `: ` — any ternary, and any string literal with a
colon followed by a space — has to be quoted, or YAML reads it as a mapping
key. And an expression cannot be split across lines: a plain scalar does not
span lines, so the multi-line layout that reads well in a design document is
a parse error in a real file.

## Testing Requirements

- **Fuzz testing**

  Random valid/invalid inputs to find edge cases

- **Property-based testing**

  Grammar rules hold for generated inputs

- **Regression tests**

  All known edge cases from spruce compatibility

- **Round-trip tests**

  Parse -> AST -> source produces equivalent expressions

- **Performance tests**

  Deeply nested expressions complete within time bounds

- **Error message tests**

  All error conditions produce helpful messages
