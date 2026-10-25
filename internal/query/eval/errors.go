package eval

import (
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// RuntimeError returns a runtime error carrying msg, which try catches.
func RuntimeError(msg string) *query.RuntimeError {
	return &query.RuntimeError{Msg: msg}
}

// BreakError is break $name on its way to the label that owns Label. try
// catches it, as jq 1.8.2 does, and so does the label it names.
type BreakError struct{ Label uintptr }

// Error describes the break.
func (e *BreakError) Error() string { return "break" }

// StopError ends a generator early. limit, first, and isempty raise it
// and swallow only their own, and try never catches it.
type StopError struct{}

// Error describes the stop.
func (e *StopError) Error() string { return "stop" }

// DownstreamError wraps an error that came back from the continuation a
// try handed its body, so the try lets it pass instead of catching an
// error raised outside it.
type DownstreamError struct{ Err error }

// Error returns the wrapped error's message.
func (e *DownstreamError) Error() string { return e.Err.Error() }

// Unwrap returns the wrapped error.
func (e *DownstreamError) Unwrap() error { return e.Err }

// catchable reports whether try and ?// handle err, which is true only for
// an error raised directly in their body and never for a wrapped one.
func catchable(err error) bool {
	switch err.(type) { //nolint:errorlint // only an error raised directly is caught, never a wrapped one
	case *query.RuntimeError, *BreakError:
		return true
	default:
		return false
	}
}

// invalidPathResult is the error for an output of a path expression that
// is not a path.
func invalidPathResult(v *value.Value) error {
	return RuntimeError("Invalid path expression with result " + dump(v))
}

// invalidPathAccess is the error for indexing a value that is not a path.
func invalidPathAccess(key, v *value.Value) error {
	return RuntimeError("Invalid path expression near attempt to access element " + dump(key) + " of " + dump(v))
}

// invalidPathIterate is the error for iterating a value that is not a
// path.
func invalidPathIterate(v *value.Value) error {
	return RuntimeError("Invalid path expression near attempt to iterate through " + dump(v))
}

// dump quotes v for an error message the way jq does.
func dump(v *value.Value) string { return runtime.Dump(v) }
