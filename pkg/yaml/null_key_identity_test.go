package yaml_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// decodeEveryRoute decodes doc into an any on the strict route, through
// a Node and its strict Decode, and through the callback route into an
// any and into a Node. It returns the four results, the two strict
// errors, and the reports each callback route saw.
type routeResults struct {
	strict, viaNode, cbAny, cbNode any
	strictErr, nodeErr             error
	anyReports, nodeReports        []yaml.DuplicateKey
}

func decodeEveryRoute(t *testing.T, doc string) routeResults {
	t.Helper()
	var r routeResults
	r.strictErr = yaml.Unmarshal([]byte(doc), &r.strict)
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &n); err != nil {
		t.Fatalf("Unmarshal into a Node: %v", err)
	}
	r.nodeErr = n.Decode(&r.viaNode)
	if err := yaml.UnmarshalWith([]byte(doc), &r.cbAny, collect(&r.anyReports)); err != nil {
		t.Fatalf("UnmarshalWith into an any: %v", err)
	}
	var cn yaml.Node
	if err := yaml.UnmarshalWith([]byte(doc), &cn, collect(&r.nodeReports)); err != nil {
		t.Fatalf("UnmarshalWith into a Node: %v", err)
	}
	if err := cn.Decode(&r.cbNode); err != nil {
		t.Fatalf("Decode of the collapsed Node: %v", err)
	}
	return r
}

// TestNullKeyBesideStringNullKey pins a null key and a string key with
// the text null in one mapping. spruce keeps both, which a map with
// string keys cannot hold, so the strict routes refuse the document,
// and the callback routes report the later key and keep its value.
func TestNullKeyBesideStringNullKey(t *testing.T) {
	cases := []struct {
		doc       string
		line, col int
		first     string
		want      any
	}{
		{"~: 1\n!!str null: 2\n", 2, 1, "[1:1]", 2},
		{"!!str null: 1\n~: 2\n", 2, 1, "[1:1]", 2},
		{"\"null\": a\n~: b\n", 2, 1, "[1:1]", "b"},
		{"&n ~: 1\n!!str null: 2\n", 2, 1, "[1:1]", 2},
		{"? ~\n: 1\n\"null\": 2\n", 3, 1, "[1:1]", 2},
		{"{~: 1, \"null\": 2}\n", 1, 8, "[1:2]", 2},
		{"!!null ~: 1\n!!str null: 2\n", 2, 1, "[1:1]", 2},
		{"~: 1\nnull: 2\n!!str null: 3\n", 3, 1, "[2:1]", 3},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			r := decodeEveryRoute(t, c.doc)
			// The Node route places an explicit key at the key, where the
			// text route places it at its "?", so only the line of the
			// first key is compared there.
			msg := "mapping key already defined at " + c.first
			for name, err := range map[string]error{"Unmarshal": r.strictErr, "Node.Decode": r.nodeErr} {
				var de *yaml.DecodeError
				if !errors.As(err, &de) || de.Line != c.line || de.Column != c.col ||
					!strings.HasPrefix(de.Msg, msg[:len(msg)-len(c.first)]+c.first[:2]) ||
					name == "Unmarshal" && de.Msg != msg {
					t.Errorf("%s: err = %v, want %q at %d:%d", name, err, msg, c.line, c.col)
				}
				if err != nil && strings.Contains(err.Error(), "null") {
					t.Errorf("%s: error quotes the key: %v", name, err)
				}
			}
			want := map[string]any{"null": c.want}
			if !reflect.DeepEqual(r.cbAny, want) || !reflect.DeepEqual(r.cbNode, want) {
				t.Errorf("callback routes gave %v and %v, want %v", r.cbAny, r.cbNode, want)
			}
			if len(r.anyReports) != 1 || len(r.nodeReports) != 1 || r.anyReports[0].Line != c.line || r.nodeReports[0].Line != c.line {
				t.Errorf("reports = %+v and %+v, want one at line %d each", r.anyReports, r.nodeReports, c.line)
			}
		})
	}
}

// TestNullKeysThatStayApart pins keys that spruce keeps apart and that
// read as distinct keys in graft too, so no route refuses or reports
// them: a null key beside a key whose text is ~ but which is a string,
// and two null keys spelled differently, which read as one key, as
// spruce reads them.
func TestNullKeysThatStayApart(t *testing.T) {
	cases := []struct {
		doc  string
		want any
	}{
		{"a:\n  ~: keep-me\n  !!str ~: 2\n", map[string]any{"a": map[string]any{"null": "keep-me", "~": 2}}},
		{"\"~\": 1\n~: 2\n", map[string]any{"null": 2, "~": 1}},
		{"!foo ~: 1\n~: 2\n", map[string]any{"null": 2, "~": 1}},
		{"null: 1\n~: 2\n", map[string]any{"null": 2}},
		{"~: 1\n!!str Null: 2\n", map[string]any{"null": 1, "Null": 2}},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			r := decodeEveryRoute(t, c.doc)
			if r.strictErr != nil || r.nodeErr != nil {
				t.Fatalf("strict routes refused: %v and %v", r.strictErr, r.nodeErr)
			}
			for name, got := range map[string]any{"Unmarshal": r.strict, "Node.Decode": r.viaNode, "callback any": r.cbAny, "callback Node": r.cbNode} {
				if !reflect.DeepEqual(got, c.want) {
					t.Errorf("%s = %v, want %v", name, got, c.want)
				}
			}
			if len(r.anyReports) != 0 || len(r.nodeReports) != 0 {
				t.Errorf("reports = %+v and %+v, want none", r.anyReports, r.nodeReports)
			}
		})
	}
}

// TestRepeatedKeysStillRefused pins the repeats the strict routes refused
// before keys were told apart by whether they read as null.
func TestRepeatedKeysStillRefused(t *testing.T) {
	for _, doc := range []string{
		"a: 1\na: 2\n",
		"~: 1\n~: 2\n",
		"\"a\": 1\na: 2\n",
		"!foo null: 1\n\"null\": 2\n",
		"o: {a: 1, a: 2}\n",
	} {
		var v any
		if err := yaml.Unmarshal([]byte(doc), &v); err == nil {
			t.Errorf("%q: Unmarshal accepted a repeated key", doc)
		}
	}
	var v any
	if err := yaml.Unmarshal([]byte("o: {a, a}\n"), &v); err != nil {
		t.Errorf("a flow mapping entry with no colon is no repeat: %v", err)
	}
}
