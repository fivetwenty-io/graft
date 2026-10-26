package load

import (
	"math"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// jqNumber reads a number the way jq's parser does. Beyond JSON's grammar it
// takes a leading plus, leading zeros, a bare or trailing decimal point,
// nan, and inf or infinity in any case, each with an optional sign. A
// number in JSON's grammar keeps its text, and the other spellings keep the
// text of the number they stand for. It reports false for a token jq
// refuses.
func jqNumber(s string) (*value.Value, bool) {
	if isJSONNumber(s) {
		return numberLiteral(s), true
	}
	neg := strings.HasPrefix(s, "-")
	body := s
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		body = s[1:]
	}
	switch strings.ToLower(body) {
	case "nan":
		return value.NumDec(decimal.NaN()), true
	case "inf", "infinity":
		if neg {
			return value.Num(math.Inf(-1)), true
		}
		return value.Num(math.Inf(1)), true
	}
	text, ok := canonicalDecimal(body, neg)
	if !ok {
		return nil, false
	}
	return numberLiteral(text), true
}

// canonicalDecimal rewrites a decimal that follows jq's looser grammar into
// JSON's.
func canonicalDecimal(body string, neg bool) (string, bool) {
	i := skipDigits(body, 0)
	whole := body[:i]
	frac := ""
	if i < len(body) && body[i] == '.' {
		j := skipDigits(body, i+1)
		frac = body[i+1 : j]
		i = j
	}
	if whole == "" && frac == "" {
		return "", false
	}
	exp, end, ok := scanExponent(body, i)
	if !ok || end != len(body) {
		return "", false
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	text := whole
	if frac != "" {
		text += "." + frac
	}
	if neg {
		text = "-" + text
	}
	return text + exp, true
}
