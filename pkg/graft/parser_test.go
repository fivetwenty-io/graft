package graft

import (
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"
)

// stubStuckTokenizer implements tokenStream but never advances past its
// first token, simulating a scanner arm that regresses to the pre-fix
// livelock behavior. Parser.tokenize must detect this and return an error
// rather than loop forever.
type stubStuckTokenizer struct {
	calls int
}

func (s *stubStuckTokenizer) HasMore() bool {
	return true // always claims more input remains, like the pre-fix bug
}

func (s *stubStuckTokenizer) NextToken() *interfaces.Token {
	s.calls++
	return &interfaces.Token{
		Type:    interfaces.TokenInvalid,
		Literal: "stuck",
		Pos:     interfaces.Position{Offset: 0},
		End:     interfaces.Position{Offset: 0},
	}
}

func (s *stubStuckTokenizer) Position() int {
	return 0 // never advances
}

func TestTokenizeAbortsOnNonAdvancingTokenizer(t *testing.T) {
	p := &Parser{
		tokenizer: &stubStuckTokenizer{},
		input:     "((( stuck",
	}

	err := p.tokenize()
	if err == nil {
		t.Fatalf("expected tokenize to abort on a non-advancing tokenizer, got nil error")
	}
	if !strings.Contains(err.Error(), "tokenizer made no progress") {
		t.Fatalf("expected 'tokenizer made no progress' error, got: %v", err)
	}
}

func TestTokenizeSucceedsOnRealTokenizer(t *testing.T) {
	p := NewParser("1 + 2", EvalPhase)
	if err := p.tokenize(); err != nil {
		t.Fatalf("unexpected error tokenizing well-formed input: %v", err)
	}
	if len(p.tokens) == 0 {
		t.Fatalf("expected tokens to be produced")
	}
}

// mustParseInner parses "(( src ))" and returns the Expr that the resulting
// Opcall wraps: for an exprOperator wrapper (infix/unary nodes) that is
// Args()[0], and for an operator-call or bare-reference wrapper it is the
// same — Opcall.Args()[0] — by construction of exprToOpcall.
func mustParseInner(t *testing.T, src string) *Expr {
	t.Helper()
	opcall, err := ParseOpcallWithParser(EvalPhase, "(( "+src+" ))")
	if err != nil {
		t.Fatalf("ParseOpcallWithParser(%q) failed: %v", src, err)
	}
	if opcall == nil {
		t.Fatalf("ParseOpcallWithParser(%q) returned a nil Opcall", src)
	}
	args := opcall.Args()
	if len(args) == 0 {
		t.Fatalf("ParseOpcallWithParser(%q) produced an Opcall with no args", src)
	}
	return args[0]
}

// mustParseOpcall parses "(( src ))" and returns the resulting Opcall
// itself, for cases where the parse produces a genuine named operator call
// (registered or not) rather than the exprOperator/bare-reference wrapping
// mustParseInner assumes.
func mustParseOpcall(t *testing.T, src string) *Opcall {
	t.Helper()
	opcall, err := ParseOpcallWithParser(EvalPhase, "(( "+src+" ))")
	if err != nil {
		t.Fatalf("ParseOpcallWithParser(%q) failed: %v", src, err)
	}
	if opcall == nil {
		t.Fatalf("ParseOpcallWithParser(%q) returned a nil Opcall", src)
	}
	return opcall
}

// isNullOperatorCallNamed reports whether opcall is the NullOperator
// pass-through for the given unregistered operator name — i.e. the parse
// kept "anything else" position B-1/B-2 behavior (operator call, not
// reference).
func isNullOperatorCallNamed(opcall *Opcall, name string) bool {
	null, ok := opcall.Operator().(NullOperator)
	return ok && null.Missing == name
}

