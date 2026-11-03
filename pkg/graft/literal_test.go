package graft

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/literal"
	pyaml "github.com/fivetwenty-io/graft/pkg/yaml"
)

// hasLiteral reports whether v holds a LiteralValue at any depth.
func hasLiteral(v interface{}) bool {
	return literal.Contains(v)
}

func TestNewLiteral(t *testing.T) {
	inner := map[string]interface{}{"a": 1}
	tests := []struct {
		name string
		in   interface{}
		want interface{}
	}{
		{name: "map", in: inner, want: inner},
		{name: "scalar", in: "x", want: "x"},
		{name: "nil", in: nil, want: nil},
		{name: "already wrapped", in: NewLiteral(inner), want: inner},
		{name: "wrapped twice", in: NewLiteral(NewLiteral("y")), want: "y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewLiteral(tt.in)
			if !reflect.DeepEqual(got.Value(), tt.want) {
				t.Fatalf("Value() = %#v, want %#v", got.Value(), tt.want)
			}
			if _, nested := got.Value().(LiteralValue); nested {
				t.Fatal("NewLiteral wrapped a LiteralValue a second time")
			}
		})
	}

	w := NewLiteral(inner)
	if NewLiteral(w) != w {
		t.Fatal("NewLiteral of a LiteralValue did not return it unchanged")
	}
	var zero LiteralValue
	if zero.Value() != nil {
		t.Fatal("the zero LiteralValue does not wrap nil")
	}
}

// TestNewLiteralCopies checks that NewLiteral keeps its own copy, so a
// later change to the value passed in does not reach the document, and
// that it turns map[interface{}]interface{} into the string-keyed shape
// a path read needs.
func TestNewLiteralCopies(t *testing.T) {
	list := []interface{}{"a"}
	in := map[string]interface{}{"list": list, "m": map[string]interface{}{"k": "v"}, "w": NewLiteral(map[string]interface{}{"x": 1})}
	w := NewLiteral(in)
	in["added"] = true
	list[0] = "changed"
	in["m"].(map[string]interface{})["k"] = "changed"
	want := map[string]interface{}{"list": []interface{}{"a"}, "m": map[string]interface{}{"k": "v"}, "w": map[string]interface{}{"x": 1}}
	if got := literal.Strip(w); !reflect.DeepEqual(got, want) {
		t.Fatalf("NewLiteral shares containers with its argument: %#v", got)
	}

	odd := NewLiteral(map[interface{}]interface{}{"k": "v", 7: []interface{}{map[interface{}]interface{}{true: "t"}}})
	wantOdd := map[string]interface{}{"k": "v", "7": []interface{}{map[string]interface{}{"true": "t"}}}
	if !reflect.DeepEqual(odd.Value(), wantOdd) {
		t.Fatalf("NewLiteral did not normalize map keys: %#v", odd.Value())
	}

	for _, mode := range literalEngineModes(t) {
		t.Run(mode.name, func(t *testing.T) {
			doc := NewDocument(map[string]interface{}{
				"w":   NewLiteral(map[interface{}]interface{}{"k": "v"}),
				"out": "(( grab w.k ))",
			})
			got, err := mode.eng.Evaluate(context.Background(), doc)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if v, _ := got.Get("out"); v != "v" {
				t.Fatalf("out = %#v, want the value read through the wrapper", v)
			}
		})
	}
}

// literalFixture returns a document tree with wrappers at the top, in a
// list, and nested inside another wrapper.
// styledScalars are strings whose YAML quoting or block style depends on
// the encoder seeing them as plain strings.
func styledScalars() map[string]interface{} {
	return map[string]interface{}{
		"bool":      "yes",
		"number":    "1.0",
		"date":      "2026-01-01",
		"empty":     "",
		"multiline": "first\nsecond\n",
		"colon":     "a: b",
	}
}

func literalFixture() map[string]interface{} {
	return map[string]interface{}{
		"plain": "p",
		"params": NewLiteral(map[string]interface{}{
			"cmd":    "(( vault \"secret/x:y\" ))",
			"nested": NewLiteral([]interface{}{"a", NewLiteral("b")}),
			"styled": NewLiteral(styledScalars()),
		}),
		"list": []interface{}{NewLiteral(map[string]interface{}{"name": "n1"}), "s"},
		"null": NewLiteral(nil),
	}
}

func literalFixturePlain() map[string]interface{} {
	return map[string]interface{}{
		"plain": "p",
		"params": map[string]interface{}{
			"cmd":    "(( vault \"secret/x:y\" ))",
			"nested": []interface{}{"a", "b"},
			"styled": styledScalars(),
		},
		"list": []interface{}{map[string]interface{}{"name": "n1"}, "s"},
		"null": nil,
	}
}

