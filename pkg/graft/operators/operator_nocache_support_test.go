package operators

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// The nocache helpers connect the parsed :nocache modifier to the backend
// cache layers: Opcall.Run publishes the call's flag as ev.NoCache (ambient
// per-call context, exactly like ev.Target), ShouldSkipCache reads it, and
// WithNoCacheCheck/IsNoCacheResponse mark and inspect a Response so cache
// layers can decline to store it.
func TestNoCacheSupportHelpers(t *testing.T) {
	Convey("ShouldSkipCache reflects the evaluator's ambient NoCache flag", t, func() {
		So(ShouldSkipCache(&Evaluator{}), ShouldBeFalse)
		So(ShouldSkipCache(&Evaluator{NoCache: true}), ShouldBeTrue)
	})

	Convey("WithNoCacheCheck marks a response and IsNoCacheResponse reads it", t, func() {
		resp := &Response{Type: Replace, Value: "v"}
		So(IsNoCacheResponse(resp), ShouldBeFalse)

		marked := WithNoCacheCheck(resp, true)
		So(marked, ShouldEqual, resp) // same response, mutated in place
		So(IsNoCacheResponse(resp), ShouldBeTrue)

		unmarked := WithNoCacheCheck(&Response{Type: Replace}, false)
		So(IsNoCacheResponse(unmarked), ShouldBeFalse)
	})

	Convey("IsNoCacheResponse is nil-safe", t, func() {
		So(IsNoCacheResponse(nil), ShouldBeFalse)
	})
}