// TestParserPrecedenceTable pins the worked parses in spec §9.1. The
// precedence constants themselves are pre-existing (operator_registry.go);
// this only confirms parseExprWithPrecedence combines them as documented.
//
//nolint:gocyclo // one subtest per worked-parse row, each with its own shape checks; mirrors the spec table directly
func TestParserPrecedenceTable(t *testing.T) {
	t.Run("a + b * c parses as a + (b * c)", func(t *testing.T) {
		e := mustParseInner(t, "a + b * c")
		if e.Type != Addition {
			t.Fatalf("expected Addition at top, got %v", e.Type)
		}
		if e.Right.Type != Multiplication {
			t.Fatalf("expected Multiplication on the right, got %v", e.Right.Type)
		}
	})

	t.Run("a * b + c parses as (a * b) + c", func(t *testing.T) {
		e := mustParseInner(t, "a * b + c")
		if e.Type != Addition {
			t.Fatalf("expected Addition at top, got %v", e.Type)
		}
		if e.Left.Type != Multiplication {
			t.Fatalf("expected Multiplication on the left, got %v", e.Left.Type)
		}
	})

	t.Run("a - b - c is left-associative: (a - b) - c", func(t *testing.T) {
		e := mustParseInner(t, "a - b - c")
		if e.Type != Subtraction {
			t.Fatalf("expected Subtraction at top, got %v", e.Type)
		}
		if e.Left.Type != Subtraction {
			t.Fatalf("expected left-associative nesting on the left, got %v", e.Left.Type)
		}
		if e.Right.Type != Reference {
			t.Fatalf("expected bare reference 'c' on the right, got %v", e.Right.Type)
		}
	})

	t.Run("a == b < c parses as a == (b < c): comparison binds tighter", func(t *testing.T) {
		e := mustParseInner(t, "a == b < c")
		if e.Type != Equal {
			t.Fatalf("expected Equal at top, got %v", e.Type)
		}
		if e.Right.Type != LessThan {
			t.Fatalf("expected LessThan on the right, got %v", e.Right.Type)
		}
	})

	t.Run("a < b == c < d parses as (a < b) == (c < d)", func(t *testing.T) {
		e := mustParseInner(t, "a < b == c < d")
		if e.Type != Equal {
			t.Fatalf("expected Equal at top, got %v", e.Type)
		}
		if e.Left.Type != LessThan || e.Right.Type != LessThan {
			t.Fatalf("expected LessThan on both sides, got left=%v right=%v", e.Left.Type, e.Right.Type)
		}
	})

	t.Run("a && b || c parses as (a && b) || c", func(t *testing.T) {
		e := mustParseInner(t, "a && b || c")
		if e.Type != LogicalOr {
			t.Fatalf("expected LogicalOr at top, got %v", e.Type)
		}
		if e.Left.Type != LogicalAnd {
			t.Fatalf("expected LogicalAnd on the left, got %v", e.Left.Type)
		}
	})

	t.Run("a || b && c parses as a || (b && c)", func(t *testing.T) {
		e := mustParseInner(t, "a || b && c")
		if e.Type != LogicalOr {
			t.Fatalf("expected LogicalOr at top, got %v", e.Type)
		}
		if e.Right.Type != LogicalAnd {
			t.Fatalf("expected LogicalAnd on the right, got %v", e.Right.Type)
		}
	})

	t.Run("!a && b parses as (!a) && b", func(t *testing.T) {
		e := mustParseInner(t, "!a && b")
		if e.Type != LogicalAnd {
			t.Fatalf("expected LogicalAnd at top, got %v", e.Type)
		}
		if e.Left.Type != Negate {
			t.Fatalf("expected Negate on the left, got %v", e.Left.Type)
		}
	})

	t.Run("!a == b parses as (!a) == b", func(t *testing.T) {
		e := mustParseInner(t, "!a == b")
		if e.Type != Equal {
			t.Fatalf("expected Equal at top, got %v", e.Type)
		}
		if e.Left.Type != Negate {
			t.Fatalf("expected Negate on the left, got %v", e.Left.Type)
		}
	})

	t.Run("-a + b folds unary minus into a Subtraction with literal 0", func(t *testing.T) {
		e := mustParseInner(t, "-a + b")
		if e.Type != Addition {
			t.Fatalf("expected Addition at top, got %v", e.Type)
		}
		if e.Left.Type != Subtraction {
			t.Fatalf("expected Subtraction on the left (unary minus lowering), got %v", e.Left.Type)
		}
		if e.Left.Left == nil || e.Left.Left.Type != Literal {
			t.Fatalf("expected a literal 0 as the left operand of the lowered subtraction")
		}
	})
}

