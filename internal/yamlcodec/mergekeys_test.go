package yamlcodec_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/yamlcodec"
)

func decodeMerge(t *testing.T, doc string) (any, error) {
	t.Helper()
	return yamlcodec.DecodeGeneric([]byte(doc), yamlcodec.DefaultYAMLCompat(), false)
}

// TestMergeKeysFollowSpruce pins what spruce 1.35.17 gives for each
// document's m. Its YAML library reads a mapping's pairs in turn, each
// writing over the ones before it, and a merge key writes the pairs of
// the maps it names, with the first map of a list winning.
func TestMergeKeysFollowSpruce(t *testing.T) {
	cases := []struct {
		name, doc string
		want      map[string]any
	}{
		{"first map of a list wins",
			"b: &b {k1: 1, k2: 2}\no: &o {k2: 9, k3: 9}\nm:\n  <<: [*b, *o]\n",
			map[string]any{"k1": 1, "k2": 2, "k3": 9}},
		{"merge wins over a key before it",
			"m:\n  k2: 5\n  <<: [{k2: 1}, {k2: 2}]\n",
			map[string]any{"k2": 1}},
		{"a key after the merge wins",
			"m:\n  <<: {a: 1, b: 1}\n  a: 2\n",
			map[string]any{"a": 2, "b": 1}},
		{"second merge key wins over the first",
			"m:\n  <<: {a: 1, b: 1}\n  <<: {a: 2}\n  c: 9\n",
			map[string]any{"a": 2, "b": 1, "c": 9}},
		{"a key after both merge keys wins",
			"m:\n  a: 0\n  <<: {a: 1}\n  <<: {a: 2, b: 2}\n  b: 3\n",
			map[string]any{"a": 2, "b": 3}},
		{"merge of a map that merges",
			"b1: &b1 {q: 1}\nb2: &b2\n  <<: *b1\n  r: 2\nm:\n  <<: [*b2, {q: 3, r: 3, s: 3}]\n",
			map[string]any{"q": 1, "r": 2, "s": 3}},
		{"one anchor twice",
			"b: &b {a: 1}\nm:\n  <<: [*b, *b]\n  c: 1\n",
			map[string]any{"a": 1, "c": 1}},
		{"tagged map", "m:\n  <<: !!map {a: 1}\n", map[string]any{"a": 1}},
		{"flow mapping", "m: {<<: {a: 1}, b: 2}\n", map[string]any{"a": 1, "b": 2}},
		{"single pair source", "m:\n  <<: {a: 1}\n", map[string]any{"a": 1}},
		{"empty list", "m:\n  <<: []\n  a: 1\n", map[string]any{"a": 1}},
		{"empty map", "m:\n  <<: {}\n", map[string]any{}},
		{"explicit merge key",
			"b: &b {a: 1, c: 1}\nm:\n  ? <<\n  : *b\n  c: 2\n",
			map[string]any{"a": 1, "c": 2}},
		{"explicit merge key with a list",
			"x: &x {a: 1}\ny: &y {a: 2, d: 2}\nm:\n  ? <<\n  : [*x, *y]\n",
			map[string]any{"a": 1, "d": 2}},
		{"explicit merge key with spaces and a comment",
			"m:\n  ?   <<   # c\n  :   {a: 1}\n  b: 2\n",
			map[string]any{"a": 1, "b": 2}},
		{"anchored explicit merge key", "m:\n  ? &x <<\n  : {a: 1}\n", map[string]any{"a": 1}},
		{"two explicit merge keys",
			"m:\n  ? <<\n  : {a: 1}\n  ? <<\n  : [{a: 2}]\n",
			map[string]any{"a": 2}},
		{"explicit and plain merge keys",
			"m:\n  ? <<\n  : {a: 1}\n  <<: {a: 2, b: 1}\n",
			map[string]any{"a": 2, "b": 1}},
		{"double-quoted key is no merge", "m:\n  \"<<\": {a: 1}\n", map[string]any{"<<": map[string]any{"a": 1}}},
		{"single-quoted key is no merge", "m:\n  '<<': {a: 1}\n", map[string]any{"<<": map[string]any{"a": 1}}},
		{"quoted explicit key is no merge", "m:\n  ? \"<<\"\n  : {a: 1}\n", map[string]any{"<<": map[string]any{"a": 1}}},
		{"string-tagged explicit key is no merge", "m:\n  ? !!str <<\n  : {a: 1}\n", map[string]any{"<<": map[string]any{"a": 1}}},
		{"explicit alias to a << string is no merge",
			"k: &x <<\nm:\n  ? *x\n  : {a: 1}\n",
			map[string]any{"<<": map[string]any{"a": 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := decodeMerge(t, c.doc)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := v.(map[string]any)["m"]
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("m = %#v, want %#v", got, c.want)
			}
		})
	}
}

