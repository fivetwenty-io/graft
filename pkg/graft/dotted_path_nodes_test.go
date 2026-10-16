package graft

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

// These tests cover document locations whose path nodes contain dots:
// a list entry whose name has dots, such as the networks of a Genesis 3
// cloud config (ocfp-lab-ocf.rabbitmq.net-rabbitmq), or a map key with
// dots. Writing such a location as a plain dotted path string loses
// where one node ends and the next begins, so every path that is queued
// as a string for later processing must keep each node whole.

// evaluateYAMLData evaluates a single document with Engine.Evaluate.
func evaluateYAMLData(t *testing.T, src string) map[string]interface{} {
	t.Helper()

	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("evaluated document is not a map: %T", result.RawData())
	}
	return data
}

// mergeYAMLData merges docs with the merge builder, as graft merge does.
func mergeYAMLData(t *testing.T, docs ...string) map[string]interface{} {
	t.Helper()

	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	result, err := mergeYAML(context.Background(), t, engine, docs...)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("merged document is not a map: %T", result.RawData())
	}
	return data
}

// firstEntry returns data[list][0] as a map.
func firstEntry(t *testing.T, data map[string]interface{}, list string) map[string]interface{} {
	t.Helper()

	entries, ok := data[list].([]interface{})
	if !ok || len(entries) == 0 {
		t.Fatalf("%s[0] not found in %#v", list, data)
	}
	entry, ok := entries[0].(map[string]interface{})
	if !ok {
		t.Fatalf("%s[0] is not a map: %#v", list, entries[0])
	}
	return entry
}

// dottedNames pairs an undotted name, whose behavior already worked,
// with dotted ones.
var dottedNames = []string{"abc", "a.b.c", "ocfp-lab-ocf.rabbitmq.net-rabbitmq"}

func TestPruneOperatorUnderDottedNames(t *testing.T) {
	const tmpl = `networks:
- name: %[1]s
  foo: (( prune ))
  keep: 1
meta:
  %[1]s: (( prune ))
  other: 2
`

	flows := map[string]func(t *testing.T, src string) map[string]interface{}{
		"evaluate": evaluateYAMLData,
		"merge": func(t *testing.T, src string) map[string]interface{} {
			return mergeYAMLData(t, src)
		},
	}

	for flow, run := range flows {
		for _, name := range dottedNames {
			t.Run(flow+"/"+name, func(t *testing.T) {
				data := run(t, fmt.Sprintf(tmpl, name))

				network := firstEntry(t, data, "networks")
				if v, present := network["foo"]; present {
					t.Errorf("networks[%s].foo survived pruning: %#v", name, v)
				}
				if network["keep"] != 1 {
					t.Errorf("networks[%s].keep = %#v, want 1", name, network["keep"])
				}

				meta, ok := data["meta"].(map[string]interface{})
				if !ok {
					t.Fatalf("meta is not a map: %#v", data["meta"])
				}
				if v, present := meta[name]; present {
					t.Errorf("meta[%s] survived pruning: %#v", name, v)
				}
				if meta["other"] != 2 {
					t.Errorf("meta.other = %#v, want 2", meta["other"])
				}
			})
		}
	}
}

func TestPruneFallbackUnderDottedNames(t *testing.T) {
	const tmpl = `networks:
- name: %[1]s
  foo: (( grab meta.missing || (( prune )) ))
  keep: 1
`

	for _, name := range dottedNames {
		t.Run(name, func(t *testing.T) {
			network := firstEntry(t, mergeYAMLData(t, fmt.Sprintf(tmpl, name)), "networks")
			if v, present := network["foo"]; present {
				t.Errorf("networks[%s].foo survived pruning: %#v", name, v)
			}
			if network["keep"] != 1 {
				t.Errorf("networks[%s].keep = %#v, want 1", name, network["keep"])
			}
		})
	}
}