// TestA6BareReferenceOperand pins cluster A6 (spec §3): an unregistered bare
// identifier at the operator-call-opening position becomes a reference, but
// only when the next token places it in operand position.
//
//nolint:gocyclo // one subtest per lookahead case (B-1, B-2, H4, ternary, etc.), each with its own shape checks
func TestA6BareReferenceOperand(t *testing.T) {
	t.Run("bare identifier before == becomes a reference", func(t *testing.T) {
		e := mustParseInner(t, `env == "production"`)
		if e.Type != Equal {
			t.Fatalf("expected Equal at top, got %v", e.Type)
		}
		if e.Left.Type != Reference {
			t.Fatalf("expected env to parse as a Reference, got %v", e.Left.Type)
		}
		if e.Left.Reference == nil || e.Left.Reference.String() != "env" {
			t.Fatalf("expected reference path 'env', got %v", e.Left.Reference)
		}
	})

	t.Run("bare identifier before && becomes a reference", func(t *testing.T) {
		e := mustParseInner(t, `flag && other`)
		if e.Type != LogicalAnd {
			t.Fatalf("expected LogicalAnd at top, got %v", e.Type)
		}
		if e.Left.Type != Reference {
			t.Fatalf("expected flag to parse as a Reference, got %v", e.Left.Type)
		}
	})

	t.Run("bare identifier before ? becomes a reference (ternary condition)", func(t *testing.T) {
		// The ternary itself is always a genuine "?:" operator call (this is
		// pre-existing, unaffected by A6); what A6 changes is the shape of
		// its first argument, the condition.
		opcall := mustParseOpcall(t, `large ? "8Gi" : "2Gi"`)
		if !isNullOperatorCallNamed(opcall, "?:") {
			// "?:" is registered, so this should never be a NullOperator —
			// confirm instead that it is not accidentally the bare "large"
			// NullOperator pass-through, which would indicate the ternary
			// failed to parse as a ternary at all.
			if isNullOperatorCallNamed(opcall, "large") {
				t.Fatalf("expected a ternary operator call, got the bare 'large' pass-through")
			}
		}
		args := opcall.Args()
		if len(args) != 3 {
			t.Fatalf("expected 3 ternary args (condition, true, false), got %d", len(args))
		}
		if args[0].Type != Reference {
			t.Fatalf("expected ternary condition to parse as a Reference, got %v", args[0].Type)
		}
	})

	// B-1 (§9.2): the "anything else" arm must be preserved verbatim — a bare
	// identifier at primary position with no following infix operator is not
	// an operator call at all: ParseOpcallWithParser reports "not an opcall"
	// (nil, nil) so the raw `(( a ))` text survives a pass byte-for-byte,
	// because defer / multi-pass genesis templating and BOSH/CredHub
	// placeholder interpolation depend on unevaluated `(( a ))` surviving a
	// merge unchanged (BOSH's placeholder grammar allows no interior
	// whitespace, so even re-rendering with normalized spacing corrupts it).
	t.Run("bare identifier alone at primary position passes through as raw text", func(t *testing.T) {
		opcall, err := ParseOpcallWithParser(EvalPhase, "(( a ))")
		if err != nil {
			t.Fatalf("ParseOpcallWithParser(\"a\") failed: %v", err)
		}
		if opcall != nil {
			t.Fatalf("expected nil Opcall (raw pass-through) for a lone bare identifier, got %#v", opcall)
		}
	})

	t.Run("bare identifier followed by another identifier stays an operator call", func(t *testing.T) {
		// (( bogus foo )) — B-2: must still pass through as an operator call
		// with "foo" as its argument, not become a reference.
		opcall := mustParseOpcall(t, "bogus foo")
		if !isNullOperatorCallNamed(opcall, "bogus") {
			t.Fatalf("expected the NullOperator pass-through for 'bogus', got operator %#v", opcall.Operator())
		}
		args := opcall.Args()
		if len(args) != 1 || args[0].Type != Reference {
			t.Fatalf("expected a single Reference argument 'foo', got %+v", args)
		}
	})

	t.Run("bare identifier followed by ( stays an operator call", func(t *testing.T) {
		opcall := mustParseOpcall(t, "bogus(1)")
		if !isNullOperatorCallNamed(opcall, "bogus") {
			t.Fatalf("expected the NullOperator pass-through for 'bogus', got operator %#v", opcall.Operator())
		}
	})

	// H4: a directly-adjacent unary-looking minus is read as infix, matching
	// the intuitive reading; the pre-A6 form was a parse error.
	t.Run("a -1 is read as infix subtraction, not unary", func(t *testing.T) {
		e := mustParseInner(t, "a -1")
		if e.Type != Subtraction {
			t.Fatalf("expected Subtraction, got %v", e.Type)
		}
		if e.Left.Type != Reference {
			t.Fatalf("expected a to parse as a Reference, got %v", e.Left.Type)
		}
	})
}

