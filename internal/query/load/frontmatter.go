package load

import (
	"bytes"
	"io"
)

// SplitFrontMatter divides r the way yq's --front-matter does. When the
// first line is "---", the YAML runs from that line up to the next line
// that starts with "---", and the rest begins at that closing line, so it
// still holds the separator. With no closing line the whole input is the
// YAML and the rest is empty. Any other input has no front matter, and all
// of it comes back as the YAML with found false.
func SplitFrontMatter(r io.Reader) (yaml []byte, rest []byte, found bool, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, false, err
	}
	first, after, _ := bytes.Cut(data, []byte("\n"))
	if string(bytes.TrimSpace(first)) != "---" {
		return data, nil, false, nil
	}
	pos := len(data) - len(after)
	for pos < len(data) {
		if bytes.HasPrefix(data[pos:], []byte("---")) {
			return data[:pos], data[pos:], true, nil
		}
		next := bytes.IndexByte(data[pos:], '\n')
		if next < 0 {
			break
		}
		pos += next + 1
	}
	return data, nil, true, nil
}
