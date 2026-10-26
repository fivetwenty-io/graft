package yq

import (
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

var _ runtime.KeyExpander = (*Dialect)(nil)

// ExpandKey is a quoted map key with a "*" or "?" in it read as a glob
// over the map's keys, where "*" matches any run of characters and "?"
// any one. It returns the keys that match, and none for a key without a
// wildcard, for anything but a map, or when no key matches, so the key
// then reads as it is written.
func (*Dialect) ExpandKey(c, key *value.Value) []*value.Value {
	if c.Kind() != value.KindObject || key.Kind() != value.KindString || !strings.ContainsAny(key.Str(), "*?") {
		return nil
	}
	pattern := []rune(key.Str())
	var keys []*value.Value
	for _, e := range c.Entries() {
		text := numberOrText(e.Key)
		if !globMatch(pattern, []rune(text)) {
			continue
		}
		k := e.Key
		if k.Kind() != value.KindString {
			k = value.Str(text)
		}
		keys = append(keys, k)
	}
	return keys
}

// globMatch reports whether name matches pattern in full.
func globMatch(pattern, name []rune) bool {
	px, nx := 0, 0
	star, mark := -1, 0
	for nx < len(name) {
		switch {
		case px < len(pattern) && pattern[px] == '*':
			star, mark = px, nx
			px++
		case px < len(pattern) && (pattern[px] == '?' || pattern[px] == name[nx]):
			px++
			nx++
		case star >= 0:
			mark++
			px, nx = star+1, mark
		default:
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}
