package histdiff

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

type m = map[string]interface{}
type l = []interface{}

func TestCompareCharacterization(t *testing.T) {
	when := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	podA := m{"apiVersion": "v1", "kind": "X", "metadata": m{"name": "a"}, "v": 1}
	podB := m{"apiVersion": "v1", "kind": "X", "metadata": m{"name": "b"}, "v": 1}
	bare := m{"apiVersion": "v1", "kind": "X", "metadata": m{"name": "a"}}
	for _, c := range []struct {
		name     string
		from, to interface{}
		want     []Change
		err      string
	}{
		{"float-int", m{"a": 1.0, "b": int64(2)}, m{"a": 1, "b": 2.0}, nil, ""},
		{"k8s-rename", podA, podB, []Change{
			{Path: "", Kind: Removed, Old: podA},
			{Path: "", Kind: Added, New: podB},
			{Path: "", Kind: Modified, Old: l{"v1/X/a"}, New: l{"v1/X/b"}},
		}, ""},
		{"nil-from-k8s", nil, bare, []Change{{Path: "", Kind: Added, New: bare}}, ""},
		{"nil-from", nil, m{"a": 1}, []Change{{Path: "", Kind: Modified, Old: nil, New: m{"a": 1}}}, ""},
		{"empty-from", m{}, m{"a": 1}, []Change{{Path: "a", Kind: Added, New: 1}}, ""},
		{"merge-key", m{"x": m{"<<": m{"a": 1}, "b": 2}}, m{"x": m{"a": 1, "b": 3}}, []Change{
			{Path: "x.<<", Kind: Removed, Old: m{"a": 1}},
			{Path: "x.a", Kind: Added, New: 1},
			{Path: "x.b", Kind: Modified, Old: 2, New: 3},
		}, ""},
		{"list-dupe", m{"l": l{"a", "a", "b"}}, m{"l": l{"a", "b", "b", "c"}}, []Change{
			{Path: "l", Kind: Modified, Old: l{"a", "a", "b"}, New: l{"a", "b", "b"}},
			{Path: "l[1]", Kind: Removed, Old: "a"},
			{Path: "l[2]", Kind: Added, New: "b"},
			{Path: "l[3]", Kind: Added, New: "c"},
		}, ""},
		{"list-order", m{"l": l{"a", "b", "c"}}, m{"l": l{"c", "b", "a"}}, []Change{
			{Path: "l", Kind: Modified, Old: l{"a", "b", "c"}, New: l{"c", "b", "a"}},
		}, ""},
		{"named-list", m{"jobs": l{m{"name": "x", "v": 1}, m{"name": "y"}}}, m{"jobs": l{m{"name": "y"}, m{"name": "x", "v": 2}, m{"name": "z"}}}, []Change{
			{Path: "jobs", Kind: Modified, Old: l{"x", "y"}, New: l{"y", "x"}},
			{Path: "jobs.x.v", Kind: Modified, Old: 1, New: 2},
			{Path: "jobs[2]", Kind: Added, New: m{"name": "z"}},
		}, ""},
		{"time", m{"t": when}, m{"t": "2001-01-01T00:00:00Z"}, []Change{
			{Path: "t", Kind: Modified, Old: when, New: "2001-01-01T00:00:00Z"},
		}, ""},
		{"str-num", m{"p": "8080"}, m{"p": 8080}, []Change{{Path: "p", Kind: Modified, Old: "8080", New: 8080}}, ""},
		{"chan", m{"c": make(chan int)}, m{}, nil, "histdiff: encoding from: marshaling value to YAML: cannot marshal type: chan int"},
		{"dotted-keys", m{"a.b": 1, "": 2}, m{"a.b": 2, "": 3}, []Change{
			{Path: "", Kind: Modified, Old: 2, New: 3},
			{Path: "a.b", Kind: Modified, Old: 1, New: 2},
		}, ""},
		{"nested-tag-eq", m{"l": l{m{"a": 1}, m{"b": 2}}}, m{"l": l{m{"a": "1"}, m{"b": 2}}}, nil, ""},
		{"root-scalar", "x", "y", []Change{{Path: "", Kind: Modified, Old: "x", New: "y"}}, ""},
		{"bytes", m{"b": []byte("hi")}, m{"b": []byte("ho")}, []Change{
			{Path: "b[1]", Kind: Removed, Old: 105},
			{Path: "b[1]", Kind: Added, New: 111},
		}, ""},
		{"seq-add-idx", m{"l": l{"a", "b"}}, m{"l": l{"x", "a", "b", "y"}}, []Change{
			{Path: "l[0]", Kind: Added, New: "x"},
			{Path: "l[3]", Kind: Added, New: "y"},
		}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Compare("from", c.from, "to", c.to)
			if c.err != "" {
				if err == nil || err.Error() != c.err {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Compare: %v", err)
			}
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %s\nwant %s", fmt.Sprintf("%#v", got), fmt.Sprintf("%#v", c.want))
			}
		})
	}
}
