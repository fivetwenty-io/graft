package yq

// exitFailure is the one code yq exits with for every failure.
const exitFailure = 1

// ExitCode returns the code yq exits with. produced says whether the run
// printed any output, anyTruthy says whether any output was neither null
// nor false, err is the error that ended the run, and exitStatus is -e.
//
// Every error exits with 1, whether the expression did not parse, the
// evaluation failed, or an input file could not be read. Without an error,
// -e exits 1 when nothing was produced or every output was null or false,
// and 0 otherwise, because yq treats one truthy output among several as a
// match. Without -e a run that ends cleanly exits 0.
func ExitCode(produced, anyTruthy bool, err error, exitStatus bool) int {
	if err != nil {
		return exitFailure
	}
	if exitStatus && (!produced || !anyTruthy) {
		return exitFailure
	}
	return 0
}
