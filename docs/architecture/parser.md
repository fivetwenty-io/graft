# Parser Design

Graft's parser uses a hybrid strategy that preserves YAML compatibility while providing full control over operator expression parsing.

## Design Rationale

### Why Not Fork yaml.v3?

- yaml.v3 is battle-tested (~15k lines of code)

- YAML spec is complex; a custom parser risks compliance bugs

- The real complexity is in operator parsing, not YAML parsing

- A hybrid approach gives accurate positions without fork risk

### Hybrid Strategy

1. **Pre-Scanner**

   Extract `(( ... ))` operator locations with precise positions

2. **yaml.v3 with Node API**

   Parse YAML while preserving line/column information

3. **Unified Operator Parser**

   Parse operator expressions with position mapping

4. **Position Mapping**

   Combine YAML positions with operator positions for accurate errors

## Pre-Scanner

### Purpose

Scan input for `(( ... ))` operator expressions and control flow constructs before YAML parsing.

### Interface

```go
type OperatorLocation struct {
    StartLine   int
    StartColumn int
    EndLine     int
    EndColumn   int
    RawText     string           // Full "(( ... ))" text
    InnerText   string           // Just the "..." part
    Type        OperatorType     // Standard, ControlFlow
}

func PreScanOperators(source []byte) ([]OperatorLocation, error)
```

### Algorithm

```go
func PreScanOperators(source []byte) ([]OperatorLocation, error) {
    var locations []OperatorLocation
    line, col := 1, 1
    i := 0

    for i < len(source)-1 {
        // Track position
        if source[i] == '\n' {
            line++
            col = 1
            i++
            continue
        }

        // Look for "(("
        if source[i] == '(' && source[i+1] == '(' {
            start := Position{Line: line, Column: col}

            // Find matching "))"
            end, inner, err := findOperatorEnd(source, i+2)
            if err != nil {
                return nil, err
            }

            loc := OperatorLocation{
                StartLine:   start.Line,
                StartColumn: start.Column,
                EndLine:     end.Line,
                EndColumn:   end.Column,
                RawText:     string(source[i:end.Offset+2]),
                InnerText:   inner,
                Type:        classifyOperator(inner),
            }
            locations = append(locations, loc)

            i = end.Offset + 2
            continue
        }

        col++
        i++
    }

    return locations, nil
}
```

### Control Flow Classification

```go
func classifyOperator(inner string) OperatorType {
    trimmed := strings.TrimSpace(inner)
    words := strings.Fields(trimmed)

    if len(words) == 0 {
        return OperatorTypeStandard
    }

    switch words[0] {
    case "if":
        return OperatorTypeIf
    case "elif":
        return OperatorTypeElseIf
    case "else":
        return OperatorTypeElse
    case "fi":
        return OperatorTypeEndIf
    case "for":
        return OperatorTypeFor
    case "while":
        return OperatorTypeWhile
    case "done":
        return OperatorTypeEndLoop
    case "case":
        return OperatorTypeCase
    case "when":
        return OperatorTypeWhen
    case "default":
        return OperatorTypeDefault
    case "esac":
        return OperatorTypeEndCase
    default:
        return OperatorTypeStandard
    }
}
```

### Handled Cases

- Multi-line operators

- Nested parentheses within operators

- Operators in any YAML context (values, keys, array items)

- Escaped/quoted strings (not treated as operators)

- Control flow block detection (if/fi, for/done, case/esac)

## Tokenizer

The tokenizer performs lexical analysis on operator expressions, producing a stream of tokens with position information.

### Token Types

