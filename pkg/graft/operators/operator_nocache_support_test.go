package operators

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// ShouldSkipCache connects the parsed :nocache modifier to the backend
// cache layers: Opcall.Run publishes the call's flag as ev.NoCache
// (ambient per-call context, exactly like ev.Target), and caching
// operators consult ShouldSkipCache before both the cache read and the
// cache write.
func TestNoCacheSupportHelpers(t *testing.T) {
	Convey("ShouldSkipCache reflects the evaluator's ambient NoCache flag", t, func() {
		So(ShouldSkipCache(&Evaluator{}), ShouldBeFalse)
		So(ShouldSkipCache(&Evaluator{NoCache: true}), ShouldBeTrue)
	})

	Convey("ShouldSkipCache is nil-safe", t, func() {
		So(ShouldSkipCache(nil), ShouldBeFalse)
	})
}
