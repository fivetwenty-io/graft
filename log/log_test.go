package log

import (
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDebug(t *testing.T) {
	var stderr string
	PrintStdErrf = func(format string, args ...interface{}) {
		stderr = fmt.Sprintf(format, args...)
	}
	Convey("debug", t, func() {
		Convey("Outputs when debug is set to true", func() {
			stderr = ""
			DebugOn = true
			DEBUG("test debugging")
			So(stderr, ShouldEqual, "DEBUG> test debugging\n")
		})
		Convey("Multi-line debug inputs are each prefixed", func() {
			stderr = ""
			DebugOn = true
			DEBUG("test debugging\nsecond line")
			So(stderr, ShouldEqual, "DEBUG> test debugging\nDEBUG> second line\n")
		})
		Convey("Doesn't output when debug is set to false", func() {
			stderr = ""
			DebugOn = false
			DEBUG("test debugging")
			So(stderr, ShouldEqual, "")
		})
	})
}

func TestFilter(t *testing.T) {
	var stderr string
	PrintStdErrf = func(format string, args ...interface{}) {
		stderr += fmt.Sprintf(format, args...)
	}
	defer func() { Filter = nil }()
	Convey("filter", t, func() {
		Filter = func(message string) (string, bool) {
			if message == "drop" {
				return "", false
			}
			return "<" + message + ">", true
		}
		Convey("Rewrites a kept message before each line is prefixed", func() {
			stderr = ""
			DebugOn = true
			DEBUG("one\ntwo")
			So(stderr, ShouldEqual, "DEBUG> <one\nDEBUG> two>\n")
		})
		Convey("Writes nothing for a dropped message", func() {
			stderr = ""
			TraceOn = true
			TRACE("drop")
			So(stderr, ShouldEqual, "")
		})
	})
}
