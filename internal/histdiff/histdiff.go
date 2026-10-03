// Package histdiff provides a shared semantic-diff primitive used by the
// graft CLI's diff-rendering flags (graft diff --changes/--unified/
// --side-by-side) and by merge history tracking (graft merge --history/
// --trace-path/--show-changes/--changes-only). It is deliberately internal:
// it is presentation/tracking plumbing for the CLI, not public library API.
//
// Compare is built on internal/yamldiff, graft's port of the dyff engine
// behind graft diff's default report, so both code paths agree on what
// counts as a semantic change (key order independence, type-aware
// comparison, keyed-list identification, etc.) instead of maintaining a
// second diff algorithm.
package histdiff

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/fivetwenty-io/graft/internal/yamldiff"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
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

	// Document is the one-based position of the document the change is
	// in. It is set only when an input holds more than one document, and
	// is zero otherwise, as it is for every change Compare returns.
	Document int
}

// Compare returns the semantic changes between from and to (each normally a
// map[string]interface{} document tree, though any value yamldiff can
// represent as a YAML document root works), sorted by Path. fromLabel and
// toLabel are used only as yamldiff's document locations (visible in
// error messages), not in the returned Change values.
//
// Compare builds from and to into the YAML node trees yaml.v3 would
// produce by marshaling them and parsing the text back (see
// yamlnode.FromValue), the representation the comparison engine works
// in. A value containing a Go type YAML cannot represent is returned as
// an error rather than silently dropped.
func Compare(fromLabel string, from interface{}, toLabel string, to interface{}) ([]Change, error) {
	fromNode, err := toYAMLNode(from)
	if err != nil {
		return nil, fmt.Errorf("histdiff: encoding %s: %w", fromLabel, err)
	}
	toNode, err := toYAMLNode(to)
	if err != nil {
		return nil, fmt.Errorf("histdiff: encoding %s: %w", toLabel, err)
	}

	report, err := yamldiff.CompareInputFiles(
		yamldiff.InputFile{Location: fromLabel, Documents: []*yamlnode.Node{fromNode}},
		yamldiff.InputFile{Location: toLabel, Documents: []*yamlnode.Node{toNode}},
	)
	if err != nil {
		return nil, fmt.Errorf("histdiff: comparing %s to %s: %w", fromLabel, toLabel, err)
	}

	changes, err := diffsToChanges(report.Diffs, 0)
	if err != nil {
		return nil, err
	}

	sortChanges(changes)
	return changes, nil
}

// DocumentChanges holds the changes found in one document pair, with the
// decoded documents they came from.
type DocumentChanges struct {
	// Document is the one-based position of the document in its input.
	// A document that only the "to" input has is numbered by that input.
	Document int
	// From and To are the decoded documents, nil on a side that has no
	// document.
	From, To interface{}
	Changes  []Change
}

// CompareDocumentPairs pairs the documents of from and to the way graft's
// default diff does (yamldiff.PairDocuments), compares each pair, and
// returns the pairs that differ. Each pair is compared on the node trees
// yaml.v3 would produce by marshaling the decoded documents, as Compare
// does, so a single-document comparison finds exactly the changes Compare
// finds. A change in the order of Kubernetes documents is not a change
// here. The error for inputs that cannot be paired is yamldiff's own,
// with the text the default diff prints.
//
// A change is numbered with its document only when from or to holds more
// than one document.
func CompareDocumentPairs(fromLabel string, from []interface{}, toLabel string, to []interface{}) ([]DocumentChanges, error) {
	fromFile, err := encodeInputFile(fromLabel, from)
	if err != nil {
		return nil, err
	}
	toFile, err := encodeInputFile(toLabel, to)
	if err != nil {
		return nil, err
	}

	pairing, err := yamldiff.PairDocuments(fromFile, toFile)
	if err != nil {
		return nil, err
	}
	groups, err := pairing.Compare()
	if err != nil {
		return nil, err
	}

	numbered := len(from) > 1 || len(to) > 1
	var result []DocumentChanges
	for _, group := range groups {
		document := documentChanges(group.Pair, from, to)
		changeDocument := 0
		if numbered {
			changeDocument = document.Document
		}
		changes, err := diffsToChanges(group.Diffs, changeDocument)
		if err != nil {
			return nil, err
		}
		if len(changes) == 0 {
			continue
		}
		sortChanges(changes)
		document.Changes = changes
		result = append(result, document)
	}

	return result, nil
}

