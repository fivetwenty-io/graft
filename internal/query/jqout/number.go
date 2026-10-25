package jqout

import (
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// maxFloat is how jq spells an infinity, the largest finite double.
const maxFloat = "1.7976931348623157e+308"

// FormatNumber prints a number the way jq 1.8.2 does with exact decimals. A
// literal that has not changed prints in decNumber's canonical form, and a
// computed result prints by graft's plain-up-to-21-digits rule.
func FormatNumber(v *value.Value) string {
	return format(v, decimal.Decimal.String)
}

// FormatNumberFloat prints a number for --float-numbers. A literal prints as
// it does in exact mode, and a computed result prints by the placement rule
// of jq's jvp_dtoa_fmt over the shortest digits that round-trip the double.
func FormatNumberFloat(v *value.Value) string {
	return format(v, decimal.Decimal.FloatString)
}

func format(v *value.Value, result func(decimal.Decimal) string) string {
	if lit := v.Literal(); lit != "" {
		if !v.Bounded() {
			return lit
		}
		return value.CanonicalLiteral(lit)
	}
	d := v.Dec()
	switch {
	case d.IsNaN():
		return "null"
	case d.IsZero() && d.IsNegative():
		return "-0"
	}
	if inf, sign := d.IsInf(); inf {
		if sign < 0 {
			return "-" + maxFloat
		}
		return maxFloat
	}
	return result(d)
}