// TestMergeKeysInListItems pins merge keys in the maps of a list.
func TestMergeKeysInListItems(t *testing.T) {
	v, err := decodeMerge(t, "l:\n- <<: {a: 1}\n  b: 2\n- <<: [{a: 3}, {a: 4, c: 4}]\n- ? <<\n  : {e: 5}\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"a": 1, "b": 2}, map[string]any{"a": 3, "c": 4}, map[string]any{"e": 5}}
	if got := v.(map[string]any)["l"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("l = %#v, want %#v", got, want)
	}
}

// TestMergeKeyValueRefused pins the merge values spruce refuses with
// "map merge requires map or sequence of maps as the value". That
// includes an alias to a list of maps, which its YAML library refuses
// though it takes the same list written in place. The error carries
// the merge key's line and quotes nothing of the document.
func TestMergeKeyValueRefused(t *testing.T) {
	cases := []struct {
		doc  string
		line int
	}{
		{"m:\n  <<: s3kr1t\n", 2},
		{"m:\n  <<: [1, 2]\n", 2},
		{"m:\n  <<: ~\n", 2},
		{"m:\n  <<:\n", 2},
		{"m:\n  q: 1\n  <<: !!str s3kr1t\n", 3},
		{"s: &s [{a: 1}]\nm:\n  <<: *s\n", 3},
		{"m:\n  <<: [{a: 1}, [1]]\n", 2},
		{"x: &x s3kr1t\nm:\n  <<: [*x]\n", 3},
		{"x: &x [1]\nm:\n  <<: [*x]\n", 3},
		{"x: &x {a: 1}\nm:\n  <<: [*x, s3kr1t]\n", 3},
		{"m:\n  ? <<\n  : s3kr1t\n", 2},
		{"m:\n  ? <<\n", 2},
		{"s: &s [{a: 1}]\nm:\n  ? <<\n  : *s\n", 3},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			_, err := decodeMerge(t, c.doc)
			if err == nil {
				t.Fatal("decode accepted the merge value")
			}
			want := fmt.Sprintf("yaml: line %d: map merge requires map or sequence of maps as the value", c.line)
			if err.Error() != want {
				t.Fatalf("error = %q, want %q", err, want)
			}
			if strings.Contains(err.Error(), "s3kr1t") {
				t.Fatal("error quotes the value")
			}
		})
	}
}

// TestMergeKeyUnknownAlias keeps the decoder's own error for a merge
// key that names an anchor the document never defines.
func TestMergeKeyUnknownAlias(t *testing.T) {
	_, err := decodeMerge(t, "pw: supersecret\n<<: *S3KRITzq9\n")
	if err == nil || strings.Contains(err.Error(), "map merge") {
		t.Fatalf("error = %v, want the unknown alias error", err)
	}
}