func TestRemoveKeyQuotedDottedNodes(t *testing.T) {
	m := &mergeBuilderImpl{}
	data := map[string]interface{}{
		"networks": []interface{}{
			map[string]interface{}{"name": "a.b.c", "foo": 1, "keep": 2},
		},
		"meta": map[string]interface{}{"x.y": 1, "other": 2},
		"a.b":  1,
		"c":    2,
	}

	m.removeKey(data, "networks.[a.b.c].foo")
	m.removeKey(data, "meta.[x.y]")
	m.removeKey(data, "[a.b]")

	want := map[string]interface{}{
		"networks": []interface{}{
			map[string]interface{}{"name": "a.b.c", "keep": 2},
		},
		"meta": map[string]interface{}{"other": 2},
		"c":    2,
	}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("after removeKey:\n got %#v\nwant %#v", data, want)
	}
}

func TestDocumentPruneQuotedDottedNodes(t *testing.T) {
	doc := NewDocument(map[string]interface{}{
		"networks": []interface{}{
			map[string]interface{}{"name": "a.b.c", "foo": 1, "keep": 2},
		},
		"meta": map[string]interface{}{"x.y": 1, "other": 2},
		"a.b":  1,
		"c":    2,
	})

	pruned := doc.Prune("networks.[a.b.c].foo", "meta.[x.y]", "[a.b]")

	want := map[string]interface{}{
		"networks": []interface{}{
			map[string]interface{}{"name": "a.b.c", "keep": 2},
		},
		"meta": map[string]interface{}{"other": 2},
		"c":    2,
	}
	if got := pruned.RawData(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after Prune:\n got %#v\nwant %#v", got, want)
	}
}

func TestMergePruneMarkerUnderDottedNames(t *testing.T) {
	const base = `networks:
- name: %[1]s
  foo: bar
  keep: 1
meta:
  %[1]s: 1
  other: 2
`
	const overlay = `networks:
- name: %[1]s
  foo: (( prune ))
meta:
  %[1]s: (( prune ))
`

	for _, name := range dottedNames {
		t.Run(name, func(t *testing.T) {
			data := mergeYAMLData(t, fmt.Sprintf(base, name), fmt.Sprintf(overlay, name))

			network := firstEntry(t, data, "networks")
			if v, present := network["foo"]; present {
				t.Errorf("networks[%s].foo survived pruning: %#v", name, v)
			}
			if network["keep"] != 1 {
				t.Errorf("networks[%s].keep = %#v, want 1", name, network["keep"])
			}

			meta, ok := data["meta"].(map[string]interface{})
			if !ok {
				t.Fatalf("meta is not a map: %#v", data["meta"])
			}
			if v, present := meta[name]; present {
				t.Errorf("meta[%s] survived pruning: %#v", name, v)
			}
			if meta["other"] != 2 {
				t.Errorf("meta.other = %#v, want 2", meta["other"])
			}
		})
	}
}

func TestMergeSortMarkerUnderDottedNames(t *testing.T) {
	const base = `groups:
- name: %[1]s
  list: [c, a, b]
meta:
  %[1]s: [z, y]
`
	const overlay = `groups:
- name: %[1]s
  list: (( sort ))
meta:
  %[1]s: (( sort ))
`

	for _, name := range dottedNames {
		t.Run(name, func(t *testing.T) {
			data := mergeYAMLData(t, fmt.Sprintf(base, name), fmt.Sprintf(overlay, name))

			group := firstEntry(t, data, "groups")
			if got, want := group["list"], []interface{}{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
				t.Errorf("groups[%s].list = %#v, want %#v", name, got, want)
			}

			meta, ok := data["meta"].(map[string]interface{})
			if !ok {
				t.Fatalf("meta is not a map: %#v", data["meta"])
			}
			if got, want := meta[name], []interface{}{"y", "z"}; !reflect.DeepEqual(got, want) {
				t.Errorf("meta[%s] = %#v, want %#v", name, got, want)
			}
		})
	}
}
