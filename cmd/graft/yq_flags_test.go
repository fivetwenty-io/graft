package main

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestYQJSONNumbers(t *testing.T) {
	Convey("json input numbers read as yq reads them", t, func() {
		out, _, _ := runYQ(t, []string{"-p", "json", "-o", "yaml", "."}, `{"a": 1.0, "b": "x"}`)
		So(out, ShouldEqual, "a: 1\nb: x\n")
		out, _, _ = runYQ(t, []string{"-p", "json", "-o", "json", "-I0", "."}, `{"a":1.50,"b":1e3,"c":1e20,"d":12345678.5,"e":0.00001,"f":-0.0,"g":9007199254740993,"h":[1.0]}`)
		So(out, ShouldEqual, `{"a":1.5,"b":1000,"c":1e+20,"d":1.23456785e+07,"e":1e-05,"f":0,"g":9007199254740992,"h":[1]}`+"\n")
	})
}
