package yamlprep

// Prepare applies graft's goccy input workarounds in ParseYAML's order:
// the bare-dash rewrite, inject-key quoting, and brace-placeholder
// quoting. It returns the rewritten bytes and the 1-based lines whose bare
// "-" became "- ~". None of the rewrites adds or removes a newline, so
// line numbers in the result match the input.
func Prepare(data []byte) ([]byte, []int) {
	out, lines := BareDashRewrites(data)
	out = QuoteInjectKeys(out)
	return QuoteBracePlaceholders(out), lines
}
