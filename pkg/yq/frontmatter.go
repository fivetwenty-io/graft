package yq

import (
	"io"

	"github.com/fivetwenty-io/graft/internal/query/load"
)

// SplitFrontMatter divides r the way yq's --front-matter does. When the
// first line is "---", the YAML runs up to the next line that starts with
// "---", and the rest begins at that line, so it still holds the
// separator. With no closing line the whole input is the YAML. Any other
// input has no front matter, and all of it comes back as the YAML with
// found false.
func SplitFrontMatter(r io.Reader) (yaml []byte, rest []byte, found bool, err error) {
	return load.SplitFrontMatter(r)
}