// TestMergeKeyRefusesAnUnknownAliasItDrops pins that an alias to an
// anchor the document never defines fails the decode, as it does
// anywhere else, even inside a merged map whose pairs all lose to an
// earlier one, so the fold drops them.
func TestMergeKeyRefusesAnUnknownAliasItDrops(t *testing.T) {
	for _, doc := range []string{
		"<<: [{n: 1}, {n: *a0}]\n",
		"m:\n  <<: [{n: 1}, {n: *a0}]\n",
		"m:\n  <<: {n: *a0}\n  n: 1\n",
		"m:\n  <<: [{n: 1}, {n: [*a0]}]\n",
		"m:\n  <<: [{n: 1}, {n: {k: *a0}}]\n",
	} {
		t.Run(doc, func(t *testing.T) {
			_, err := decodeMerge(t, doc)
			if err == nil || !strings.Contains(err.Error(), "could not find alias") {
				t.Fatalf("error = %v, want the unknown alias error", err)
			}
		})
	}
}

// TestMergeKeysStayBounded pins that merges of merges, merges of long
// lists of aliases, and an alias bomb built of merge keys all finish
// quickly, and that the bomb fails at the alias expansion limit.
func TestMergeKeysStayBounded(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("l0: &l0 {k0: 0}\n")
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&chain, "l%d: &l%d {<<: *l%d, k%d: %d}\n", i, i, i-1, i, i)
	}

	var wide strings.Builder
	wide.WriteString("a: &a {")
	for i := range 100 {
		fmt.Fprintf(&wide, "k%d: %d, ", i, i)
	}
	wide.WriteString("z: 0}\nm:\n  <<: [")
	for range 1000 {
		wide.WriteString("*a, ")
	}
	wide.WriteString("{z: 1}]\n")

	var bomb strings.Builder
	bomb.WriteString("l0: &l0 {k0: 0, k1: 1, k2: 2, k3: 3, k4: 4, k5: 5, k6: 6, k7: 7, k8: 8, k9: 9}\n")
	for i := 1; i <= 9; i++ {
		fmt.Fprintf(&bomb, "l%d: &l%d {<<: [", i, i)
		for range 10 {
			fmt.Fprintf(&bomb, "*l%d, ", i-1)
		}
		fmt.Fprintf(&bomb, "{x%d: %d}]}\n", i, i)
	}

	start := time.Now()
	v, err := decodeMerge(t, chain.String())
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	top := v.(map[string]any)["l300"].(map[string]any)
	if len(top) != 301 || top["k0"] != 0 || top["k300"] != 300 {
		t.Fatalf("chain l300 holds %d keys, want 301", len(top))
	}

	v, err = decodeMerge(t, wide.String())
	if err != nil {
		t.Fatalf("wide: %v", err)
	}
	m := v.(map[string]any)["m"].(map[string]any)
	if len(m) != 101 || m["z"] != 0 {
		t.Fatalf("wide m holds %d keys and z %v, want 101 and 0", len(m), m["z"])
	}

	_, err = decodeMerge(t, bomb.String())
	var limit *yamlcodec.MaxRecursionError
	if !errors.As(err, &limit) {
		t.Fatalf("bomb error = %v, want a MaxRecursionError", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the three documents took %v", elapsed)
	}
}

// TestDecodeEnvValueMergeKeys pins an environment value that holds a
// merge key, which spruce reads as it reads one in a document.
func TestDecodeEnvValueMergeKeys(t *testing.T) {
	cases := []struct {
		text string
		want any
	}{
		{"{<<: [{a: 1}, {a: 2}], b: 3}", map[string]any{"a": 1, "b": 3}},
		{"{<<: {a: 1}, <<: {a: 2}}", map[string]any{"a": 2}},
		{"{a: 0, <<: {a: 1}}", map[string]any{"a": 1}},
		{"{<<: 1}", "{<<: 1}"},
	}
	for _, c := range cases {
		if got := yamlcodec.DecodeEnvValue(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("DecodeEnvValue(%q) = %#v, want %#v", c.text, got, c.want)
		}
	}
}
