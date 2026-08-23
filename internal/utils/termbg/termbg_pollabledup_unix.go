//go:build !windows

package termbg

import (
	"os"
	"syscall"
)

// pollableDup is queryOSC11's seam for turning in into a handle that
// supports SetReadDeadline; on unix it starts out as defaultPollableDup,
// the real implementation below, and tests substitute a failure by
// reassigning it directly.
var pollableDup = defaultPollableDup

// defaultPollableDup returns an independent handle sharing in's
// underlying open file description, freshly marked non-blocking so
// Go's runtime poller registers it - and SetReadDeadline works -
// regardless of whether in itself ever got that registration. In
// production in is normally os.Stdin, and Go does not register the
// standard descriptors with its runtime poller at all, so
// in.SetReadDeadline would fail with os.ErrNoDeadline before a query
// could ever be answered; a plain freshly opened /dev/tty does not
// reliably fix this either - on at least one BSD-derived kernel,
// /dev/tty is its own distinct "redirect to whatever my controlling
// terminal is" character device, and registering that specific device
// with the runtime's poller fails even though reads and writes through
// it work fine, silently leaving SetReadDeadline unsupported there
// too. Duplicating in's own descriptor via dup(2) keeps the real
// terminal device in.SetReadDeadline needs to see, rather than a
// separate, possibly unpollable alias of it.
//
// release restores in's shared file-status flags to whatever they
// were before (dup shares the OPEN FILE DESCRIPTION, including
// O_NONBLOCK, with in - not just the descriptor number - so marking
// the duplicate non-blocking marks in non-blocking too, for as long as
// both stay open; some streams, os.Pipe's own ends among them, are
// already non-blocking to begin with, so release restores exactly the
// flag it found rather than assuming in was blocking) and closes the
// duplicate. It must run - and complete - before anything else reads
// in again; queryOSC11's own defer guarantees that, and Detect always
// runs before any reader (readline, in cmd/graft) is even constructed
// (see withDetectedBackground's doc comment in
// cmd/graft/debug_theme.go).
func defaultPollableDup(in *os.File) (dup *os.File, release func(), err error) {
	origFd, err := rawFd(in)
	if err != nil {
		return nil, nil, err
	}

	// dup(2) shares the OPEN FILE DESCRIPTION - and so its O_NONBLOCK
	// flag - with in, not just the descriptor number: some streams
	// (os.Pipe's own ends, in particular) are already non-blocking
	// under the hood, since Go's own poll.FD.Read for a pollable
	// stream depends on the raw fd actually being non-blocking to work
	// at all. Recording the flag as found - rather than assuming in
	// was blocking - lets release restore exactly that, instead of
	// silently breaking in's own reads afterward by clearing a flag
	// Go itself was relying on.
	origFlags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(origFd), uintptr(syscall.F_GETFL), 0)
	if errno != 0 {
		return nil, nil, errno
	}
	wasNonblocking := origFlags&syscall.O_NONBLOCK != 0

	dupFd, err := syscall.Dup(origFd)
	if err != nil {
		return nil, nil, err
	}

	if err := syscall.SetNonblock(dupFd, true); err != nil {
		_ = syscall.Close(dupFd)
		return nil, nil, err
	}

	dup = os.NewFile(uintptr(dupFd), in.Name()+"-query")
	release = func() {
		_ = syscall.SetNonblock(dupFd, wasNonblocking)
		_ = dup.Close()
	}
	return dup, release, nil
}