```go
type TokenType int

const (
    TokenTypeEOF TokenType = iota
    TokenTypeError

    // Literals
    TokenTypeInteger
    TokenTypeFloat
    TokenTypeString
    TokenTypeBoolean
    TokenTypeNull

    // Identifiers
    TokenTypeIdentifier
    TokenTypeReference
    TokenTypeEnvironment

    // Operators
    TokenTypeOperatorStart  // ((
    TokenTypeOperatorEnd    // ))
    TokenTypePlus           // +
    TokenTypeMinus          // -
    TokenTypeStar           // *
    TokenTypeSlash          // /
    TokenTypePercent        // %
    TokenTypeEqual          // ==
    TokenTypeNotEqual       // !=
    TokenTypeLess           // <
    TokenTypeGreater        // >
    TokenTypeLessEqual      // <=
    TokenTypeGreaterEqual   // >=
    TokenTypeAnd            // &&
    TokenTypeOr             // ||
    TokenTypeNot            // !
    TokenTypeQuestion       // ?
    TokenTypeColon          // :

    // Delimiters
    TokenTypeLeftParen      // (
    TokenTypeRightParen     // )
    TokenTypeLeftBracket    // [
    TokenTypeRightBracket   // ]
    TokenTypeLeftBrace      // {
    TokenTypeRightBrace     // }
    TokenTypeComma          // ,
    TokenTypeDot            // .
    TokenTypeAt             // @
    TokenTypePipe           // |

    // Control Flow Keywords
    TokenTypeIf
    TokenTypeElif
    TokenTypeElse
    TokenTypeFi
    TokenTypeFor
    TokenTypeWhile
    TokenTypeDone
    TokenTypeCase
    TokenTypeWhen
    TokenTypeDefault
    TokenTypeEsac
    TokenTypeIn
    TokenTypeRange
)
```

### Token Structure

```go
type Token struct {
    Type     TokenType
    Value    string
    Position Position
}

type Position struct {
    Line   int
    Column int
    Offset int
}
```

### Tokenizer Interface

```go
type Tokenizer interface {
    // Reset initializes with new input
    Reset(input string)

    // Next returns the next token
    Next() Token

    // Peek returns the next token without consuming
    Peek() Token

    // Position returns current position
    Position() Position
}
```

### Keyword Recognition

Keywords are recognized only in operator context:

```go
var keywords = map[string]TokenType{
    "if":      TokenTypeIf,
    "elif":    TokenTypeElif,
    "else":    TokenTypeElse,
    "fi":      TokenTypeFi,
    "for":     TokenTypeFor,
    "while":   TokenTypeWhile,
    "done":    TokenTypeDone,
    "case":    TokenTypeCase,
    "when":    TokenTypeWhen,
    "default": TokenTypeDefault,
    "esac":    TokenTypeEsac,
    "in":      TokenTypeIn,
    "range":   TokenTypeRange,
    "true":    TokenTypeBoolean,
    "false":   TokenTypeBoolean,
    "nil":     TokenTypeNull,
    "null":    TokenTypeNull,
}
```

## Grammar Specification

### EBNF Grammar

```ebnf
(* Document structure *)
document       = ( statement | control_flow )* ;

(* Control flow constructs *)
control_flow   = if_block | for_block | while_block | case_block ;

if_block       = "((" "if" expression "))" statement*
                 ( "((" "elif" expression "))" statement* )*
                 ( "((" "else" "))" statement* )?
                 "((" "fi" "))" ;

for_block      = "((" "for" IDENTIFIER ( "," IDENTIFIER )? "in" expression "))"
                 statement*
                 "((" "done" "))" ;

while_block    = "((" "while" expression "))"
                 statement*
                 "((" "done" "))" ;

case_block     = "((" "case" expression "))"
                 ( when_clause )*
                 ( "((" "default" "))" statement* )?
                 "((" "esac" "))" ;

when_clause    = "((" "when" pattern ( "|" pattern )* "))" statement* ;

pattern        = STRING | NUMBER | BOOLEAN | IDENTIFIER ;

(* Standard operator expressions *)
statement      = "((" expression "))" | yaml_content ;

expression     = ternary ;
ternary        = logical_or ( "?" expression ":" expression )? ;
logical_or     = logical_and ( "||" logical_and )* ;
logical_and    = equality ( "&&" equality )* ;
equality       = comparison ( ( "==" | "!=" ) comparison )* ;
comparison     = additive ( ( "<" | ">" | "<=" | ">=" ) additive )* ;
additive       = multiplicative ( ( "+" | "-" ) multiplicative )* ;
multiplicative = unary ( ( "*" | "/" | "%" ) unary )* ;
unary          = ( "!" | "-" ) unary | call ;
call           = primary ( "(" arguments? ")" )* ;
primary        = literal | reference | "(" expression ")" | operator_call ;
operator_call  = OPERATOR arguments? ;
arguments      = expression ( expression )* ;
literal        = STRING | NUMBER | BOOLEAN | NULL ;
reference      = IDENTIFIER ( "." IDENTIFIER | "[" index "]" )* ;
index          = NUMBER | STRING "=" value ;

(* Built-in functions *)
range          = "range" NUMBER NUMBER ( NUMBER )? ;
```

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
| 9 (highest) | `()` function call | Left |

