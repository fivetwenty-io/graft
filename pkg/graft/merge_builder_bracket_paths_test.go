package graft

import (
	"context"
	"fmt"
	"testing"
)

func bracketPathDoc() Document {
	return NewDocument(map[string]interface{}{
		"list": []interface{}{"a", "b", "c"},
		"jobs": []interface{}{
			map[string]interface{}{"name": "a", "k": 1},
			map[string]interface{}{"name": "b", "k": 2},
		},
	})
}

func runBracketPaths(t *testing.T, prune, pick []string) (string, error) {
	t.Helper()
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	b := engine.Merge(context.Background(), bracketPathDoc())
	if len(prune) > 0 {
		b = b.WithPrune(prune...)
	}
	if len(pick) > 0 {
		b = b.WithCherryPick(pick...)
	}
	result, err := b.Execute()
	if err != nil {
		return "", err
	}
	return fmt.Sprint(result.RawData()), nil
}

const (
	bpList = "[a b c]"
	bpJobs = "[map[k:1 name:a] map[k:2 name:b]]"
)

// TestPruneAcceptsBracketIndexes pins that --prune reads list[0] like
// list.0, the way spruce's ParseCursor does. The expected values are
// what spruce v1.35.17 prints for the same flags.
func TestPruneAcceptsBracketIndexes(t *testing.T) {
	cases := []struct{ path, want string }{
		{"list[0]", "map[jobs:" + bpJobs + " list:[b c]]"},
		{"list.0", "map[jobs:" + bpJobs + " list:[b c]]"},
		{"jobs[1]", "map[jobs:[map[k:1 name:a]] list:" + bpList + "]"},
		{"jobs[0].k", "map[jobs:[map[name:a] map[k:2 name:b]] list:" + bpList + "]"},
		{"jobs.[0].k", "map[jobs:[map[name:a] map[k:2 name:b]] list:" + bpList + "]"},
		{"$.list[0]", "map[jobs:" + bpJobs + " list:[b c]]"},
		{"list[5]", "map[jobs:" + bpJobs + " list:" + bpList + "]"},
		{"list[-1]", "map[jobs:" + bpJobs + " list:" + bpList + "]"},
		// spruce prunes nothing for a field=value selector.
		{"jobs[name=b]", "map[jobs:" + bpJobs + " list:" + bpList + "]"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, err := runBracketPaths(t, []string{tc.path}, nil)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// TestCherryPickAcceptsBracketIndexes pins that --cherry-pick reads
// list[0] like list.0, and builds a list, not a map keyed "0", for an
// index followed by more path. The expected values are what spruce
// v1.35.17 prints for the same flags.
func TestCherryPickAcceptsBracketIndexes(t *testing.T) {
	cases := []struct {
		name  string
		picks []string
		want  string
	}{
		{"list[0]", []string{"list[0]"}, "map[list:[a]]"},
		{"jobs[1]", []string{"jobs[1]"}, "map[jobs:[map[k:2 name:b]]]"},
		{"jobs[b]", []string{"jobs[b]"}, "map[jobs:[map[k:2 name:b]]]"},
		{"jobs[0].k", []string{"jobs[0].k"}, "map[jobs:[map[k:1]]]"},
		{"jobs.0.k", []string{"jobs.0.k"}, "map[jobs:[map[k:1]]]"},
		{"jobs.b.k", []string{"jobs.b.k"}, "map[jobs:[map[k:2]]]"},
		{"jobs.1.k and list.2", []string{"jobs.1.k", "list.2"}, "map[jobs:[map[k:2]] list:[c]]"},
		{"jobs.1.k then jobs.0.k", []string{"jobs.1.k", "jobs.0.k"}, "map[jobs:[map[k:1] map[k:2]]]"},
		{"jobs.0.k then jobs.1.k", []string{"jobs.0.k", "jobs.1.k"}, "map[jobs:[map[k:2] map[k:1]]]"},
		{"jobs.1 then jobs.0.k", []string{"jobs.1", "jobs.0.k"}, "map[jobs:[map[k:1] map[k:2 name:b]]]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runBracketPaths(t, nil, tc.picks)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// TestCherryPickFieldSelectorStillFails pins that jobs[name=b], which
// spruce also cannot resolve, keeps failing.
func TestCherryPickFieldSelectorStillFails(t *testing.T) {
	if _, err := runBracketPaths(t, nil, []string{"jobs[name=b]"}); err == nil {
		t.Fatal("jobs[name=b] resolved, want a failure as before")
	}
}
