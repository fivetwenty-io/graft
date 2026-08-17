package graft

import (
	"bytes"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// These tests characterize graft's YAML marshal output against spruce's
// (github.com/geofffranks/yaml, a yaml.v2 fork) observed behavior for null
// rendering and map key ordering, confirmed by running both `spruce merge`
// and `graft merge` binaries over equivalent fixtures. See
// docs/spruce/yaml-formatting.md for the observed formatting differences.

// TestMarshalYAML_NullRendering locks in that every null representation
// (explicit `null`, `~`, and an empty scalar) marshals to the bare word
// `null`, matching spruce's output byte-for-byte.
func TestMarshalYAML_NullRendering(t *testing.T) {
	data := map[string]interface{}{
		"explicit_null": nil, // from `key: null` or `key: ~` or `key:`
		"nested": map[string]interface{}{
			"a": nil,
		},
		"list_with_null": []interface{}{nil, "foo", nil},
		"quoted_null":    "null", // string "null", must stay quoted
		"quoted_tilde":   "~",    // string "~", must stay quoted
	}

	out, err := MarshalYAML(data)
	if err != nil {
		t.Fatalf("MarshalYAML returned error: %v", err)
	}
	got := string(out)

	mustContain := []string{
		"explicit_null: null",
		"a: null",
		"- null",
		`quoted_null: "null"`,
		`quoted_tilde: "~"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q; full output:\n%s", want, got)
		}
	}

	// spruce never renders the `~` short form on output, regardless of
	// which null spelling was used on input — graft must match.
	if strings.Contains(got, "~") && !strings.Contains(got, `"~"`) {
		t.Errorf("output rendered bare `~` instead of `null`; full output:\n%s", got)
	}
}

// TestMarshalYAML_KeyOrdering locks in that map keys are emitted in
// alphabetical order regardless of insertion order, matching spruce's
// yaml.v2-family marshal behavior (yaml.v2 sorts map keys on encode).
func TestMarshalYAML_KeyOrdering(t *testing.T) {
	data := map[string]interface{}{
		"zebra": 1,
		"apple": 2,
		"mango": 3,
		"banana": map[string]interface{}{
			"z": 1,
			"a": 2,
			"m": 3,
		},
	}

	out, err := MarshalYAML(data)
	if err != nil {
		t.Fatalf("MarshalYAML returned error: %v", err)
	}
	got := string(out)

	wantOrder := []string{"apple:", "banana:", "mango:", "zebra:"}
	lastIdx := -1
	for _, key := range wantOrder {
		idx := strings.Index(got, key)
		if idx == -1 {
			t.Fatalf("output missing top-level key %q; full output:\n%s", key, got)
		}
		if idx < lastIdx {
			t.Fatalf("key %q out of alphabetical order; full output:\n%s", key, got)
		}
		lastIdx = idx
	}

	nestedOrder := []string{"a:", "m:", "z:"}
	nestedLastIdx := -1
	for _, key := range nestedOrder {
		idx := strings.Index(got, "\n  "+key)
		if idx == -1 {
			t.Fatalf("output missing nested key %q; full output:\n%s", key, got)
		}
		if idx < nestedLastIdx {
			t.Fatalf("nested key %q out of alphabetical order; full output:\n%s", key, got)
		}
		nestedLastIdx = idx
	}
}

// TestMarshalYAML_KeyOrderingDeterministic guards against Go's randomized
// map iteration order leaking into marshal output — a real correctness
// requirement now that parallel evaluation is enabled by default and
// genesis diffs manifests byte-for-byte.
// TestParseYAML_BareTrailingSequenceItem locks in the workaround for a
// goccy/go-yaml v1.19.2 parser bug: a block-sequence item consisting of a
// bare "-" (no value token) immediately followed by a sibling top-level
// key was silently misparsed -- the sibling key nested inside the empty
// list item instead of terminating the sequence. spruce parses the bare
// "-" as an explicit null list entry and keeps the following key as a
// sibling; graft must match.
func TestParseYAML_BareTrailingSequenceItem(t *testing.T) {
	engine := NewDefaultEngine()

	input := []byte("list1:\n- a\n- b\n-\nnext_key: value\n")

	doc, err := engine.ParseYAML(input)
	if err != nil {
		t.Fatalf("ParseYAML returned error: %v", err)
	}

	list, err := doc.Get("list1")
	if err != nil {
		t.Fatalf("Get(list1) failed: %v", err)
	}
	items, ok := list.([]interface{})
	if !ok {
		t.Fatalf("list1 is not a slice, got %T: %v", list, list)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items in list1, got %d: %v", len(items), items)
	}
	if items[0] != "a" || items[1] != "b" || items[2] != nil {
		t.Fatalf("expected [a b <nil>], got %v", items)
	}

	nextKey, err := doc.Get("next_key")
	if err != nil {
		t.Fatalf("next_key was swallowed into the list instead of staying a sibling key: %v", err)
	}
	if nextKey != "value" {
		t.Fatalf("expected next_key=value, got %v", nextKey)
	}
}

// TestParseYAML_BareTrailingSequenceItem_NestedAndDedented covers the
// same bug trigger at a nested indent level, where the sibling key
// dedents past the list's owning key entirely.
func TestParseYAML_BareTrailingSequenceItem_NestedAndDedented(t *testing.T) {
	engine := NewDefaultEngine()

	input := []byte("top:\n  list1:\n  - a\n  -\nnext_key: value\n")

	doc, err := engine.ParseYAML(input)
	if err != nil {
		t.Fatalf("ParseYAML returned error: %v", err)
	}

	list, err := doc.Get("top.list1")
	if err != nil {
		t.Fatalf("Get(top.list1) failed: %v", err)
	}
	items, ok := list.([]interface{})
	if !ok {
		t.Fatalf("top.list1 is not a slice, got %T: %v", list, list)
	}
	if len(items) != 2 || items[0] != "a" || items[1] != nil {
		t.Fatalf("expected [a <nil>], got %v", items)
	}

	nextKey, err := doc.Get("next_key")
	if err != nil || nextKey != "value" {
		t.Fatalf("expected top-level next_key=value sibling, got %v, err=%v", nextKey, err)
	}
}

// TestParseYAML_BareDashFollowedBySequenceItem confirms a bare "-"
// followed by another sequence item (not a mapping key) is left
// untouched -- this shape does not trigger the goccy bug and both items
// should parse as null list entries.
func TestParseYAML_BareDashFollowedBySequenceItem(t *testing.T) {
	engine := NewDefaultEngine()

	input := []byte("list1:\n- a\n-\n-\nnext_key: value\n")

	doc, err := engine.ParseYAML(input)
	if err != nil {
		t.Fatalf("ParseYAML returned error: %v", err)
	}

	list, err := doc.Get("list1")
	if err != nil {
		t.Fatalf("Get(list1) failed: %v", err)
	}
	items, ok := list.([]interface{})
	if !ok {
		t.Fatalf("list1 is not a slice, got %T: %v", list, list)
	}
	if len(items) != 3 || items[0] != "a" || items[1] != nil || items[2] != nil {
		t.Fatalf("expected [a <nil> <nil>], got %v", items)
	}

	nextKey, err := doc.Get("next_key")
	if err != nil || nextKey != "value" {
		t.Fatalf("expected next_key=value sibling, got %v, err=%v", nextKey, err)
	}
}

// TestParseYAML_BareDashInBlockScalarUntouched confirms a line that is
// literally just "-" inside a literal block scalar's body is never
// rewritten -- only real bare sequence terminators are guarded against.
func TestParseYAML_BareDashInBlockScalarUntouched(t *testing.T) {
	engine := NewDefaultEngine()

	input := []byte("desc: |\n  line one\n  -\n  line three\nnext: value\n")

	doc, err := engine.ParseYAML(input)
	if err != nil {
		t.Fatalf("ParseYAML returned error: %v", err)
	}

	desc, err := doc.Get("desc")
	if err != nil {
		t.Fatalf("Get(desc) failed: %v", err)
	}
	want := "line one\n-\nline three\n"
	if desc != want {
		t.Fatalf("block scalar content corrupted: got %q, want %q", desc, want)
	}

	next, err := doc.Get("next")
	if err != nil || next != "value" {
		t.Fatalf("expected next=value sibling, got %v, err=%v", next, err)
	}
}

// TestMarshalYAML_QuotesSpecialFloatLookalikeStrings locks in that
// string values matching goccy/go-yaml's reserved Inf/NaN float
// keywords (".nan", ".inf", "-.inf", and case variants) stay quoted on
// marshal, matching spruce, so re-parsing yields a string rather than a
// float. See needsExplicitQuote in yaml.go for the goccy root cause.
func TestMarshalYAML_QuotesSpecialFloatLookalikeStrings(t *testing.T) {
	values := []string{
		".nan", ".NaN", ".NAN",
		".inf", ".Inf", ".INF",
		"-.inf", "-.Inf", "-.INF",
	}

	for _, v := range values {
		data := map[string]interface{}{"v": v}
		out, err := MarshalYAML(data)
		if err != nil {
			t.Fatalf("MarshalYAML(%q) returned error: %v", v, err)
		}

		var back map[string]interface{}
		if err := yaml.Unmarshal(out, &back); err != nil {
			t.Fatalf("re-parsing marshal output of %q failed: %v\noutput:\n%s", v, err, out)
		}

		got, ok := back["v"].(string)
		if !ok || got != v {
			t.Errorf("value %q round-tripped as %v (%T), not the original string; marshal output:\n%s",
				v, back["v"], back["v"], out)
		}
	}
}

// TestMarshalYAML_PlusInfLookalikeUnaffected confirms "+.inf" is left
// alone: goccy does not recognize a "+" sign on the Inf keyword, so it
// already round-trips as a string without help, and forcing a quote
// here would just be cosmetic noise.
func TestMarshalYAML_PlusInfLookalikeUnaffected(t *testing.T) {
	data := map[string]interface{}{"v": "+.inf"}
	out, err := MarshalYAML(data)
	if err != nil {
		t.Fatalf("MarshalYAML returned error: %v", err)
	}
	if strings.Contains(string(out), `"+.inf"`) {
		t.Errorf("expected +.inf to stay unquoted (already round-trips as a string), got:\n%s", out)
	}

	var back map[string]interface{}
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-parsing marshal output failed: %v", err)
	}
	if got, ok := back["v"].(string); !ok || got != "+.inf" {
		t.Errorf("expected v=+.inf (string), got %v (%T)", back["v"], back["v"])
	}
}

// TestMarshalYAML_TrickyStringSetAlreadyRoundTrips is a regression guard
// covering the standard tricky-string set (YAML 1.1 booleans, null
// words, octal/hex-lookalikes, and numeric-lookalike strings): these
// were verified against spruce's output to already round-trip as
// strings through graft's marshal without needing the forced-quote
// workaround, and must keep doing so.
func TestMarshalYAML_TrickyStringSetAlreadyRoundTrips(t *testing.T) {
	values := []string{
		"true", "True", "TRUE", "false", "False", "FALSE",
		"yes", "Yes", "YES", "no", "No", "NO",
		"on", "On", "ON", "off", "Off", "OFF",
		"null", "Null", "NULL", "~",
		"0123", "0o123", "0x1A", "0X1A", "0b101",
		"123", "1.5", "1e10", "1E10", "+123", "-123", ".5", "-.5",
	}

	for _, v := range values {
		data := map[string]interface{}{"v": v}
		out, err := MarshalYAML(data)
		if err != nil {
			t.Fatalf("MarshalYAML(%q) returned error: %v", v, err)
		}

		var back map[string]interface{}
		if err := yaml.Unmarshal(out, &back); err != nil {
			t.Fatalf("re-parsing marshal output of %q failed: %v\noutput:\n%s", v, err, out)
		}

		got, ok := back["v"].(string)
		if !ok || got != v {
			t.Errorf("value %q round-tripped as %v (%T), not the original string; marshal output:\n%s",
				v, back["v"], back["v"], out)
		}
	}
}

func TestMarshalYAML_KeyOrderingDeterministic(t *testing.T) {
	data := map[string]interface{}{
		"zebra":   1,
		"apple":   2,
		"mango":   3,
		"newkey1": "a",
		"banana": map[string]interface{}{
			"new_b": 5,
			"z":     100,
			"a":     2,
			"m":     3,
		},
	}

	first, err := MarshalYAML(data)
	if err != nil {
		t.Fatalf("MarshalYAML returned error: %v", err)
	}

	const iterations = 50
	for i := 0; i < iterations; i++ {
		out, err := MarshalYAML(data)
		if err != nil {
			t.Fatalf("MarshalYAML returned error on iteration %d: %v", i, err)
		}
		if !bytes.Equal(out, first) {
			t.Fatalf("nondeterministic marshal output on iteration %d:\nfirst:\n%s\ngot:\n%s", i, first, out)
		}
	}
}

// TestParseYAML_QuotedYAML11BoolLookalikesStayStrings locks in that an
// explicitly quoted "yes"/"no"/"on"/"off" scalar (single- or
// double-quoted, any of the case variants YAMLCompat recognizes) is
// decoded as a string, not coerced to a boolean. spruce (yaml.v2-family)
// keeps quoted values as strings; graft's YAML 1.1 compat layer was
// coercing them regardless of quoting, silently changing their type.
// Confirmed against a real spruce binary on the same fixture.
func TestParseYAML_QuotedYAML11BoolLookalikesStayStrings(t *testing.T) {
	input := []byte(`double_yes: "yes"
single_on: 'On'
double_NO: "NO"
single_off: 'off'
double_YES: "YES"
list:
- "yes"
- keep
- 'OFF'
nested:
  flag: "on"
`)

	engine := NewDefaultEngine()
	doc, err := engine.ParseYAML(input)
	if err != nil {
		t.Fatalf("ParseYAML returned error: %v", err)
	}

	cases := []struct {
		path string
		want string
	}{
		{"double_yes", "yes"},
		{"single_on", "On"},
		{"double_NO", "NO"},
		{"single_off", "off"},
		{"double_YES", "YES"},
		{"nested.flag", "on"},
	}
	for _, c := range cases {
		got, err := doc.Get(c.path)
		if err != nil {
			t.Fatalf("Get(%s) failed: %v", c.path, err)
		}
		if got != c.want {
			t.Errorf("%s = %v (%T), want string %q (quoted bool-lookalikes must stay strings)", c.path, got, got, c.want)
		}
	}

	list, err := doc.Get("list")
	if err != nil {
		t.Fatalf("Get(list) failed: %v", err)
	}
	items, ok := list.([]interface{})
	if !ok || len(items) != 3 {
		t.Fatalf("list is not a 3-element slice, got %T: %v", list, list)
	}
	if items[0] != "yes" {
		t.Errorf("list[0] = %v (%T), want string \"yes\"", items[0], items[0])
	}
	if items[1] != "keep" {
		t.Errorf("list[1] = %v (%T), want string \"keep\"", items[1], items[1])
	}
	if items[2] != "OFF" {
		t.Errorf("list[2] = %v (%T), want string \"OFF\"", items[2], items[2])
	}
}

// TestParseYAML_UnquotedYAML11BoolLookalikesStillCoerce is the
// companion regression guard to the quoted-string test above: an
// unquoted yes/no/on/off (any recognized case variant) must keep
// coercing to a boolean, matching spruce's YAML 1.1 behavior. The fix
// for the quoted case must not touch this path.
func TestParseYAML_UnquotedYAML11BoolLookalikesStillCoerce(t *testing.T) {
	input := []byte(`bare_yes: yes
bare_on: On
bare_NO: NO
bare_off: off
list:
- yes
- keep
- OFF
nested:
  flag: on
`)

	engine := NewDefaultEngine()
	doc, err := engine.ParseYAML(input)
	if err != nil {
		t.Fatalf("ParseYAML returned error: %v", err)
	}

	cases := []struct {
		path string
		want bool
	}{
		{"bare_yes", true},
		{"bare_on", true},
		{"bare_NO", false},
		{"bare_off", false},
		{"nested.flag", true},
	}
	for _, c := range cases {
		got, err := doc.Get(c.path)
		if err != nil {
			t.Fatalf("Get(%s) failed: %v", c.path, err)
		}
		if got != c.want {
			t.Errorf("%s = %v (%T), want bool %v", c.path, got, got, c.want)
		}
	}

	list, err := doc.Get("list")
	if err != nil {
		t.Fatalf("Get(list) failed: %v", err)
	}
	items, ok := list.([]interface{})
	if !ok || len(items) != 3 {
		t.Fatalf("list is not a 3-element slice, got %T: %v", list, list)
	}
	if items[0] != true {
		t.Errorf("list[0] = %v (%T), want true", items[0], items[0])
	}
	if items[1] != "keep" {
		t.Errorf("list[1] = %v (%T), want string \"keep\"", items[1], items[1])
	}
	if items[2] != false {
		t.Errorf("list[2] = %v (%T), want false", items[2], items[2])
	}
}

// TestParseYAML_QuotedYAML11BoolLookalikeInsideBlockLiteralUntouched
// confirms the fix's AST-token-based detection never touches literal
// text that merely contains a quote-bounded bool-lookalike word inside
// a block scalar body -- that text is data, not a YAML quoted scalar,
// and must survive byte-for-byte.
func TestParseYAML_QuotedYAML11BoolLookalikeInsideBlockLiteralUntouched(t *testing.T) {
	input := []byte("desc: |\n  Set this to \"yes\" to enable.\n  Or 'off' to disable.\n")

	engine := NewDefaultEngine()
	doc, err := engine.ParseYAML(input)
	if err != nil {
		t.Fatalf("ParseYAML returned error: %v", err)
	}

	got, err := doc.Get("desc")
	if err != nil {
		t.Fatalf("Get(desc) failed: %v", err)
	}
	want := "Set this to \"yes\" to enable.\nOr 'off' to disable.\n"
	if got != want {
		t.Errorf("desc = %q, want %q (marker must not leak into block-literal content)", got, want)
	}
}

// TestParseYAML_QuotedYAML11BoolLookalikeRoundTripsThroughMarshal
// confirms the fix survives a full parse -> marshal round trip: a
// quoted "yes" decodes as a string and marshals back out re-quoted
// (goccy already quotes these words on encode; only the decode side
// needed the fix), matching spruce byte-for-byte.
func TestParseYAML_QuotedYAML11BoolLookalikeRoundTripsThroughMarshal(t *testing.T) {
	engine := NewDefaultEngine()
	doc, err := engine.ParseYAML([]byte(`a: "yes"
b: yes
`))
	if err != nil {
		t.Fatalf("ParseYAML returned error: %v", err)
	}

	out, err := MarshalYAML(map[string]interface{}{
		"a": mustGet(t, doc, "a"),
		"b": mustGet(t, doc, "b"),
	})
	if err != nil {
		t.Fatalf("MarshalYAML returned error: %v", err)
	}

	got := string(out)
	if !strings.Contains(got, `a: "yes"`) {
		t.Errorf(`expected a: "yes" (re-quoted string) in output, got:\n%s`, got)
	}
	if !strings.Contains(got, "b: true") {
		t.Errorf("expected b: true (unquoted yes still coerces) in output, got:\n%s", got)
	}
}

func mustGet(t *testing.T, doc Document, path string) interface{} {
	t.Helper()
	v, err := doc.Get(path)
	if err != nil {
		t.Fatalf("Get(%s) failed: %v", path, err)
	}
	return v
}

// TestMarshalYAML_SpruceKeyOrder locks in the spruce-compatible two-tier
// key order on encode: numeric-looking keys first (numerically), then
// string keys in spruce's natural order (digit runs numeric, non-letters
// before letters). Expected byte output verified against the live spruce
// 1.35.16 binary on equivalent fixtures; key quoting is graft's own
// (coerced numeric-looking keys stay quoted strings — a documented label
// difference from spruce's bare typed keys).
func TestMarshalYAML_SpruceKeyOrder(t *testing.T) {
	cases := []struct {
		name string
		data interface{}
		want string
	}{
		{
			name: "mixed numeric and string keys",
			data: map[string]interface{}{
				"m": map[string]interface{}{
					"10": 1, "2": 1, "9": 1, "10x": 1, "alpha": 1, "zulu": 1,
				},
			},
			want: "m:\n  \"2\": 1\n  \"9\": 1\n  \"10\": 1\n  10x: 1\n  alpha: 1\n  zulu: 1\n",
		},
		{
			name: "integer-only keys",
			data: map[string]interface{}{
				"ports": map[string]interface{}{"443": 1, "80": 1, "8080": 1},
			},
			want: "ports:\n  \"80\": 1\n  \"443\": 1\n  \"8080\": 1\n",
		},
		{
			name: "embedded digit runs in string keys",
			data: map[string]interface{}{
				"jobs": map[string]interface{}{"item10": 1, "item9": 1, "item2": 1},
				"azs":  map[string]interface{}{"z1a": 1, "z10a": 1, "z2b": 1},
			},
			want: "azs:\n  z1a: 1\n  z2b: 1\n  z10a: 1\njobs:\n  item2: 1\n  item9: 1\n  item10: 1\n",
		},
		{
			name: "empty digit run counts as zero",
			data: map[string]interface{}{
				"int64_val": 1, "int_val": 1,
			},
			want: "int_val: 1\nint64_val: 1\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := MarshalYAML(tc.data)
			if err != nil {
				t.Fatalf("MarshalYAML returned error: %v", err)
			}
			if string(out) != tc.want {
				t.Fatalf("output mismatch\n got:\n%s\nwant:\n%s", out, tc.want)
			}
		})
	}
}

// TestMarshalYAML_SpruceKeyOrder_Punctuation covers the non-letter-
// before-letter branch (which spans punctuation, not just digits):
// spruce emits _x, |p, ~t, Zx, ax for these five keys. Asserted by
// relative position rather than full bytes so goccy's own key-quoting
// style stays out of scope.
func TestMarshalYAML_SpruceKeyOrder_Punctuation(t *testing.T) {
	out, err := MarshalYAML(map[string]interface{}{
		"_x": 1, "Zx": 1, "ax": 1, "~t": 1, "|p": 1,
	})
	if err != nil {
		t.Fatalf("MarshalYAML returned error: %v", err)
	}
	got := string(out)
	lastIdx := -1
	for _, key := range []string{"_x", "|p", "~t", "Zx", "ax"} {
		idx := strings.Index(got, key)
		if idx == -1 {
			t.Fatalf("output missing key %q; full output:\n%s", key, got)
		}
		if idx < lastIdx {
			t.Fatalf("key %q out of spruce order; full output:\n%s", key, got)
		}
		lastIdx = idx
	}
}

// TestMarshalYAML_NonStringMapKeys locks in that a
// map[interface{}]interface{} whose keys are not strings marshals
// without panicking (goccy's MapItem encoder type-asserts .(string)
// unchecked) and that the stringified keys join the normal ordering.
func TestMarshalYAML_NonStringMapKeys(t *testing.T) {
	out, err := MarshalYAML(map[string]interface{}{
		"m": map[interface{}]interface{}{
			10:      "ten",
			2:       "two",
			"alpha": "a",
		},
	})
	if err != nil {
		t.Fatalf("MarshalYAML returned error: %v", err)
	}
	want := "m:\n  \"2\": two\n  \"10\": ten\n  alpha: a\n"
	if string(out) != want {
		t.Fatalf("output mismatch\n got:\n%s\nwant:\n%s", out, want)
	}
}

// TestMarshalYAML_SingleQuoteStyle locks in spruce's quote character for
// strings that need quoting but contain nothing requiring escapes: spruce
// (yaml.v2 family) emits them single-quoted (`'*.uaa.((system_domain))'`),
// and downstream consumers depend on that byte shape — genesis's Credhub
// entombment step regex-replaces `((...))`  with `""` inside the rendered
// manifest before re-parsing it, which stays valid YAML inside single
// quotes but is malformed inside double quotes (`"*.uaa."""`).
func TestMarshalYAML_SingleQuoteStyle(t *testing.T) {
	data := map[string]interface{}{
		"uris": []interface{}{
			"*.uaa.((system_domain))",
			"uaa.((system_domain))",
		},
	}

	out, err := MarshalYAML(data)
	if err != nil {
		t.Fatalf("MarshalYAML failed: %v", err)
	}

	got := string(out)
	if !strings.Contains(got, "- '*.uaa.((system_domain))'") {
		t.Errorf("expected single-quoted '*.uaa.((system_domain))', got:\n%s", got)
	}
	if !strings.Contains(got, "- uaa.((system_domain))") ||
		strings.Contains(got, "'uaa.((system_domain))'") {
		t.Errorf("expected plain (unquoted) uaa.((system_domain)), got:\n%s", got)
	}
}