// TestParseOpcallDoesNotHangOnBareEquals pins the fix for the tokenizer
// livelock: before the fix, an unescaped `=`, `&`, or
// `|` inside an operator call — including the dotted predicate form —
// spun the tokenizer forever, appending identical TokenInvalid tokens.
//
// Termination alone is asserted with a goroutine + timeout, so a regression
// fails the test instead of hanging the suite. Each case also asserts its
// outcome: without that, the test would pass even if the tokenizer started
// returning garbage promptly, and it would not distinguish the three inputs
// that must still be parse errors from the predicate form that A7 makes
// succeed.
func TestParseOpcallDoesNotHangOnBareEquals(t *testing.T) {
	cases := []struct {
		src string
		// wantErrSubstring is the parse error the input must produce, or ""
		// if the input must parse successfully.
		wantErrSubstring string
	}{
		// A lone '=', '&' or '|' is still not valid expression syntax: the
		// predicate support A7 adds applies only to a dotted path segment
		// (see the ArrayReferencePattern change), never to a bare operand.
		{`(( grab a=b ))`, "unexpected character: ="},
		{`(( grab a & b ))`, "unexpected character: &"},
		{`(( grab a | b ))`, "unexpected character: |"},
		// The dotted predicate form, by contrast, now parses cleanly.
		{`(( grab servers.name=primary.host ))`, ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.src, func(t *testing.T) {
			type result struct {
				opcall *Opcall
				err    error
			}
			done := make(chan result, 1)
			go func() {
				opcall, err := ParseOpcallWithParser(EvalPhase, tc.src)
				done <- result{opcall, err}
			}()

			var got result
			select {
			case got = <-done:
				// parsing terminated — the livelock is fixed
			case <-time.After(3 * time.Second):
				t.Fatalf("ParseOpcallWithParser(%q) did not return within 3s; tokenizer livelock regression", tc.src)
			}

			if tc.wantErrSubstring == "" {
				if got.err != nil {
					t.Fatalf("expected %q to parse, got error: %v", tc.src, got.err)
				}
				if got.opcall == nil {
					t.Fatalf("expected %q to produce an Opcall", tc.src)
				}
				return
			}

			if got.err == nil {
				t.Fatalf("expected %q to fail to parse, got Opcall %v", tc.src, got.opcall)
			}
			if !strings.Contains(got.err.Error(), tc.wantErrSubstring) {
				t.Fatalf("expected error containing %q for %q, got: %v",
					tc.wantErrSubstring, tc.src, got.err)
			}
		})
	}
}
