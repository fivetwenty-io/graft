package ast

import "strings"

// NormalizeNumber rewrites jq's looser number syntax as a JSON number. It
// drops a plus sign and leading zeros, puts a zero before a bare fraction,
// and drops a trailing point, and it reports false for anything else.
func NormalizeNumber(s string) (string, bool) {
	sign, rest := "", s
	if rest != "" && (rest[0] == '+' || rest[0] == '-') {
		if rest[0] == '-' {
			sign = "-"
		}
		rest = rest[1:]
	}
	exp := ""
	if i := strings.IndexAny(rest, "eE"); i >= 0 {
		digits := strings.TrimPrefix(strings.TrimPrefix(rest[i+1:], "+"), "-")
		if digits == "" || !allDigits(digits) || len(rest[i+1:])-len(digits) > 1 {
			return "", false
		}
		rest, exp = rest[:i], "e"+rest[i+1:]
	}
	whole, frac, _ := strings.Cut(rest, ".")
	if whole+frac == "" || !allDigits(whole) || !allDigits(frac) {
		return "", false
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	if frac != "" {
		whole += "." + frac
	}
	return sign + whole + exp, true
}

func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
