package graft

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func rootPathDoc() Document {
	return NewDocument(map[string]interface{}{
		"meta":  map[string]interface{}{"a": 1, "b": 2},
		"other": 2,
		"list": []interface{}{
			map[string]interface{}{"name": "x", "v": 1},
			map[string]interface{}{"name": "z", "v": 2},
		},
	})
}

// TestMergeBuilderPruneAcceptsRootPath pins that a prune path written
// with a leading "$." names the same key as the path without it, as
// spruce reads "--prune $.meta". A "$." with nothing after it names no
// key, so it removes nothing, which is also what spruce does.
func TestMergeBuilderPruneAcceptsRootPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"$.meta", "map[list:[map[name:x v:1] map[name:z v:2]] other:2]"},
		{"$.meta.a", "map[list:[map[name:x v:1] map[name:z v:2]] meta:map[b:2] other:2]"},
		{"$.list.1", "map[list:[map[name:x v:1]] meta:map[a:1 b:2] other:2]"},
		{"$.", "map[list:[map[name:x v:1] map[name:z v:2]] meta:map[a:1 b:2] other:2]"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			engine, err := NewEngine()
			if err != nil {
				t.Fatalf("NewEngine: %v", err)
			}
			result, err := engine.Merge(context.Background(), rootPathDoc()).WithPrune(tc.path).Execute()
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got := fmt.Sprint(result.RawData()); got != tc.want {
				t.Fatalf("result = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestMergeBuilderCherryPickAcceptsRootPath pins that a cherry-pick
// path written with a leading "$." keeps the same key as the path
// without it, as spruce reads "--cherry-pick $.meta". A "$." with
// nothing after it names no key, so it fails as an empty path would.
func TestMergeBuilderCherryPickAcceptsRootPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"$.meta", "map[meta:map[a:1 b:2]]"},
		{"$.meta.a", "map[meta:map[a:1]]"},
		{"$.list.1", "map[list:[map[name:z v:2]]]"},
		{"$.list.x", "map[list:[map[name:x v:1]]]"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			engine, err := NewEngine()
			if err != nil {
				t.Fatalf("NewEngine: %v", err)
			}
			result, err := engine.Merge(context.Background(), rootPathDoc()).WithCherryPick(tc.path).Execute()
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got := fmt.Sprint(result.RawData()); got != tc.want {
				t.Fatalf("result = %s, want %s", got, tc.want)
			}
		})
	}

	t.Run("$. alone", func(t *testing.T) {
		engine, err := NewEngine()
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		_, err = engine.Merge(context.Background(), rootPathDoc()).WithCherryPick("$.").Execute()
		if err == nil || !strings.Contains(err.Error(), "empty key path") {
			t.Fatalf("err = %v, want an empty key path error", err)
		}
	})
}
