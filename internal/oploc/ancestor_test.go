package oploc

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/literal"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

func ancestorTree() (root, q map[string]interface{}, l []interface{}) {
	l = []interface{}{1, "(( op ))"}
	q = map[string]interface{}{"a": 1, "b": "(( op ))", "l": l}
	root = map[string]interface{}{
		"q":    q,
		"qa":   map[string]interface{}{"x": "(( op ))"},
		"jobs": []interface{}{map[string]interface{}{"name": "api", "self": "(( op ))"}},
	}
	return root, q, l
}

func TestRefuseAncestor(t *testing.T) {
	root, _, _ := ancestorTree()
	cases := []struct {
		here, target string
		want         string
	}{
		{"q.b", "q", "cycle detected: $.q.b reads $.q, which contains it"},
		{"q.l.1", "q.l", "cycle detected: $.q.l.1 reads $.q.l, which contains it"},
		{"jobs.api.self", "jobs.0", "cycle detected: $.jobs.api.self reads $.jobs.0, which contains it"},
		{"jobs.0.self", "jobs.name=api", "cycle detected: $.jobs.0.self reads $.jobs.name=api, which contains it"},
		{"qa.x", "q", ""},
		{"q.b", "q.a", ""},
		{"q.b", "q.b", ""},
		{"q.b", "nope", ""},
	}
	for _, tc := range cases {
		c := cursors(t, tc.here, tc.target)
		here, target := c[0], c[1]
		err := RefuseAncestor(here, target, root)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s reading %s: unexpected error %v", tc.here, tc.target, err)
		case tc.want != "" && (err == nil || err.Error() != tc.want):
			t.Errorf("%s reading %s: err = %v, want %q", tc.here, tc.target, err, tc.want)
		}
	}
}

func TestRefuseSelfHolding(t *testing.T) {
	root, q, l := ancestorTree()
	at := func(p string) *tree.Cursor { return cursors(t, p)[0] }
	cases := []struct {
		name string
		here string
		val  interface{}
		want string
	}{
		{"its own map", "q.b", q, "$.q.b reads $.q"},
		{"its own map in a literal", "q.b", literal.Wrap(q), "$.q.b reads $.q"},
		{"its own list", "q.l.1", l, "$.q.l.1 reads $.q.l"},
		{"the map above its list", "q.l.1", q, "$.q.l.1 reads $.q"},
		{"another map", "qa.x", q, ""},
		{"a copy", "q.b", map[string]interface{}{"a": 1}, ""},
		{"an empty list", "q.l.1", []interface{}{}, ""},
		{"a scalar", "q.b", "q", ""},
	}
	for _, tc := range cases {
		err := RefuseSelfHolding(at(tc.here), tc.val, root)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: unexpected error %v", tc.name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: err = %v, want it to say %q", tc.name, err, tc.want)
		}
	}
}

// A list that shares its backing array with an ancestor list holds that
// ancestor's entry exactly when the entry lies inside its own span, so
// a reslice is told apart from the list it was cut from.
func TestRefuseSelfHoldingReslices(t *testing.T) {
	arr := []interface{}{"a", "b", "(( op ))", "d"}
	root := map[string]interface{}{"l": arr}
	cases := []struct {
		name string
		here string
		val  interface{}
		want string
	}{
		{"the whole list", "l.2", arr, "$.l.2 reads $.l"},
		{"a prefix that stops short", "l.2", arr[:2], ""},
		{"a prefix that holds the entry", "l.2", arr[:3], "$.l.2 reads $.l"},
		{"a suffix that holds the entry", "l.2", arr[1:], "$.l.2 reads $.l"},
		{"a suffix that starts after it", "l.2", arr[3:], ""},
		{"a suffix under the entry", "l.2.x", arr[1:], "$.l.2.x reads $.l"},
		{"a copy", "l.2", append([]interface{}(nil), arr...), ""},
	}
	for _, tc := range cases {
		err := RefuseSelfHolding(cursors(t, tc.here)[0], tc.val, root)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: unexpected error %v", tc.name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: err = %v, want it to say %q", tc.name, err, tc.want)
		}
	}
}
