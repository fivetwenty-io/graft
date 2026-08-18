// Package histdiff provides a shared semantic-diff primitive used by the
// graft CLI's diff-rendering flags (graft diff --changes/--unified/
// --side-by-side) and by merge history tracking (graft merge --history/
// --trace-path/--show-changes/--changes-only). It is deliberately internal:
// it is presentation/tracking plumbing for the CLI, not public library API.
//
// Compare is built on top of github.com/homeport/dyff, the same engine
// graft diff's default output uses, so both codepaths agree on what counts
// as a semantic change (key order independence, type-aware comparison,
// keyed-list identification, etc.) instead of maintaining a second diff
// algorithm.
package histdiff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	yamlv3 "go.yaml.in/yaml/v3"
)

// Kind classifies the nature of a single semantic change.
type Kind int

const (
	// Modified means the value at Path changed from Old to New.
	Modified Kind = iota
	// Added means Path exists only in the "to" document (Old is nil).
	Added
	// Removed means Path exists only in the "from" document (New is nil).
	Removed
)

// String renders Kind as the label used by change-list renderers.
func (k Kind) String() string {
	switch k {
	case Added:
		return "ADDED"
	case Removed:
		return "REMOVED"
	case Modified:
		return "MODIFIED"
	default:
		return "UNKNOWN"
	}
}

// Change is one semantic difference between two document states, at a
// single dot-style path (e.g. "database.host").
type Change struct {
	Path string
	Kind Kind
	Old  interface{}
	New  interface{}
}