## Unified Parser

### Structure

```go
type UnifiedParser struct {
    tokenizer     Tokenizer
    registry      OperatorRegistry
    options       ParserOptions

    // State
    currentToken  Token
    peekToken     Token
    hasNext       bool
    errors        []error
    originalInput string
}

type ParserOptions struct {
    Phase           OperatorPhase
    StrictMode      bool
    MaxNestingDepth int
}
```

### Core Methods

```go
// ParseExpression parses a complete expression
func (p *UnifiedParser) ParseExpression(input string) (Expression, error)

// ParseOperatorCall parses "(( operator args ))"
func (p *UnifiedParser) ParseOperatorCall(input string) (Expression, error)

// ParseReference parses "foo.bar[0].baz"
func (p *UnifiedParser) ParseReference(input string) (Expression, error)
```

### Recursive Descent Implementation

```go
func (p *UnifiedParser) parseExpr() (Expression, error) {
    return p.parseTernary()
}

func (p *UnifiedParser) parseTernary() (Expression, error) {
    condition, err := p.parseLogicalOr()
    if err != nil {
        return nil, err
    }

    if p.currentToken.Type == TokenTypeQuestion {
        p.advance()

        trueExpr, err := p.parseExpr()
        if err != nil {
            return nil, err
        }

        if p.currentToken.Type != TokenTypeColon {
            return nil, p.expectedError("':'")
        }
        p.advance()

        falseExpr, err := p.parseExpr()
        if err != nil {
            return nil, err
        }

        return &TernaryOp{
            Condition: condition,
            TrueExpr:  trueExpr,
            FalseExpr: falseExpr,
        }, nil
    }

    return condition, nil
}

func (p *UnifiedParser) parseLogicalOr() (Expression, error) {
    left, err := p.parseLogicalAnd()
    if err != nil {
        return nil, err
    }

    for p.currentToken.Type == TokenTypeOr {
        p.advance()

        right, err := p.parseLogicalAnd()
        if err != nil {
            return nil, err
        }

        left = &LogicalOr{Left: left, Right: right}
    }

    return left, nil
}

// Similar implementations for other precedence levels...
```

## AST Node Types

### Expression Nodes

```go
// Expression is the base interface for all AST nodes
type Expression interface {
    Accept(v Visitor) interface{}
    Position() Range
    String() string
}

// OperatorCall represents a named operator with arguments
type OperatorCall struct {
    Operator   string
    Arguments  []Expression
    Target     string       // Optional target (e.g., "prod@" in vault)
    Modifiers  []string     // Optional modifiers
    Pos        Range
}

// Reference represents a path to a document value
type Reference struct {
    Path       []PathElement
    Pos        Range
}

type PathElement struct {
    Type  PathElementType // Field or Index
    Field string          // For field access
    Index interface{}     // For index access (int or map key lookup)
}

// Literal represents a constant value
type Literal struct {
    Value interface{} // string, int64, float64, bool, nil
    Kind  LiteralKind
    Pos   Range
}

// BinaryOp represents a binary operation
type BinaryOp struct {
    Op    string
    Left  Expression
    Right Expression
    Pos   Range
}

// UnaryOp represents a unary operation
type UnaryOp struct {
    Op      string
    Operand Expression
    Pos     Range
}

// TernaryOp represents a conditional expression
type TernaryOp struct {
    Condition Expression
    TrueExpr  Expression
    FalseExpr Expression
    Pos       Range
}

// LogicalOr represents || with fallback semantics
type LogicalOr struct {
    Left  Expression
    Right Expression
    Pos   Range
}
```

### Control Flow Nodes

```go
// IfStatement represents if/elif/else/fi
type IfStatement struct {
    Conditions []Expression  // Condition for if and each elif
    Bodies     [][]Statement // Statements for each branch
    ElseBody   []Statement   // Statements for else (optional)
    Pos        Range
}

// ForStatement represents for/done
type ForStatement struct {
    Variable   string       // Loop variable
    KeyVar     string       // Optional key variable
    Collection Expression   // Collection to iterate
    Body       []Statement
    Pos        Range
}

// WhileStatement represents while/done
type WhileStatement struct {
    Condition Expression
    Body      []Statement
    MaxIter   int          // Safety limit
    Pos       Range
}

// CaseStatement represents case/when/default/esac
type CaseStatement struct {
    Value   Expression
    When    []WhenClause
    Default []Statement
    Pos     Range
}

// WhenClause represents a single when in case
type WhenClause struct {
    Patterns []Expression // Multiple via |
    Body     []Statement
    Pos      Range
}

// RangeExpr represents range start end [step]
type RangeExpr struct {
    Start Expression
    End   Expression
    Step  Expression // Optional
    Pos   Range
}
```

