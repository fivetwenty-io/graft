package main

import (
	"fmt"
	"strings"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// This file implements `graft merge --report-deferred=<placement>`'s
// in-band YAML-comment reporting (plans/dennis-feedback-gaps.md Item 2):
// deferred keys - whether from --defer-on-error/--adaptive's retry loop
// or from a --skip-vault/--skip-aws/--skip-nats flag - become comments
// woven into the merged output itself, so the report travels with the
// document and the output stays valid, re-mergeable YAML.

// reportPlacement selects where --report-deferred's comments go.
type reportPlacement string

const (
	reportPlacementBeginning reportPlacement = "beginning"
	reportPlacementInline    reportPlacement = "inline"
	reportPlacementEnd       reportPlacement = "end"
	reportPlacementNone      reportPlacement = "none"

	// defaultReportPlacement is what --report-deferred defaults to when
	// the flag is not given at all.
	defaultReportPlacement = reportPlacementBeginning
)

// parseReportPlacement validates a --report-deferred value, returning a
// clear error for anything other than the four documented placements.
// "" (the CLI flag's own default is "beginning", so this only arises
// from mergeOpts constructed directly - e.g. by tests, or a future
// library-style caller - without setting ReportDeferred at all) resolves
// to defaultReportPlacement, so a zero-value *mergeOpts behaves exactly
// like before --report-deferred existed.
func parseReportPlacement(s string) (reportPlacement, error) {
	if s == "" {
		return defaultReportPlacement, nil
	}
	switch reportPlacement(s) {
	case reportPlacementBeginning, reportPlacementInline, reportPlacementEnd, reportPlacementNone:
		return reportPlacement(s), nil
	default:
		return "", fmt.Errorf("invalid --report-deferred value %q: must be one of beginning, inline, end, none", s)
	}
}

// renderMergedTreeWithReport is renderMergedTree (main.go) extended with
// --report-deferred support. Output is byte-identical to renderMergedTree
// whenever deferred is empty or placement is "none" - plans/dennis-
// feedback-gaps.md Item 2's "a merge with no deferrals stays byte-
// identical to today" requirement - so a merge that never defers
// anything (--defer-on-error given but nothing failed, or no skip flag
// given at all) is completely unaffected by this function existing.
func renderMergedTreeWithReport(tree map[string]interface{}, deferred []graft.DeferredPath, placement reportPlacement) ([]byte, int) {
	log.TRACE("Converting the following data back to YML:")
	log.TRACE("%#v", tree)

	if cycleErr := graft.CheckForCycles(tree, 4096); cycleErr != nil {
		log.PrintStdErrf("%s\n", cycleErr.Error())
		return nil, 2
	}

	if len(deferred) == 0 || placement == reportPlacementNone {
		return marshalMergedDocument(tree, "")
	}

	switch placement {
	case reportPlacementInline:
		comments := make([]graft.YAMLHeadComment, 0, len(deferred))
		for _, d := range deferred {
			comments = append(comments, graft.YAMLHeadComment{
				Path:  d.Path,
				Lines: []string{" " + deferredCommentText(d, false)},
			})
		}
		merged, err := graft.MarshalYAMLWithComments(tree, comments)
		if err != nil {
			log.PrintStdErrf("Unable to convert merged result back to YAML: %s\nData:\n%#v", err.Error(), tree)
			return nil, 2
		}
		return finishMergedDocument(merged, ""), 0

	case reportPlacementEnd:
		merged, err := graft.MarshalYAML(tree)
		if err != nil {
			log.PrintStdErrf("Unable to convert merged result back to YAML: %s\nData:\n%#v", err.Error(), tree)
			return nil, 2
		}
		return finishMergedDocument(merged, deferredReportBlock(deferred)), 0

	default: // reportPlacementBeginning
		return marshalMergedDocument(tree, deferredReportBlock(deferred))
	}
}

// marshalMergedDocument is renderMergedTree's own document assembly
// ("---\n" + optional leading block + marshaled document + trailing
// newline), factored out so both the plain path and the "beginning"
// --report-deferred placement share it.
func marshalMergedDocument(tree map[string]interface{}, leadingBlock string) ([]byte, int) {
	merged, err := graft.MarshalYAML(tree)
	if err != nil {
		log.PrintStdErrf("Unable to convert merged result back to YAML: %s\nData:\n%#v", err.Error(), tree)
		return nil, 2
	}
	out := append([]byte("---\n"), []byte(leadingBlock)...)
	out = append(out, merged...)
	return append(out, '\n'), 0
}

// finishMergedDocument assembles "---\n" + merged + optional trailing
// block + trailing newline - the "end"/"inline" --report-deferred
// placements' own document assembly (leadingBlock is always empty for
// those two; only "beginning" needs one, via marshalMergedDocument).
func finishMergedDocument(merged []byte, trailingBlock string) []byte {
	out := append([]byte("---\n"), merged...)
	out = append(out, []byte(trailingBlock)...)
	return append(out, '\n')
}

// deferredReportBlock renders the "beginning"/"end" placements' comment
// block: a summary line, then one "# graft: deferred $.path: reason"
// line per entry, sorted by path, each ending in its own newline so it
// composes cleanly whether prepended or appended.
func deferredReportBlock(deferred []graft.DeferredPath) string {
	var b strings.Builder
	plural := "s"
	if len(deferred) == 1 {
		plural = ""
	}
	fmt.Fprintf(&b, "# graft: %d key%s deferred\n", len(deferred), plural)
	for _, d := range deferred {
		fmt.Fprintf(&b, "# %s\n", deferredCommentText(d, true))
	}
	return b.String()
}

// deferredCommentText renders one deferred entry's comment text (without
// the leading "# " - callers add that themselves, since inline
// placement needs a leading space instead, to read "# graft: ..." once
// goccy/go-yaml prepends its own bare "#"). withPath includes the
// "$.<path>: " prefix (beginning/end placement, where the comment is not
// already positioned next to the key it describes); inline placement
// passes withPath=false, since the comment's position already conveys
// the path.
func deferredCommentText(d graft.DeferredPath, withPath bool) string {
	if withPath {
		return fmt.Sprintf("graft: deferred $.%s: %s", d.Path, d.Reason)
	}
	return fmt.Sprintf("graft: deferred: %s", d.Reason)
}
