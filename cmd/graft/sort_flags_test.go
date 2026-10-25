package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

var topKeyLine = regexp.MustCompile(`^["']?([^\s"':#-][^"':]*)["']?:`)

// topKeys lists the top-level keys of a block-style YAML document in the
// order they are written.
func topKeys(doc string) []string {
	var keys []string
	for _, line := range strings.Split(doc, "\n") {
		if m := topKeyLine.FindStringSubmatch(line); m != nil {
			keys = append(keys, m[1])
		}
	}
	return keys
}

// jsonTopKeys lists the top-level keys of a JSON object in the order they
// are written.
func jsonTopKeys(doc string) []string {
	dec := json.NewDecoder(strings.NewReader(doc))
	if _, err := dec.Token(); err != nil {
		return nil
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}

func TestSortFlags(t *testing.T) {
	Convey("merge and json take --sort-keys", t, func() {
		f := "testdata/sortkeys/mixed.yml"
		def, _, rc := runGraftCommand(t, []string{"merge", f})
		So(rc, ShouldEqual, 0)
		spruce, _, _ := runGraftCommand(t, []string{"merge", "--sort-keys", "spruce", f})
		So(spruce, ShouldEqual, def)
		lex, _, _ := runGraftCommand(t, []string{"merge", "--sort-keys", "lexical", f})
		So(topKeys(lex), ShouldResemble, []string{"10", "2", "a", "a10", "a2", "b"})
		So(topKeys(def), ShouldResemble, []string{"2", "10", "a", "a2", "a10", "b"})
		So(lex, ShouldNotEqual, def)

		jdef, _, _ := runGraftCommand(t, []string{"json", f})
		jlex, _, _ := runGraftCommand(t, []string{"json", "--sort-keys", "lexical", f})
		So(jlex, ShouldEqual, jdef)
		jspruce, _, _ := runGraftCommand(t, []string{"json", "--sort-keys", "spruce", f})
		So(jsonTopKeys(jspruce), ShouldResemble, []string{"2", "10", "a", "a2", "a10", "b"})

		_, e, rc := runGraftCommand(t, []string{"merge", "--sort-keys", "none", f})
		So(rc, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "none is not available on merge (lexical or spruce)")
		_, e, rc = runGraftCommand(t, []string{"json", "--sort-keys", "none", f})
		So(rc, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "none is not available on json (lexical or spruce)")
		_, e, rc = runGraftCommand(t, []string{"json", "--sort-keys", "alpha", f})
		So(rc, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "unknown sort mode \"alpha\"")
	})

	Convey("the spruce order reaches nested maps in json", t, func() {
		out, _, rc := runGraftCommand(t, []string{"json", "--sort-keys", "spruce", "testdata/sortkeys/mixed.yml"})
		So(rc, ShouldEqual, 0)
		So(out, ShouldContainSubstring, `"b":{"2":4,"10":3,"a":6,"a2":1,"a10":2,"b":5}`)
	})
}

func TestSortKeysCacheKey(t *testing.T) {
	inputs := [][]byte{[]byte("a: 1\n")}
	spruce := mergeOutputCacheKey(&mergeOpts{SortKeys: "spruce"}, inputs, false)
	lexical := mergeOutputCacheKey(&mergeOpts{SortKeys: "lexical"}, inputs, false)
	if spruce == lexical {
		t.Fatal("different sort modes must produce different cache keys")
	}
}

func TestSortKeysHonoredByDeferredPlacements(t *testing.T) {
	tree := map[string]interface{}{"2": 1, "10": 2, "a": 3}
	deferred := []graft.DeferredPath{{Path: "a", Reason: "test reason"}}
	lexical := func(a, b string) bool { return a < b }

	for _, placement := range []reportPlacement{reportPlacementBeginning, reportPlacementInline, reportPlacementEnd} {
		t.Run(string(placement), func(t *testing.T) {
			def, rc := renderMergedTreeWithReport(tree, deferred, placement, false, nil)
			if rc != 0 {
				t.Fatalf("default rc = %d", rc)
			}
			lex, rc := renderMergedTreeWithReport(tree, deferred, placement, false, lexical)
			if rc != 0 {
				t.Fatalf("lexical rc = %d", rc)
			}
			if got, want := topKeys(string(def)), []string{"2", "10", "a"}; strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("default keys = %v, want %v", got, want)
			}
			if got, want := topKeys(string(lex)), []string{"10", "2", "a"}; strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("lexical keys = %v, want %v", got, want)
			}
			if !strings.Contains(string(lex), "deferred") {
				t.Errorf("deferred report missing: %q", lex)
			}
		})
	}
}
