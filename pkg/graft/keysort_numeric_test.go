package graft

import (
	"math"
	"strconv"
	"testing"
)

// keyNumericValueReference is keyNumericValue without the first-byte
// guard, kept as the behavioral oracle.
func keyNumericValueReference(s string) (f float64, isInt bool, ok bool) {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return float64(n), true, true
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, false, false
	}
	return n, false, true
}

func TestKeyNumericValueMatchesReference(t *testing.T) {
	corpus := []string{
		"", "0", "1", "-1", "+1", "10", "01", "1.5", "-.5", "+.5", ".5",
		"1e3", "-1e-3", "1E3", "9223372036854775807", "-9223372036854775808",
		"99999999999999999999", "1_000", "0x10", "0o17", "0b101",
		"inf", "Inf", "+Inf", "-Inf", "infinity", "Infinity", "nan", "NaN",
		"name", "z1", "int_val", "int64_val", "meta", "-flag", ".hidden",
		"10.244.0.34", "v1", "properties", "jobs",
	}
	for _, s := range corpus {
		gf, gi, gok := keyNumericValue(s)
		wf, wi, wok := keyNumericValueReference(s)
		if gf != wf || gi != wi || gok != wok {
			t.Errorf("keyNumericValue(%q) = (%v, %v, %v); reference says (%v, %v, %v)",
				s, gf, gi, gok, wf, wi, wok)
		}
	}
}
