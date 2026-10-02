// Command ptyrun runs a command under a pseudo-terminal with a fixed
// window size and copies everything the command writes to the terminal
// into a file. The spruce-compat harness uses it to run spruce and graft
// the way an interactive terminal would, at sizes Genesis's script(1)
// wrapper cannot set.
//
// Usage: ptyrun -cols 80 -rows 25 -out FILE -- COMMAND [ARGS...]
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func main() {
	cols := flag.Int("cols", 80, "terminal columns")
	rows := flag.Int("rows", 25, "terminal rows")
	out := flag.String("out", "", "file that receives the terminal output")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: ptyrun -cols N -rows N -out FILE -- COMMAND [ARGS...]")
		os.Exit(2)
	}
	f, err := os.Create(*out)
	if err != nil {
		fail(err)
	}
	defer func() { _ = f.Close() }()

	cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(*cols), Rows: uint16(*rows)})
	if err != nil {
		fail(err)
	}
	_, _ = io.Copy(f, ptmx)
	err = cmd.Wait()
	_ = ptmx.Close()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ptyrun:", err)
	os.Exit(125)
}
