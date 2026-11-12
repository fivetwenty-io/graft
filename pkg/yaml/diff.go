package yaml

import (
	"bytes"
	"errors"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/cursorpath"
	"github.com/fivetwenty-io/graft/internal/yamldiff"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// DiffOptions changes how Diff compares. The zero value compares the way
// graft diff does.
type DiffOptions struct {
	// IgnoreOrderChanges leaves out changes that only reorder a list or
	// the documents of a stream.
	IgnoreOrderChanges bool
}

// ChangeKind tells what a Change did to the value at its path.
type ChangeKind int

const (
	// ChangeAdded means entries were added to the collection at Path.
	ChangeAdded ChangeKind = iota + 1
	// ChangeRemoved means entries were removed from the collection at Path.
	ChangeRemoved
	// ChangeModified means the value at Path changed.
	ChangeModified
	// ChangeOrder means the entries at Path, or the documents of the
	// stream, were reordered.
	ChangeOrder
)

// String returns "added", "removed", "modified", or "order".
func (k ChangeKind) String() string {
	switch k {
	case ChangeAdded:
		return "added"
	case ChangeRemoved:
		return "removed"
	case ChangeModified:
		return "modified"
	case ChangeOrder:
		return "order"
	}
	return "ChangeKind(" + strconv.Itoa(int(k)) + ")"
}

// Change is one difference. For an addition or a removal, Path names the
// collection that gained or lost entries, and To or From holds them. For
// a modification, Path names the value that changed. Document is the
// document's index in the stream, or -1 for a change in document order.
type Change struct {
	Document int
	Path     string
	Kind     ChangeKind
	From     *Node
	To       *Node
}

// DiffReport is the result of Diff.
type DiffReport struct {
	changes []Change
	report  yamldiff.Report
}

// Changes returns a copy of the changes, in report order.
func (r *DiffReport) Changes() []Change {
	return append([]Change(nil), r.changes...)
}

// Empty reports whether the inputs have no differences.
func (r *DiffReport) Empty() bool {
	return len(r.changes) == 0
}

// DiffError is a pair of inputs that Diff can't compare. Reason is fixed
// text and never quotes the input.
type DiffError struct {
	Reason string
	err    error
}

// Error returns "yaml: diff: " and the reason.
func (e *DiffError) Error() string {
	return "yaml: diff: " + e.Reason
}

// Unwrap returns the error from the comparison.
func (e *DiffError) Unwrap() error {
	return e.err
}

// Diff compares two YAML or JSON inputs, each of which may hold several
// documents, with the engine graft diff uses. It never reads TOML, which
// graft diff reads only when it opens a file. TOML that starts with a
// table header fails to parse, and TOML made of key = value lines reads
// as one YAML string. A parse failure comes back as a *DecodeError, and
// inputs it can't compare come back as a *DiffError.
func Diff(a, b []byte, opts *DiffOptions) (*DiffReport, error) {
	fromDocs, err := yamldiff.ParseDocuments(a)
	if err != nil {
		return nil, translate(err)
	}
	toDocs, err := yamldiff.ParseDocuments(b)
	if err != nil {
		return nil, translate(err)
	}

	report, err := yamldiff.CompareInputFiles(
		yamldiff.InputFile{Documents: fromDocs},
		yamldiff.InputFile{Documents: toDocs},
	)
	if err != nil {
		reason := "the inputs could not be compared"
		if errors.Is(err, yamldiff.ErrDocumentCount) {
			reason = "the inputs hold different numbers of documents"
		}
		return nil, &DiffError{Reason: reason, err: err}
	}

	if opts != nil && opts.IgnoreOrderChanges {
		report.Diffs = withoutOrderChanges(report.Diffs)
	}

	from, to := newDiffConverter(a), newDiffConverter(b)
	var changes []Change
	for _, d := range report.Diffs {
		document, path := -1, ""
		if d.Path != nil {
			document, path = d.Path.DocumentIdx, diffPath(d.Path.PathElements)
		}
		for _, detail := range d.Details {
			changes = append(changes, Change{
				Document: document,
				Path:     path,
				Kind:     changeKind(detail.Kind),
				From:     from.node(detail.From),
				To:       to.node(detail.To),
			})
		}
	}
	return &DiffReport{changes: changes, report: report}, nil
}

// withoutOrderChanges returns diffs without any order change, and without
// any diff that held nothing else. It leaves diffs itself unchanged.
func withoutOrderChanges(diffs []yamldiff.Diff) []yamldiff.Diff {
	var kept []yamldiff.Diff
	for _, d := range diffs {
		var details []yamldiff.Detail
		for _, detail := range d.Details {
			if detail.Kind != yamldiff.ORDERCHANGE {
				details = append(details, detail)
			}
		}
		if len(details) > 0 {
			kept = append(kept, yamldiff.Diff{Path: d.Path, Details: details})
		}
	}
	return kept
}

func changeKind(k yamldiff.DetailKind) ChangeKind {
	switch k {
	case yamldiff.ADDITION:
		return ChangeAdded
	case yamldiff.REMOVAL:
		return ChangeRemoved
	case yamldiff.ORDERCHANGE:
		return ChangeOrder
	}
	return ChangeModified
}

// diffPath spells a path in graft's cursor syntax.
func diffPath(elements []yamldiff.PathElement) string {
	parts := make([]string, len(elements))
	for i, e := range elements {
		switch {
		case e.Key != "":
			parts[i] = e.Key + "=" + e.Name
		case e.Name != "":
			parts[i] = cursorpath.Quote(e.Name)
		default:
			parts[i] = strconv.Itoa(e.Idx)
		}
	}
	return strings.Join(parts, ".")
}

// newDiffConverter returns a converter for the nodes read from src.
func newDiffConverter(src []byte) *converter {
	text := parsedText(src)
	return &converter{
		seen:  make(map[*yamlnode.Node]*Node),
		lines: bytes.Split(text, []byte("\n")),
		text:  &readText{doc: text},
	}
}
