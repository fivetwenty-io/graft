package controlflow

import "strings"

// HasMarkers reports whether source contains control-flow markers - the
// same classification Expand's fast path uses to decide whether a
// document is returned byte-identical. Callers use it as a purity
// predicate: a document with markers can evaluate operators (including
// external ones like vault) during parse, so it must never be served
// from a parse or output cache.
func HasMarkers(source []byte) bool {
	text := string(source)
	if !strings.Contains(text, "((") {
		return false
	}
	return hasControlFlowMarkers(classifyLines(text))
}
