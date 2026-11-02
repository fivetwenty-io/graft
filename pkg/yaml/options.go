package yaml

import "github.com/fivetwenty-io/graft/pkg/sortkeys"

// DecodeOptions selects how Unmarshal reads a document, and its zero value
// is the default.
type DecodeOptions struct {
	// YAML12 reads yes, no, on, and off as strings, as YAML 1.2 does,
	// instead of as booleans.
	YAML12 bool
}

// EncodeOptions selects how Marshal writes a value, and its zero value is
// the default.
type EncodeOptions struct {
	// KeyOrder orders every map's keys. The zero value means
	// sortkeys.Spruce, sortkeys.Lexical is also accepted, and
	// sortkeys.None is refused because Go maps have no order to keep.
	KeyOrder sortkeys.Mode
}
