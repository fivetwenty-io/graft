package jq

import (
	"errors"
	"io/fs"

	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// The exit codes jq 1.8.2 uses.
const (
	exitFalsy       = 1
	exitUsage       = 2
	exitCompile     = 3
	exitNoOutput    = 4
	exitRuntimeFail = 5
)

// ExitCode returns the code jq exits with. last is the last output and
// produced says whether there was any, err is the error that ended the
// run, and exitStatus is -e. A *query.HaltError exits with its own code, a
// *query.CompileError with 3, an input file that cannot be opened with 2,
// and any other error, such as a runtime error or input that does not
// parse, with 5. Without an error, -e exits 4 when nothing was produced
// and 1 when the last output was false or null.
func ExitCode(last *value.Value, produced bool, err error, exitStatus bool) int {
	if err != nil {
		var he *query.HaltError
		var ce *query.CompileError
		var pe *fs.PathError
		switch {
		case errors.As(err, &he):
			return he.Code
		case errors.As(err, &ce):
			return exitCompile
		case errors.As(err, &pe):
			return exitUsage
		default:
			return exitRuntimeFail
		}
	}
	if !exitStatus {
		return 0
	}
	if !produced {
		return exitNoOutput
	}
	if last == nil || last.Kind() == value.KindNull || last.Kind() == value.KindFalse {
		return exitFalsy
	}
	return 0
}
