package operators_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
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

// A nested call under a bare || runs at the path of the call it sits
// in, so calc finds its sibling there and an operator that reads that
// path, as static_ips does, has one to read.
func TestNestedCallUnderABareOrRunsAtItsPath(t *testing.T) {
	t.Setenv("GRAFT_TEST_NESTED_UNSET", "")
	subnet := "networks:\n- name: net1\n  subnets:\n" +
		"  - range: 10.0.0.0/24\n    static: [10.0.0.10 - 10.0.0.20]\n"
	// want is the value as fmt prints it, since calc can give an int
	// or a float.
	cases := []struct {
		name, src string
		at        []string
		want      string
	}{
		{
			name: "calc reads its sibling after a missing reference",
			src: "b: 100\nm:\n  b: 2\n" +
				"  a: (( nope || (( calc \"b * 2\" )) ))\n",
			at: []string{"m", "a"}, want: "4",
		},
		{
			name: "calc reads its sibling after an unset variable",
			src: "b: 100\nm:\n  b: 2\n" +
				"  a: (( $GRAFT_TEST_NESTED_UNSET || (( calc \"b * 2\" )) ))\n",
			at: []string{"m", "a"}, want: "4",
		},
		{
			name: "static_ips",
			src: subnet + "instance_groups:\n- name: web\n  instances: 1\n" +
				"  networks:\n  - name: net1\n" +
				"    static_ips: (( nope || (( static_ips 0 )) ))\n",
			at:   []string{"instance_groups", "0", "networks", "0", "static_ips"},
			want: "[10.0.0.10]",
		},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				got, err := mergeWithin(t, parallel, tc.src)
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if v := valueAt(t, got, tc.at); fmt.Sprint(v) != tc.want {
					t.Fatalf("%v = %#v, want %s", tc.at, v, tc.want)
				}
			})
		}
	}
}

// valueAt walks m along the map keys and list indexes in at.
func valueAt(t *testing.T, m map[string]interface{}, at []string) interface{} {
	t.Helper()
	var v interface{} = m
	for _, k := range at {
		switch c := v.(type) {
		case map[string]interface{}:
			v = c[k]
		case []interface{}:
			var i int
			if _, err := fmt.Sscan(k, &i); err != nil || i >= len(c) {
				t.Fatalf("no entry %s in %#v", k, c)
			}
			v = c[i]
		default:
			t.Fatalf("no key %s in %#v", k, v)
		}
	}
	return v
}

// With vault skipped, a vault call nested under a bare || is deferred
// at the path of the call it sits in.
func TestSkippedVaultUnderABareOrIsDeferred(t *testing.T) {
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			engine, err := graft.NewEngine(
				graft.WithParallel(parallel), graft.WithSkipVault(true))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := engine.ParseYAML([]byte(
				"a: (( nope || (( vault \"secret/x:y\" )) ))\nb: 1\n"))
			if err != nil {
				t.Fatal(err)
			}
			out, err := engine.Merge(context.Background(), doc).Execute()
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			got := out.RawData().(map[string]interface{})
			if a, _ := got["a"].(string); !strings.Contains(a, "(( vault") {
				t.Fatalf("a = %#v, want the deferred vault call", got["a"])
			}
			deferred := false
			for _, d := range engine.GetOperatorState().GetDeferredPaths() {
				if strings.TrimPrefix(d.Path, "$.") == "a" {
					deferred = true
				}
			}
			if !deferred {
				t.Fatal("a is not among the deferred paths")
			}
		})
	}
}
