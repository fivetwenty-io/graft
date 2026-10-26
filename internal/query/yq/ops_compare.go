package yq

import (
	"fmt"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// CompareOp runs yq's comparison operators. Equality reads scalars as the
// text they print as, so 3 equals "3" but not 3.0, and the right side may
// hold the wildcards * and ?, so "cat" == "ca*" is true. Two maps or two
// sequences are never equal. The ordering operators compare two numbers
// by value and two strings by their text, a null is ordered against
// nothing but another null under <= and >=, and every other pair is an
// error, as yq 4.54.1 has it.
func (*Dialect) CompareOp(ctx *runtime.Ctx, op string, l, r *value.Value) (bool, error) {
	switch op {
	case "==":
		return yqEqual(l, r), nil
	case "!=":
		return !yqEqual(l, r), nil
	case "<", "<=", ">", ">=":
		return yqOrder(ctx, op, l, r)
	default:
	}
	return false, fmt.Errorf("unknown operator %s", op)
}

// yqEqual is yq's ==.
func yqEqual(l, r *value.Value) bool {
	if l.Kind() == value.KindNull {
		return r.Kind() == value.KindNull
	}
	if kindOf(l) != kindScalar || kindOf(r) != kindScalar {
		return false
	}
	return wildcardMatch(nodeText(l), nodeText(r))
}

// yqOrder is yq's <, <=, >, and >=.
func yqOrder(ctx *runtime.Ctx, op string, l, r *value.Value) (bool, error) {
	if err := orderable(l, r); err != nil {
		return false, err
	}
	orEqual := op == "<=" || op == ">="
	if l.Kind() == value.KindNull || r.Kind() == value.KindNull {
		return orEqual && l.Kind() == r.Kind(), nil
	}
	var c int
	switch {
	case l.Kind() == value.KindNumber && r.Kind() == value.KindNumber:
		c = ctx.Compare(l, r)
	case l.Kind() == value.KindString && r.Kind() == value.KindString:
		c = strings.Compare(l.Str(), r.Str())
	default:
		return false, fmt.Errorf("%s not yet supported for comparison", tagOf(l))
	}
	switch {
	case orEqual && c == 0:
		return true, nil
	case op == ">" || op == ">=":
		return c > 0, nil
	default:
	}
	return c < 0, nil
}

// orderable is the error yq gives when either side of an ordering
// operator is a map or a sequence.
func orderable(l, r *value.Value) error {
	switch kindOf(l) {
	case "map":
		return fmt.Errorf("maps not yet supported for comparison")
	case "seq":
		return fmt.Errorf("arrays not yet supported for comparison")
	default:
	}
	if kindOf(r) != kindScalar {
		return fmt.Errorf("%s cannot be subtracted from %s", tagOf(r), tagOf(l))
	}
	return nil
}

// wildcardMatch reports whether text matches pattern, where * stands for
// any run of characters and ? for any one. It backtracks only to the most
// recent *, which keeps it linear in the length of text.
func wildcardMatch(text, pattern string) bool {
	if pattern == "" {
		return text == ""
	}
	t, p := []rune(text), []rune(pattern)
	ti, pi := 0, 0
	star, mark := -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			ti++
			pi++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case star >= 0:
			mark++
			ti, pi = mark, star+1
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
