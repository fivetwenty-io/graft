package literal_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/internal/literal"
)

func TestWrap(t *testing.T) {
	inner := map[string]any{"a": 1}
	w := literal.Wrap(inner)
	if literal.Wrap(w) != w {
		t.Fatal("Wrap of a Value is not the same Value")
	}
	if !reflect.DeepEqual(w.Value(), inner) {
		t.Fatalf("Value() = %#v", w.Value())
	}
	if literal.Wrap(nil).Value() != nil || (literal.Value{}).Value() != nil {
		t.Fatal("a Value of nil does not return nil")
	}
	// Two Values that hold maps compare with == without a panic.
	first, second := literal.Wrap(inner), literal.Wrap(inner)
	if first == second {
		t.Fatal("two separate wraps compare equal")
	}
	if !literal.Is(w) || literal.Is(inner) {
		t.Fatal("Is misreports")
	}
	if v, ok := literal.Unwrap(w); !ok || !reflect.DeepEqual(v, inner) {
		t.Fatal("Unwrap of a Value")
	}
	if v, ok := literal.Unwrap("x"); ok || v != "x" {
		t.Fatal("Unwrap of a plain value")
	}
}

func TestStrip(t *testing.T) {
	plainList := []any{"x"}
	plainMap := map[string]any{"k": plainList}
	tests := []struct {
		name string
		in   any
		want any
		same bool
	}{
		{name: "scalar", in: 1, want: 1, same: true},
		{name: "wrapper", in: literal.Wrap("s"), want: "s"},
		{name: "nil wrapper", in: literal.Wrap(nil), want: nil},
		{name: "nested wrappers", in: map[string]any{
			"a": literal.Wrap(map[string]any{"b": literal.Wrap([]any{literal.Wrap(1)})}),
			"c": map[any]any{"d": literal.Wrap(2)},
		}, want: map[string]any{
			"a": map[string]any{"b": []any{1}},
			"c": map[any]any{"d": 2},
		}},
		{name: "no wrapper map", in: plainMap, want: plainMap, same: true},
		{name: "no wrapper list", in: plainList, want: plainList, same: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := literal.Strip(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Strip = %#v, want %#v", got, tt.want)
			}
			if literal.Contains(got) {
				t.Fatal("Strip left a wrapper")
			}
			if literal.Contains(tt.in) == tt.same {
				t.Fatalf("Contains = %v on the input", !tt.same)
			}
			if tt.same && reflect.ValueOf(tt.in).Kind() != reflect.Int &&
				reflect.ValueOf(got).Pointer() != reflect.ValueOf(tt.in).Pointer() {
				t.Fatal("Strip copied a container with no wrapper in it")
			}
		})
	}

	in := map[string]any{"a": literal.Wrap(1), "b": plainList}
	_ = literal.Strip(in)
	if !literal.Is(in["a"]) {
		t.Fatal("Strip changed its input")
	}
}

func TestCopy(t *testing.T) {
	inner := map[string]any{"a": []any{map[string]any{"b": 1}}, "c": map[any]any{"d": 2}}
	w := literal.Wrap(inner)
	copied, ok := literal.Copy(map[string]any{"w": w}).(map[string]any)
	if !ok {
		t.Fatal("Copy of a map is not a map")
	}
	c, ok := copied["w"].(literal.Value)
	if !ok {
		t.Fatal("Copy lost the wrapper")
	}
	inner["a"].([]any)[0].(map[string]any)["b"] = 9
	inner["c"].(map[any]any)["d"] = 9
	want := map[string]any{"a": []any{map[string]any{"b": 1}}, "c": map[any]any{"d": 2}}
	if !reflect.DeepEqual(c.Value(), want) {
		t.Fatalf("Copy shares containers with the original: %#v", c.Value())
	}
}

func TestMarshalers(t *testing.T) {
	w := literal.Wrap(map[string]any{"a": literal.Wrap([]any{1})})
	y, err := w.MarshalYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(y, map[string]any{"a": []any{1}}) {
		t.Fatalf("MarshalYAML = %#v", y)
	}
	j, err := json.Marshal(map[string]any{"w": w})
	if err != nil {
		t.Fatal(err)
	}
	if string(j) != `{"w":{"a":[1]}}` {
		t.Fatalf("json = %s", j)
	}
}

func TestDeepValuesStopAtTheBound(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = literal.Wrap(cyclic)
	// Neither call may overflow the stack.
	_ = literal.Strip(cyclic)
	_ = literal.Copy(cyclic)
	_ = literal.Contains(cyclic)
}

func TestGuardText(t *testing.T) {
	plain := map[string]any{"a": "x", "l": []any{1, "y"}}
	if out, changed := literal.GuardText(plain); changed || !reflect.DeepEqual(out, plain) {
		t.Fatalf("GuardText changed data with no operator text: %#v", out)
	}
	kept := literal.Wrap("(( grab a ))")
	in := map[string]any{
		"op":   "(( grab a ))",
		"mid":  "x (( y",
		"keep": "plain",
		"lit":  kept,
		"list": []any{"((", 1},
	}
	out, changed := literal.GuardText(in)
	if !changed {
		t.Fatal("GuardText reported no change")
	}
	m := out.(map[string]any)
	for _, k := range []string{"op", "mid"} {
		if !literal.Is(m[k]) {
			t.Fatalf("%s is not wrapped: %#v", k, m[k])
		}
	}
	if m["keep"] != "plain" || m["lit"] != kept {
		t.Fatalf("GuardText touched a value it must leave: %#v", m)
	}
	if !literal.Is(m["list"].([]any)[0]) {
		t.Fatal("a list item is not wrapped")
	}
	if _, isString := in["op"].(string); !isString || !reflect.DeepEqual(in["list"], []any{"((", 1}) {
		t.Fatal("GuardText changed its input")
	}
	if g, changed := literal.GuardText("(("); !changed || !literal.Is(g) {
		t.Fatal("a bare string is not wrapped")
	}
}

func TestTexts(t *testing.T) {
	v := map[string]any{
		"outside": "not this",
		"in": literal.Wrap(map[string]any{
			"key": []any{"leaf", 42, nil, literal.Wrap("deeper")},
			"odd": map[any]any{7: "seven"},
		}),
	}
	got := literal.Texts(v)
	want := map[string]bool{"key": true, "leaf": true, "42": true, "deeper": true, "odd": true, "7": true, "seven": true}
	if len(got) != len(want) {
		t.Fatalf("Texts = %q", got)
	}
	for _, s := range got {
		if !want[s] {
			t.Fatalf("Texts returned %q, which is not inside a literal: %q", s, got)
		}
	}
}

func TestNew(t *testing.T) {
	inner := map[string]any{"a": []any{1}}
	w := literal.New(map[any]any{"m": inner, 2: literal.Wrap(map[any]any{"x": "y"})})
	inner["a"].([]any)[0] = "changed"
	want := map[string]any{"m": map[string]any{"a": []any{1}}, "2": map[string]any{"x": "y"}}
	if got := literal.Strip(w); !reflect.DeepEqual(got, want) {
		t.Fatalf("New = %#v", got)
	}
	if !literal.Is(w.Value().(map[string]any)["2"]) {
		t.Fatal("New dropped a nested Value")
	}
	if literal.New(w) != w {
		t.Fatal("New of a Value is not the same Value")
	}
}
