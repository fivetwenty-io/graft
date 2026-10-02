package main

import (
	"bytes"
	"errors"

	"github.com/fivetwenty-io/graft/internal/humanreport"
	"github.com/fivetwenty-io/graft/internal/yamldiff"
)

// renderDefaultDiff returns exactly what `spruce diff a b` writes to
// stdout and stderr, and the exit code it returns. A load or compare
// error prints its message and exits 2. A detail that fails to render
// leaves the report written so far on stdout and exits 1 with nothing on
// stderr, as spruce does, because Genesis captures stderr into the same
// output. A failure spruce would panic on prints a message and exits 2.
// Otherwise the report is printed and the exit code is 1 when the files
// differ and 0 when they do not.
// handleDiff has already checked that paths holds exactly two files.
func renderDefaultDiff(paths []string, opts humanreport.Options) (stdout, stderr string, code int) {
	from, to, err := yamldiff.LoadFiles(paths[0], paths[1])
	if err != nil {
		var loadErr *yamldiff.LoadError
		if errors.As(err, &loadErr) {
			return "", loadErr.Styled(humanreport.LocationStyler(opts)) + "\n", 2
		}
		return "", err.Error() + "\n", 2
	}
	report, err := yamldiff.CompareInputFiles(from, to)
	if err != nil {
		return "", err.Error() + "\n", 2
	}

	var buf bytes.Buffer
	if err := humanreport.Write(&buf, report, opts); err != nil {
		var fatal *humanreport.FatalError
		if errors.As(err, &fatal) {
			return "", err.Error() + "\n", 2
		}
	}
	buf.WriteByte('\n')
	if len(report.Diffs) > 0 {
		return buf.String(), "", 1
	}
	return buf.String(), "", 0
}