// CompareDocuments returns the changes between the documents of from and
// to, found as CompareDocumentPairs finds them, sorted by document and
// then by path.
func CompareDocuments(fromLabel string, from []interface{}, toLabel string, to []interface{}) ([]Change, error) {
	documents, err := CompareDocumentPairs(fromLabel, from, toLabel, to)
	if err != nil {
		return nil, err
	}

	return AllChanges(documents), nil
}

// AllChanges returns the changes of every document, sorted by document
// and then by path.
func AllChanges(documents []DocumentChanges) []Change {
	var changes []Change
	for _, document := range documents {
		changes = append(changes, document.Changes...)
	}
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Document != changes[j].Document {
			return changes[i].Document < changes[j].Document
		}
		return pathLess(changes[i].Path, changes[j].Path)
	})

	return changes
}

// encodeInputFile builds the yamldiff input for decoded documents.
func encodeInputFile(label string, documents []interface{}) (yamldiff.InputFile, error) {
	file := yamldiff.InputFile{Location: label}
	for _, document := range documents {
		node, err := toYAMLNode(document)
		if err != nil {
			return yamldiff.InputFile{}, fmt.Errorf("histdiff: encoding %s: %w", label, err)
		}
		file.Documents = append(file.Documents, node)
	}

	return file, nil
}

// documentChanges returns the decoded documents of pair, with the number
// of its document.
func documentChanges(pair yamldiff.DocumentPair, from, to []interface{}) DocumentChanges {
	result := DocumentChanges{Document: documentNumber(pair)}
	if pair.FromPosition >= 0 {
		result.From = from[pair.FromPosition]
	}
	if pair.ToPosition >= 0 {
		result.To = to[pair.ToPosition]
	}

	return result
}

// documentNumber returns the one-based position of a pair's document,
// taken from the "from" input unless only the "to" input has the
// document.
func documentNumber(pair yamldiff.DocumentPair) int {
	if pair.FromPosition >= 0 {
		return pair.FromPosition + 1
	}

	return pair.ToPosition + 1
}

// diffsToChanges converts yamldiff diffs into changes, numbering each
// with document.
func diffsToChanges(diffs []yamldiff.Diff, document int) ([]Change, error) {
	changes := make([]Change, 0, len(diffs))
	for _, diff := range diffs {
		path := ""
		if diff.Path != nil {
			path = diff.Path.ToDotStyle()
		}
		for _, detail := range diff.Details {
			detailChanges, err := detailToChanges(path, detail)
			if err != nil {
				return nil, fmt.Errorf("histdiff: decoding change at %q: %w", path, err)
			}
			for i := range detailChanges {
				detailChanges[i].Document = document
			}
			changes = append(changes, detailChanges...)
		}
	}

	return changes, nil
}

// sortChanges orders changes by path, keeping the order yamldiff reported
// for changes at the same path. A list index sorts by its number, so
// l[2] comes before l[10].
func sortChanges(changes []Change) {
	sort.SliceStable(changes, func(i, j int) bool { return pathLess(changes[i].Path, changes[j].Path) })
}

