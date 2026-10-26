package jq_test

import "testing"

func TestLeadingDotNumberLiterals(t *testing.T) {
	wantOutputs(t, `.1 + .2`, `null`, `0.3`)
	wantOutputsFloat(t, `.1 + .2`, `null`, `0.30000000000000004`)
	wantOutputs(t, `.5e3 | ., . + 0`, `null`, `5E+2`, `500`)
	wantOutputs(t, `-.25 | ., . + 1`, `null`, `-0.25`, `0.75`)
	wantOutputs(t, `.1 * 10`, `null`, `1`)
	wantOutputs(t, `[.1] | .[0] + 1`, `null`, `1.1`)
	wantOutputs(t, `.1, 1., (1. + 1)`, `null`, `0.1`, `1`, `2`)
}

func TestLeadingDotStillLexesAsIndexing(t *testing.T) {
	wantOutputs(t, `.foo`, `{"foo":3}`, `3`)
	wantOutputs(t, `.[1]`, `[1,2]`, `2`)
	wantError(t, `.1.foo`, `null`, `Cannot index number with string ("foo")`)
}
