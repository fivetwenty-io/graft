// Package yqdoc describes where an output value's input document came
// from, so that the file-splitting code can name its files by the
// document's position.
package yqdoc

import "github.com/fivetwenty-io/graft/pkg/query/value"

// Document is where one input document came from. A pkg/yq Decoder records
// the same facts in the Meta of every value it returns, as DocIndex,
// FileIndex, and Filename.
type Document struct {
	// Value is the document's root.
	Value *value.Value
	// Index is the document's position in its file, counted from zero.
	Index int
	// FileIndex is the file's position among the inputs, counted from zero.
	FileIndex int
	// Filename names the file in error messages and in the filename
	// operator.
	Filename string
}