func TestLiteralOutputIsPlain(t *testing.T) {
	want := literalFixturePlain()
	tests := []struct {
		name string
		get  func(Document) interface{}
	}{
		{name: "RawData", get: func(d Document) interface{} { return d.RawData() }},
		{name: "GetData", get: func(d Document) interface{} { return d.GetData() }},
		{name: "ToMap", get: func(d Document) interface{} {
			m, ok := d.(interface{ ToMap() map[string]interface{} })
			if !ok {
				t.Fatal("document has no ToMap")
			}
			return m.ToMap()
		}},
		{name: "Get root", get: func(d Document) interface{} {
			v, err := d.Get("")
			if err != nil {
				t.Fatal(err)
			}
			return v
		}},
		{name: "Get list", get: func(d Document) interface{} {
			v, err := d.Get("list")
			if err != nil {
				t.Fatal(err)
			}
			return map[string]interface{}{"list": v, "plain": "p", "params": want["params"], "null": nil}
		}},
		{name: "GetMap params", get: func(d Document) interface{} {
			v, err := d.GetMap("params")
			if err != nil {
				t.Fatal(err)
			}
			return map[string]interface{}{"params": v, "plain": "p", "list": want["list"], "null": nil}
		}},
		{name: "SortKeys", get: func(d Document) interface{} { return d.SortKeys().RawData() }},
		{name: "Clone", get: func(d Document) interface{} { return d.Clone().RawData() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := NewDocument(literalFixture())
			got := tt.get(doc)
			if hasLiteral(got) {
				t.Fatalf("%s returned a LiteralValue", tt.name)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s = %#v, want %#v", tt.name, got, want)
			}
			if !hasLiteral(doc.(*document).data) {
				t.Fatalf("%s removed the wrappers from the document itself", tt.name)
			}
		})
	}
}

func TestLiteralOutputIsPlainText(t *testing.T) {
	plainDoc := NewDocument(literalFixturePlain())
	wantYAML, err := plainDoc.ToYAML()
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := plainDoc.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	wantIndent, err := plainDoc.ToJSONIndent("  ")
	if err != nil {
		t.Fatal(err)
	}
	// pkg/yaml quotes timestamp-like strings so they read back as strings,
	// so its output is compared with its own encoding of the plain data.
	wantPkgYAML, err := pyaml.Marshal(literalFixturePlain())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		got  func() ([]byte, error)
		want []byte
	}{
		{name: "ToYAML", got: func() ([]byte, error) { return NewDocument(literalFixture()).ToYAML() }, want: wantYAML},
		{name: "MarshalYAML", got: func() ([]byte, error) { return MarshalYAML(literalFixture()) }, want: wantYAML},
		{name: "ToJSON", got: func() ([]byte, error) { return NewDocument(literalFixture()).ToJSON() }, want: wantJSON},
		{name: "ToJSONIndent", got: func() ([]byte, error) { return NewDocument(literalFixture()).ToJSONIndent("  ") }, want: wantIndent},
		{name: "encoding/json", got: func() ([]byte, error) { return json.Marshal(literalFixture()) }, want: wantJSON},
		{name: "pkg/yaml Marshal", got: func() ([]byte, error) { return pyaml.Marshal(literalFixture()) }, want: wantPkgYAML},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.got()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("%s =\n%s\nwant\n%s", tt.name, got, tt.want)
			}
		})
	}
}

func TestLiteralPathsAreLeafPathsOfPlainData(t *testing.T) {
	got := NewDocument(literalFixture()).Paths()
	want := NewDocument(literalFixturePlain()).Paths()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Paths() = %v, want %v", got, want)
	}
}

// TestNewLiteralPanicsOnACycle checks that NewLiteral refuses a value
// that contains itself, quickly and with a message that quotes nothing
// from the value.
func TestNewLiteralPanicsOnACycle(t *testing.T) {
	m := map[string]interface{}{"secret": "cyc-probe"}
	m["a"], m["b"] = m, m
	done := make(chan interface{}, 1)
	go func() {
		defer func() { done <- recover() }()
		NewLiteral(m)
	}()
	select {
	case r := <-done:
		text, _ := r.(string)
		if text == "" || !strings.Contains(text, "contains itself") || strings.Contains(text, "cyc-probe") {
			t.Fatalf("NewLiteral on a cyclic value: recovered %v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("NewLiteral did not finish within a second")
	}
}
