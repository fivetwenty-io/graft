package operators_test

import "testing"

// TestStringifyEnvValueAboveInt64 checks that stringify of an env value
// too big for an int64 comes out as the bare number, as it does in
// spruce, and not as a literal block holding the number.
func TestStringifyEnvValueAboveInt64(t *testing.T) {
	t.Setenv("P5_ENV", "9223372036854775808")
	got := mergeEnvShape(t, "v: (( stringify $P5_ENV ))\n")
	if want := "v: 9223372036854775808"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
