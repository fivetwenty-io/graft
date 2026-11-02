package yaml

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

// decoderPackages are the packages whose panics a decode turns into an
// error. goccy panics on some target shapes, such as an unexported
// embedded struct whose key the document holds, either itself or
// through package reflect.
var decoderPackages = []string{"github.com/goccy/go-yaml", "reflect."}

// panicFromDecoder reports whether the panic being recovered started in
// one of decoderPackages, rather than in a caller's own method that the
// decoder ran. It must be called from the deferred function that
// recovers, while the panicking frames are still on the stack. It finds
// the first frame below runtime.gopanic that is not in the runtime,
// which is where the panic started.
func panicFromDecoder() bool {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(1, pcs)])
	panicking := false
	for {
		frame, more := frames.Next()
		switch {
		case frame.Function == "runtime.gopanic":
			panicking = true
		case panicking && !strings.HasPrefix(frame.Function, "runtime."):
			for _, pkg := range decoderPackages {
				if strings.HasPrefix(frame.Function, pkg) {
					return true
				}
			}
			return false
		}
		if !more {
			return false
		}
	}
}

// decoderPanicError is the error a decode returns in place of a decoder
// panic. It names only the target's Go type, since a panic's value can
// quote the input, and it has no position, since a panic carries none.
func decoderPanicError(t reflect.Type) *DecodeError {
	return &DecodeError{Kind: ErrorKindType, Msg: fmt.Sprintf("cannot decode into Go value of type %s", t)}
}