// Compare returns the semantic changes between from and to (each normally a
// map[string]interface{} document tree, though any value dyff/ytbx can
// represent as a YAML document root works), sorted by Path. fromLabel and
// toLabel are used only as the dyff report's document locations (visible in
// error messages), not in the returned Change values.
//
// Compare marshals from/to to YAML text via graft.MarshalYAML-compatible
// encoding and re-parses them as go.yaml.in/yaml/v3 nodes, the representation
// dyff.CompareInputFiles requires; a marshal or parse failure (e.g. a value
// containing a Go type YAML cannot represent, or a document root dyff
// cannot walk) is returned as an error rather than silently dropped.
func Compare(fromLabel string, from interface{}, toLabel string, to interface{}) ([]Change, error) {
	fromNode, err := toYAMLNode(from)
	if err != nil {
		return nil, fmt.Errorf("histdiff: encoding %s: %w", fromLabel, err)
	}
	toNode, err := toYAMLNode(to)
	if err != nil {
		return nil, fmt.Errorf("histdiff: encoding %s: %w", toLabel, err)
	}

	report, err := dyff.CompareInputFiles(
		ytbx.InputFile{Location: fromLabel, Documents: []*yamlv3.Node{fromNode}},
		ytbx.InputFile{Location: toLabel, Documents: []*yamlv3.Node{toNode}},
	)
	if err != nil {
		return nil, fmt.Errorf("histdiff: comparing %s to %s: %w", fromLabel, toLabel, err)
	}

	changes := make([]Change, 0, len(report.Diffs))
	for _, diff := range report.Diffs {
		path := ""
		if diff.Path != nil {
			path = diff.Path.ToDotStyle()
		}
		for _, detail := range diff.Details {
			detailChanges, detailErr := detailToChanges(path, detail)
			if detailErr != nil {
				return nil, fmt.Errorf("histdiff: decoding change at %q: %w", path, detailErr)
			}
			changes = append(changes, detailChanges...)
		}
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// detailToChanges converts one dyff.Detail into zero or more Changes.
//
// dyff reports an ADDITION/REMOVAL detail at the *parent* container's path,
// with detail.To/detail.From holding a YAML fragment of the added/removed
// entries themselves (a MappingNode with the child key(s) still attached, a
// SequenceNode with the child element(s), or - for multi-document input,
// which this package never produces - a DocumentNode). detailToChanges
// expands that fragment into one Change per immediate child (path =
// parent.child), each carrying the child's value as a whole (nested
// grandchildren are not flattened further, matching the one-level
// Added/Removed convention pkg/graft/diff.go's own Diff already uses).
//
// MODIFICATION and ORDERCHANGE details are already leaf-specific (path *is*
// the changed value's own path), so they map to a single Change unchanged;
// ORDERCHANGE (list reordering with no value change) is reported as
// Modified so callers that only care about "did this path change" don't
// need a separate case - Old/New both carry the reordered list, so no
// information is lost.
func detailToChanges(path string, detail dyff.Detail) ([]Change, error) {
	switch detail.Kind {
	case dyff.ADDITION:
		return fragmentToChanges(path, Added, detail.To)
	case dyff.REMOVAL:
		return fragmentToChanges(path, Removed, detail.From)
	case dyff.MODIFICATION:
		oldVal, err := decodeNodeIfSet(detail.From)
		if err != nil {
			return nil, err
		}
		newVal, err := decodeNodeIfSet(detail.To)
		if err != nil {
			return nil, err
		}
		return []Change{{Path: path, Kind: Modified, Old: oldVal, New: newVal}}, nil
	case dyff.ORDERCHANGE:
		oldVal, err := decodeNodeIfSet(detail.From)
		if err != nil {
			return nil, err
		}
		newVal, err := decodeNodeIfSet(detail.To)
		if err != nil {
			return nil, err
		}
		return []Change{{Path: path, Kind: Modified, Old: oldVal, New: newVal}}, nil
	default:
		// A detail.Kind dyff doesn't currently emit; nothing to report
		// rather than erroring the whole comparison over it.
		return nil, nil
	}
}

// fragmentToChanges expands an ADDITION/REMOVAL fragment node (see
// detailToChanges) into one Change per immediate child entry, joined onto
// parentPath. A nil fragment (shouldn't happen for ADDITION/REMOVAL, which
// always carry a non-nil To/From) yields no changes.
func fragmentToChanges(parentPath string, kind Kind, fragment *yamlv3.Node) ([]Change, error) {
	if fragment == nil {
		return nil, nil
	}

	joinPath := func(child string) string {
		if parentPath == "" {
			return child
		}
		return parentPath + "." + child
	}

	switch fragment.Kind {
	case yamlv3.MappingNode:
		changes := make([]Change, 0, len(fragment.Content)/2)
		for i := 0; i+1 < len(fragment.Content); i += 2 {
			keyNode, valueNode := fragment.Content[i], fragment.Content[i+1]
			val, err := decodeNode(valueNode)
			if err != nil {
				return nil, err
			}
			change := Change{Path: joinPath(keyNode.Value), Kind: kind}
			if kind == Added {
				change.New = val
			} else {
				change.Old = val
			}
			changes = append(changes, change)
		}
		return changes, nil

	case yamlv3.SequenceNode:
		changes := make([]Change, 0, len(fragment.Content))
		for i, itemNode := range fragment.Content {
			val, err := decodeNode(itemNode)
			if err != nil {
				return nil, err
			}
			change := Change{Path: fmt.Sprintf("%s[%d]", parentPath, i), Kind: kind}
			if kind == Added {
				change.New = val
			} else {
				change.Old = val
			}
			changes = append(changes, change)
		}
		return changes, nil

	default:
		// DocumentNode (multi-document input, never produced by this
		// package's single-document Compare) or a scalar fragment
		// (shouldn't occur for ADDITION/REMOVAL, which dyff only emits for
		// container-level changes): report the whole fragment as one
		// change at the parent path rather than dropping it silently.
		val, err := decodeNode(fragment)
		if err != nil {
			return nil, err
		}
		change := Change{Path: parentPath, Kind: kind}
		if kind == Added {
			change.New = val
		} else {
			change.Old = val
		}
		return []Change{change}, nil
	}
}

func decodeNodeIfSet(node *yamlv3.Node) (interface{}, error) {
	if node == nil {
		return nil, nil
	}
	return decodeNode(node)
}

func decodeNode(node *yamlv3.Node) (interface{}, error) {
	var v interface{}
	if err := node.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// toYAMLNode marshals v to YAML text (via go.yaml.in/yaml/v3, matching the
// representation ytbx/dyff already work in) and parses it back into a
// document root node suitable for ytbx.InputFile.Documents.
//
// yamlv3.Marshal panics (rather than returning an error) for a small set of
// genuinely unsupported Go types it has no YAML representation for at all
// (e.g. chan, func: see encode.go's marshal default case, "cannot marshal
// type: ..."); merge document trees are normally built from parsed
// YAML/JSON and never contain such values, but Compare's callers construct
// values programmatically (e.g. internal/history diffing intermediate
// merge snapshots), so a defensive recover converts that panic into a
// regular error instead of crashing the whole CLI.
func toYAMLNode(v interface{}) (node *yamlv3.Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("marshaling value to YAML: %v", r)
		}
	}()

	raw, err := yamlv3.Marshal(v)
	if err != nil {
		return nil, err
	}

	var doc yamlv3.Node
	if err := yamlv3.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	// Keep the DocumentNode wrapper Unmarshal produces (do not unwrap to
	// the inner mapping/sequence/scalar node): it matches what ytbx's own
	// file loader puts in InputFile.Documents (ytbx.LoadYAMLDocuments
	// decodes each document with yaml.Decoder.Decode(&node), which also
	// yields a DocumentNode), and dyff.CompareInputFiles's
	// isEmptyDocument/Kubernetes-entity-detection logic specifically
	// switches on Kind == DocumentNode before indexing Content[0] -
	// passing an unwrapped node (e.g. an empty map's bare MappingNode,
	// Content length 0) skips that guard and panics inside dyff on
	// Content[0].
	return &doc, nil
}

// TopLevelPaths returns the sorted, de-duplicated set of top-level path
// segments (the portion of each Change.Path before the first ".") touched
// by changes. Used by renderers (e.g. graft diff --unified) that group
// output by top-level key.
func TopLevelPaths(changes []Change) []string {
	seen := make(map[string]bool)
	var order []string
	for _, c := range changes {
		top := c.Path
		if idx := strings.IndexByte(top, '.'); idx >= 0 {
			top = top[:idx]
		}
		if top == "" {
			continue
		}
		if !seen[top] {
			seen[top] = true
			order = append(order, top)
		}
	}
	sort.Strings(order)
	return order
}

// Counts tallies changes by Kind, for summary headers like
// "Changes (2 modified, 1 added, 1 removed):".
type Counts struct {
	Modified int
	Added    int
	Removed  int
}

// CountChanges tallies changes by Kind.
func CountChanges(changes []Change) Counts {
	var c Counts
	for _, ch := range changes {
		switch ch.Kind {
		case Added:
			c.Added++
		case Removed:
			c.Removed++
		case Modified:
			c.Modified++
		}
	}
	return c
}
