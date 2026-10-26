//go:build !unix

package jq

import "os"

// openNoBlock opens path for reading.
func openNoBlock(path string) (*os.File, error) {
	return os.Open(path) // #nosec G304 -- the search path and the module name pick the file
}
