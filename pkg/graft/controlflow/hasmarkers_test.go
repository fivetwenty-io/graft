package controlflow

import "testing"

// TestHasMarkers pins the exported marker predicate to the same
// classification Expand's own fast path uses: only documents that would
// actually trigger control-flow expansion report true.
func TestHasMarkers(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want bool
	}{
		{"plain document", "meta:\n  name: thing\n", false},
		{"operator call is not a marker", "name: (( grab meta.name ))\n", false},
		{"vault operator is not a marker", "secret: (( vault \"a/b:c\" ))\n", false},
		{"if marker", "(( if meta.enabled ))\nname: x\n(( fi ))\n", true},
		{"for marker", "(( for x in meta.list ))\n- (( grab x ))\n(( done ))\n", true},
		{"while marker", "(( while meta.go ))\nn: 1\n(( done ))\n", true},
		{"case marker", "(( case meta.env ))\n(( when \"dev\" ))\nn: 1\n(( esac ))\n", true},
		{"marker inside block scalar is not a marker", "script: |\n  (( if x ))\n  echo hi\n  (( fi ))\n", false},
		{"marker with trailing comment", "(( if meta.on )) # gate\nn: 1\n(( fi ))\n", true},
		{"empty document", "", false},
	}

	for _, tc := range cases {
		if got := HasMarkers([]byte(tc.doc)); got != tc.want {
			t.Errorf("%s: HasMarkers = %v, want %v", tc.name, got, tc.want)
		}
	}
}
