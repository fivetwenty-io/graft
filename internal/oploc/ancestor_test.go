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
		{"q.b", "q", "cycle detected: $.q.b grabs $.q, which contains it"},
		{"q.l.1", "q.l", "cycle detected: $.q.l.1 grabs $.q.l, which contains it"},
		{"jobs.api.self", "jobs.0", "cycle detected: $.jobs.api.self grabs $.jobs.0, which contains it"},
		{"jobs.0.self", "jobs.name=api", "cycle detected: $.jobs.0.self grabs $.jobs.name=api, which contains it"},
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
		{"its own map", "q.b", q, "$.q.b grabs $.q"},
		{"its own map in a literal", "q.b", literal.Wrap(q), "$.q.b grabs $.q"},
		{"its own list", "q.l.1", l, "$.q.l.1 grabs $.q.l"},
		{"the map above its list", "q.l.1", q, "$.q.l.1 grabs $.q"},
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
