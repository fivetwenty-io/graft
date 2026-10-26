package jq

import (
	"errors"
	"io/fs"

	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// The results jq 1.8.2's main loop keeps, and the codes it exits with.
const (
	resultNoOutput  = -4
	resultFalsy     = -1
	exitUsage       = 2
	exitCompile     = 3
	exitNoOutput    = 4
	exitRuntimeFail = 5
)

// ExitCode returns the code jq exits with. last is the last output and
// produced says whether there was any, err is the error that ended the
// run, and exitStatus is -e.
//
// A *query.HaltError exits with its own code, a *query.CompileError with
// 3, an *fs.PathError, such as an input file that cannot be opened or
// read, with 2, and any other error, such as a runtime error or input
// that does not parse, with 5. Without -e a negative halt code exits 0.
// Without an error, -e exits 4 when nothing was produced and 1 when the
// last output was false or null, and so does halt_error(-4) under -e.
//
// A run over many inputs follows jq's main loop. last and produced come
// from the last input whose program ended without an error and produced
// something, err is the error that ended the last input to run, and an
// input file that could not be opened at any point makes err that
// *fs.PathError. So under -e an input that fails and a later one that
// ends on true exit 0, as jq does.
func ExitCode(last *value.Value, produced bool, err error, exitStatus bool) int {
	ret := runResult(last, produced, err)
	if !exitStatus {
		return max(ret, 0)
	}
	if ret == resultNoOutput {
		// jq reads a halt with code -4 as a run with no output too.
		ret = runResult(last, produced, nil)
		if ret == resultNoOutput {
			return exitNoOutput
		}
	}
	if ret < 0 {
		return -ret
	}
	return ret
}

// runResult is the result jq's main loop is left holding, which is the
// halt code or an error's exit code, or for a run without an error -4
// when nothing was produced, -1 when the last output was false or null,
// and 0 otherwise.
func runResult(last *value.Value, produced bool, err error) int {
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
	if !produced {
		return resultNoOutput
	}
	if last == nil || last.Kind() == value.KindNull || last.Kind() == value.KindFalse {
		return resultFalsy
	}
	return 0
}
