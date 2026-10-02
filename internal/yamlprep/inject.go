// Package yamlprep rewrites YAML bytes before goccy/go-yaml parses them.
// It holds the workarounds graft needs so that goccy reads the same
// documents spruce reads, and every parser entry point in graft applies
// them in the same order through Prepare.
package yamlprep

import (
	"bytes"
	"regexp"
)

// injectKeyStandaloneRe matches <<<: as a standalone map key.
var injectKeyStandaloneRe = regexp.MustCompile(`(?m)^(\s*(?:- )?)<<<:`)

// injectKeyDottedRe matches <<<: at the end of a dotted path key (e.g., host.web1.<<<:).
var injectKeyDottedRe = regexp.MustCompile(`(?m)^(\s*(?:- )?)(\S+\.<<<):`)

// QuoteInjectKeys pre-processes YAML bytes to quote the graft-specific
// <<<: inject key, which goccy/go-yaml rejects when unquoted because it
// reads <<< as a variant of the YAML merge key <<. It handles both the
// standalone (<<<:) and dotted path (foo.<<<:) forms.
func QuoteInjectKeys(data []byte) []byte {
	// Both regexes require a literal "<<<"; almost no document contains
	// one, so skip the two full-buffer regex passes when none is present
	// and hand the caller back the original slice.
	if !bytes.Contains(data, []byte("<<<")) {
		return data
	}

	// First quote dotted paths (must be first to avoid double-quoting)
	data = injectKeyDottedRe.ReplaceAll(data, []byte(`${1}"${2}":`))
	// Then quote standalone <<<:
	data = injectKeyStandaloneRe.ReplaceAll(data, []byte(`${1}"<<<":`))
	return data
}
