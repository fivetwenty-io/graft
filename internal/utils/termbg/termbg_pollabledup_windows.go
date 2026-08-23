//go:build windows

package termbg

import (
	"errors"
	"os"
)

// pollableDup is queryOSC11's seam for turning in into a handle that
// supports SetReadDeadline (see its doc comment in termbg.go); on
// Windows it starts out as defaultPollableDup, which always fails (see
// below), and tests substitute a different failure by reassigning it
// directly.
var pollableDup = defaultPollableDup

// defaultPollableDup has no Windows implementation: dup(2) and
// O_NONBLOCK are POSIX concepts with no direct Windows equivalent for
// console handles. queryOSC11 treats this the same as any other
// pollableDup failure - Unknown, with nothing ever written to the
// terminal - so Detect degrades to its documented fallback on Windows
// rather than risk writing a query it could never safely read the
// reply to.
func defaultPollableDup(*os.File) (dup *os.File, release func(), err error) {
	return nil, nil, errors.New("termbg: pollable duplication is not supported on windows")
}