### Visitor Pattern

```go
type Visitor interface {
    VisitOperatorCall(node *OperatorCall) interface{}
    VisitReference(node *Reference) interface{}
    VisitLiteral(node *Literal) interface{}
    VisitBinaryOp(node *BinaryOp) interface{}
    VisitUnaryOp(node *UnaryOp) interface{}
    VisitTernaryOp(node *TernaryOp) interface{}
    VisitLogicalOr(node *LogicalOr) interface{}
    VisitEnvironment(node *Environment) interface{}
    VisitYAMLLiteral(node *YAMLLiteral) interface{}
    VisitParenthesized(node *Parenthesized) interface{}

    // Control flow
    VisitIfStatement(node *IfStatement) interface{}
    VisitForStatement(node *ForStatement) interface{}
    VisitWhileStatement(node *WhileStatement) interface{}
    VisitCaseStatement(node *CaseStatement) interface{}
    VisitWhenClause(node *WhenClause) interface{}
    VisitRangeExpr(node *RangeExpr) interface{}
}
```

## Position Tracking

### Range Type

```go
type Range struct {
    Start Position
    End   Position
}

type Position struct {
    Line   int
    Column int
    Offset int // Byte offset in source
}
```

### Position Mapper

```go
type PositionMapper struct {
    operatorLocs map[string]OperatorLocation
    yamlNodes    map[*yaml.Node]Position
    sourceFile   string
    source       string
}

func (pm *PositionMapper) MapError(node *yaml.Node, offset int) Position {
    base := Position{Line: node.Line, Column: node.Column}

    // Adjust for offset within operator expression
    content := node.Value[:offset]
    for _, ch := range content {
        if ch == '\n' {
            base.Line++
            base.Column = 1
        } else {
            base.Column++
        }
    }

    return base
}
```

## Error Reporting

### Error Types

```go
type ParseError struct {
    Message  string
    Position Position
    Source   string
    Length   int
    Hint     string
}

func (e *ParseError) Error() string {
    return e.Format()
}

func (e *ParseError) Format() string {
    lines := strings.Split(e.Source, "\n")

    var result strings.Builder
    result.WriteString(fmt.Sprintf("Error at line %d, column %d:\n",
        e.Position.Line, e.Position.Column))

    // Show context line
    if e.Position.Line > 0 && e.Position.Line <= len(lines) {
        line := lines[e.Position.Line-1]
        result.WriteString(fmt.Sprintf("  %s\n", line))

        // Show caret
        result.WriteString(strings.Repeat(" ", e.Position.Column+1))
        result.WriteString(strings.Repeat("^", max(1, e.Length)))
        result.WriteString("\n")
    }

    result.WriteString(e.Message)

    if e.Hint != "" {
        result.WriteString(fmt.Sprintf("\n\nHint: %s", e.Hint))
    }

    return result.String()
}
```

### Example Error Output

```
Error at line 15, column 34:
  password: (( vault "secret/db:pass" || ))
                                       ^^
Expected: expression after '||' operator
Found: '))'

Hint: The '||' operator requires a default value.
Example: (( vault "path:key" || "default" ))
```

## Nested Expression Examples

All of these must work consistently:

```yaml
# Simple nesting
url: (( concat "https://" (grab host) ":" (grab port) ))

# Deep nesting
config: (( grab (concat "environments." (grab env) ".settings") ))

# Operators in operator arguments
password: (( vault (concat "secret/" (grab env) ":password") ))

# Arithmetic with references
total: (( (grab base) + (grab tax) * (grab quantity) ))

# Ternary with nested expressions
value: (( (grab enabled) ? (grab primary) : (grab fallback) ))

# Boolean expressions
allowed: (( (grab role) == "admin" || (grab permissions.write) ))

# Complex real-world example
db_url: (( concat
    "postgres://"
    (grab db.user) ":"
    (vault (concat "secret/" (grab env) "/db:password"))
    "@" (grab db.host) ":" (grab db.port)
    "/" (grab db.name)
    (grab db.ssl ? "?sslmode=require" : "")
))
```

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
