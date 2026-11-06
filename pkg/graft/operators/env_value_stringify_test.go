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

// TestStringifyEnvValueStaysString pins what the known gap
// stringify-of-an-env-value-gives-a-string records. stringify of an env
// value that decodes to a scalar gives the string, where spruce gives
// the decoded scalar itself.
func TestStringifyEnvValueStaysString(t *testing.T) {
	for value, want := range map[string]string{
		"8080": `v: "8080"`,
		"yes":  `v: "true"`,
		"3.5":  `v: "3.5"`,
	} {
		t.Setenv("P5_ENV", value)
		if got := mergeEnvShape(t, "v: (( stringify $P5_ENV ))\n"); got != want {
			t.Errorf("P5_ENV=%q gave %q, want %q", value, got, want)
		}
	}
}
