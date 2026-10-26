//go:build unix

package jq

import (
	"os"
	"syscall"
)

// openNoBlock opens path for reading without waiting on a pipe that has
// no writer, so a file swapped for a FIFO after the search checked it
// cannot hang the compiler. The caller checks the type of what it opened.
func openNoBlock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0) // #nosec G304 -- the search path and the module name pick the file
}
