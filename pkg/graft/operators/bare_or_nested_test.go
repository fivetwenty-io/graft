package operators_test

import (
	"fmt"
	"strings"
	"testing"
)

// A nested call that fails under a bare || names the path it runs at
// once, not once for itself and again for the call it sits in.
func TestNestedErrorUnderABareOrNamesThePathOnce(t *testing.T) {
	cases := []struct{ name, src, cause string }{
		{
			name:  "grab of a missing path",
			src:   "a: (( nope || (( grab missing.x )) ))\n",
			cause: "unable to resolve",
		},
		{
			name:  "param",
			src:   "a: (( nope || (( param \"set me\" )) ))\n",
			cause: "parameter",
		},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				_, err := mergeWithin(t, parallel, tc.src)
				if err == nil {
					t.Fatal("err = nil, want an error")
				}
				msg := err.Error()
				if !strings.Contains(msg, tc.cause) {
					t.Fatalf("err = %q, want it to say %q", msg, tc.cause)
				}
				if n := strings.Count(msg, "$.a"); n != 1 {
					t.Fatalf("err = %q names $.a %d times, want once", msg, n)
				}
			})
		}
	}
}
