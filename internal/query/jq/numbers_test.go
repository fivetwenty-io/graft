package jq_test

import (
	"strings"
	"testing"
)

func TestLiteralNumbersEndToEnd(t *testing.T) {
	big := `100000000000000000000000000000`
	wantOutputs(t, `., tojson, tostring, @text, (. + 0 | tojson)`, big, big, `"`+big+`"`, `"`+big+`"`, `"`+big+`"`, `"1e+29"`)
	wantOutputs(t, `1.000000000000000000001, 3.0e2, -0, [1.0] | tojson`, `null`, `"1.000000000000000000001"`, `"3.0E+2"`, `"0"`, `"[1.0]"`)
	wantOutputs(t, `tonumber`, `"100000000000000000000"`, `100000000000000000000`)
	wantOutputs(t, `[.[] | tonumber]`, `["1", "1.50", "1e2"]`, `[1,1.50,1E+2]`)
	wantOutputs(t, `. == 100000000000000000001`, `100000000000000000000`, `false`)
	wantOutputs(t, `. + 0 == 100000000000000000001, . + 1 == 100000000000000000001`, `100000000000000000000`, `false`, `true`)
	wantOutputsFloat(t, `. + 0 == 100000000000000000001, (. + 1 | tojson)`, `100000000000000000000`, `true`, `"1e+20"`)
	wantOutputs(t, `[., -., . * 1] | tojson`, `-0`, `"[-0,0,-0]"`)
	wantOutputs(t, `map(-.)`, `[100000000000000000000000000001, 1.0, 1e1000, 0]`, `[-100000000000000000000000000001,-1.0,-1E+1000,0]`)
	huge := strings.Repeat("9", 100001)
	wantOutputs(t, `tostring, tojson`, huge, `"`+huge+`"`, `"`+huge+`"`)
	wantOutputs(t, `tonumber | tojson | length`, `"`+huge+`"`, `100001`) // jq 1.8.2 keeps the text and raises only in arithmetic
	wantError(t, `tonumber | . + 0`, `"`+huge+`"`, `number exceeds 100000 significant digits`)
}