// pathLess compares two paths byte by byte, except that a run of digits
// right after a "[" in both paths compares as a number.
func pathLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == '[' && b[j] == '[' {
			aEnd, bEnd := digitsEnd(a, i+1), digitsEnd(b, j+1)
			if aEnd > i+1 && bEnd > j+1 {
				aNum, bNum := a[i+1:aEnd], b[j+1:bEnd]
				if len(aNum) != len(bNum) {
					return len(aNum) < len(bNum)
				}
				if aNum != bNum {
					return aNum < bNum
				}
				i, j = aEnd, bEnd
				continue
			}
		}
		if a[i] != b[j] {
			return a[i] < b[j]
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

// digitsEnd returns the index just past the run of ASCII digits that
// starts at s[from].
func digitsEnd(s string, from int) int {
	end := from
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return end
}

// detailToChanges converts one yamldiff.Detail into zero or more Changes.
//
// yamldiff reports an ADDITION/REMOVAL detail at the *parent* container's path,
// with detail.To/detail.From holding a YAML fragment of the added/removed
// entries themselves (a MappingNode with the child key(s) still attached, a
// SequenceNode with the child element(s), or - for a Kubernetes document
// that only one input has - a DocumentNode). detailToChanges
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
func detailToChanges(path string, detail yamldiff.Detail) ([]Change, error) {
	switch detail.Kind {
	case yamldiff.ADDITION:
		return fragmentToChanges(path, Added, detail.To, detail.Indexes)
	case yamldiff.REMOVAL:
		return fragmentToChanges(path, Removed, detail.From, detail.Indexes)
	case yamldiff.MODIFICATION:
		oldVal, err := decodeNodeIfSet(detail.From)
		if err != nil {
			return nil, err
		}
		newVal, err := decodeNodeIfSet(detail.To)
		if err != nil {
			return nil, err
		}
		return []Change{{Path: path, Kind: Modified, Old: oldVal, New: newVal}}, nil
	case yamldiff.ORDERCHANGE:
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
		// A detail.Kind yamldiff doesn't currently emit; nothing to report
		// rather than erroring the whole comparison over it.
		return nil, nil
	}
}

// fragmentToChanges expands an ADDITION/REMOVAL fragment node (see
// detailToChanges) into one Change per immediate child entry, joined onto
// parentPath. A sequence fragment's entries take their list positions
// from indexes, which yamldiff records because the fragment itself has
// lost them. A nil fragment (shouldn't happen for ADDITION/REMOVAL, which
// always carry a non-nil To/From) yields no changes.
func fragmentToChanges(parentPath string, kind Kind, fragment *yamlnode.Node, indexes []int) ([]Change, error) {
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
	case yamlnode.MappingNode:
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

	case yamlnode.SequenceNode:
		changes := make([]Change, 0, len(fragment.Content))
		for i, itemNode := range fragment.Content {
			val, err := decodeNode(itemNode)
			if err != nil {
				return nil, err
			}
			change := Change{Path: fmt.Sprintf("%s[%d]", parentPath, listIndex(indexes, i)), Kind: kind}
			if kind == Added {
				change.New = val
			} else {
				change.Old = val
			}
			changes = append(changes, change)
		}
		return changes, nil

	default:
		// DocumentNode (a Kubernetes document only one input has) or a scalar fragment
		// (shouldn't occur for ADDITION/REMOVAL, which yamldiff only emits for
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

// listIndex returns the list position of fragment entry i. A fragment
// without recorded positions numbers its entries from zero.
func listIndex(indexes []int, i int) int {
	if i < len(indexes) {
		return indexes[i]
	}
	return i
}

func decodeNodeIfSet(node *yamlnode.Node) (interface{}, error) {
	if node == nil {
		return nil, nil
	}
	return decodeNode(node)
}

func decodeNode(node *yamlnode.Node) (interface{}, error) {
	return yamlnode.Decode(node)
}

// toYAMLNode returns the DocumentNode yaml.v3 would produce for v by
// marshaling it and parsing the text back, the shape yamldiff.InputFile
// documents take. yaml.v3 panics on chan, func, and complex values, and
// the dyff-backed version recovered that panic as "marshaling value to
// YAML: …", so an unsupported type keeps that prefix. Any other error
// passes through unwrapped as it always did, which covers a failing
// MarshalText and the errors for struct-tag problems such as a duplicate
// yaml key, a bad flag, or a bad inline. Compare's callers build values
// in code, for example in internal/history, so a panic inside the encoder
// is still recovered and returned as an error with the same prefix
// instead of crashing the CLI.
func toYAMLNode(v interface{}) (doc *yamlnode.Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc, err = nil, fmt.Errorf("marshaling value to YAML: %v", r)
		}
	}()
	doc, err = yamlnode.FromValue(v)
	var unsupported *yamlnode.UnsupportedTypeError
	if errors.As(err, &unsupported) {
		return nil, fmt.Errorf("marshaling value to YAML: %w", err)
	}
	return doc, err
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
