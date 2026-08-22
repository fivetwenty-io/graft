package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/fivetwenty-io/graft/log"
)

// runGraftCommand invokes main() with the given CLI args and returns the
// captured stdout/stderr/exit code, restoring the previous test hooks
// and os.Args afterward. Shared body behind runMerge
// (skip_defer_test.go) and runVaultinfo (vaultinfo_resolve_test.go).
func runGraftCommand(t *testing.T, args []string) (stdout, stderr string, rc int) {
	t.Helper()

	prevPrintStdOutf := printStdOutf
	prevPrintStdErrf := log.PrintStdErrf
	prevExit := exit
	prevUsage := usage
	prevArgs := os.Args
	defer func() {
		printStdOutf = prevPrintStdOutf
		log.PrintStdErrf = prevPrintStdErrf
		exit = prevExit
		usage = prevUsage
		os.Args = prevArgs
	}()

	printStdOutf = func(format string, fmtArgs ...interface{}) {
		stdout += fmt.Sprintf(format, fmtArgs...)
	}
	log.PrintStdErrf = func(format string, fmtArgs ...interface{}) {
		stderr += fmt.Sprintf(format, fmtArgs...)
	}
	rc = 256 // sentinel: unset if exit is never called
	exit = func(code int) { rc = code }
	usage = func() { exit(1) }

	os.Args = append([]string{"graft"}, args...)
	main()
	return stdout, stderr, rc
}
