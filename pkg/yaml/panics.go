package yaml

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

// decoderModule is the module whose panics a decode turns into an
// error. goccy panics on some target shapes, such as an unexported
// embedded struct whose key the document holds, either itself or
// through package reflect.
const decoderModule = "github.com/goccy/go-yaml"

// inDecoder reports whether function, a frame's function name, is in
// decoderModule or in package reflect. A name has the module's path, or
// "reflect", followed by a dot or a slash, so a look-alike path such as
// github.com/goccy/go-yamlx does not match, and only the standard
// library's reflect has a name with no slash.
func inDecoder(function string) bool {
	if rest, ok := strings.CutPrefix(function, decoderModule); ok {
		return strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "/")
	}
	return strings.HasPrefix(function, "reflect.") && !strings.Contains(function, "/")
}

// panicFromDecoder reports whether the panic being recovered started in
// goccy or in package reflect, rather than in a caller's own method that the
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
			return inDecoder(frame.Function)
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
