package operators

import (
	"context"
	"os"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestJoinDefCalcLoadEmptyParity locks the observable behavior of the
// join, defer, calc, load, and empty operators against spruce's
// op_join.go, op_defer.go, op_calc.go, op_load.go, and op_empty.go
// (argument semantics, output types, and the message portion of
// ` - $.path: msg` stderr lines).
func TestJoinDefCalcLoadEmptyParity(t *testing.T) {
	// Disable ANSI color so the scraped message portion is plain text,
	// matching how genesis reads non-tty stderr output.
	ansi.Color(false)
	defer ansi.Color(true)

	// --- join ---------------------------------------------------------

	Convey("join: separator + list of scalars flattens with fmt %v", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
items: [a, b, 3]
result: (( join "," items ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "a,b,3")
	})

	Convey("join: literals and references mix in one call", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
name: world
result: (( join "-" "hello" name ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "hello-world")
	})

	Convey("join: no arguments errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( join() ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "no arguments specified to (( join ... ))")
	})

	Convey("join: separator alone (too few arguments) errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( join "," ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "too few arguments supplied to (( join ... ))")
	})

	Convey("join: non-literal separator errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
sep: ","
items: [a, b]
result: (( join sep items ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "join operator only accepts literal argument for the separator")
	})

	Convey("join: nested list entry is rejected with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
items:
  - a
  - [b, c]
result: (( join "," items ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "entry #1 in list is not compatible for (( join ... ))")
	})

	Convey("join: unresolvable reference reports a single, non-doubled 'unable to resolve' message", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( join "," missing.thing ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "unable to resolve `missing.thing`:")
		// The prior implementation double-wrapped this message ("Unable to
		// resolve `X`: unable to resolve `X`: ..."); guard against regressing.
		firstIdx := indexOfSubstring(err.Error(), "unable to resolve")
		So(firstIdx, ShouldBeGreaterThanOrEqualTo, 0)
		secondIdx := indexOfSubstring(err.Error()[firstIdx+1:], "unable to resolve")
		So(secondIdx, ShouldBeLessThan, 0)
	})

	Convey("join: map argument is joined as sorted key:value pairs (documented graft extension)", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
settings:
  timeout: 30
  debug: true
result: (( join ", " settings ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "debug:true, timeout:30")
	})

	// --- defer ----------------------------------------------------------

	Convey("defer: multi-ref grab fallback chain round-trips as literal (( ... )) text", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( defer grab base.trusted extra.trusted more.trusted ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "(( grab base.trusted extra.trusted more.trusted ))")
	})

	Convey("defer: single-reference operator call round-trips as literal (( ... )) text", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( defer inject some.jobs.entry ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "(( inject some.jobs.entry ))")
	})

	Convey("defer: bare zero-arg operator name round-trips as literal (( ... )) text", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( defer append ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "(( append ))")
	})

	Convey("defer: no arguments errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( defer() ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "defer has no arguments - what are you deferring?")
	})

	// --- calc -------------------------------------------------------------

	Convey("calc: arithmetic expression with path substitution evaluates like spruce", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
meta:
  base_port: 9090
result: (( calc "meta.base_port + 1" ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetInt("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, 9091)
	})

	Convey("calc: supported functions (min, max, mod, pow, sqrt, floor, ceil) evaluate like spruce", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
r_min:   (( calc "min(2, 5)" ))
r_max:   (( calc "max(2, 5)" ))
r_mod:   (( calc "mod(9, 4)" ))
r_pow:   (( calc "pow(2, 3)" ))
r_sqrt:  (( calc "sqrt(9)" ))
r_floor: (( calc "floor(1.9)" ))
r_ceil:  (( calc "ceil(1.1)" ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		cases := map[string]int{
			"r_min": 2, "r_max": 5, "r_mod": 1, "r_pow": 8,
			"r_sqrt": 3, "r_floor": 1, "r_ceil": 2,
		}
		for path, want := range cases {
			got, err := out.GetInt(path)
			So(err, ShouldBeNil)
			So(got, ShouldEqual, want)
		}
	})

	Convey("calc: named variables remaining after substitution error with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( calc "unknown_var + 1" ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "calc operator does not support named variables in expression")
	})

	Convey("calc: nil-valued path reference errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
meta:
  base_port: ~
result: (( calc "meta.base_port + 1" ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "references a nil value, which cannot be used in calculations")
	})

	// --- load ---------------------------------------------------------

	Convey("load: reference argument reads a map-root YAML file (genesis's `load meta.users_path` pattern)", t, func() {
		f, err := os.CreateTemp(t.TempDir(), "graft-load-*.yml")
		So(err, ShouldBeNil)
		_, err = f.WriteString("host: db.example.com\nport: 5432\n")
		So(err, ShouldBeNil)
		So(f.Close(), ShouldBeNil)

		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
users_path: ` + f.Name() + `
result: (( load users_path ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result.host")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "db.example.com")
	})

	Convey("load: list-root YAML file loads as a list", t, func() {
		f, err := os.CreateTemp(t.TempDir(), "graft-load-*.yml")
		So(err, ShouldBeNil)
		_, err = f.WriteString("- one\n- two\n")
		So(err, ShouldBeNil)
		So(f.Close(), ShouldBeNil)

		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( load "` + f.Name() + `" ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetSlice("result")
		So(err, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{"one", "two"})
	})

	Convey("load: reference resolving to a map argument errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
meta:
  thing:
    nested: value
result: (( load meta.thing ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "tried to read file meta.thing, which is not a string scalar")
	})

	Convey("load: reference resolving to a list argument errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
meta:
  items: [1, 2, 3]
result: (( load meta.items ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "tried to read file meta.items, which is not a string scalar")
	})

	Convey("load: wrong argument count errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( load() ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "load operator requires exactly one literal string or reference argument")
	})

	// --- empty ----------------------------------------------------------

	Convey("empty: map/hash type-construction form", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( empty hash ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetMap("result")
		So(err, ShouldBeNil)
		So(val, ShouldResemble, map[string]interface{}{})
	})

	Convey("empty: array/list type-construction form (genesis's `empty array` pattern)", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( empty array ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetSlice("result")
		So(err, ShouldBeNil)
		So(val, ShouldResemble, []interface{}{})
	})

	Convey("empty: string type-construction form", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( empty string ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "")
	})

	Convey("empty: wrong argument count errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( empty() ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "empty operator expects 1 argument, received 0")
	})

	Convey("file: unreadable file errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( file "missing-file-for-parity-test.txt" ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "tried to read file missing-file-for-parity-test.txt: could not be read - open missing-file-for-parity-test.txt: no such file or directory")
	})

	Convey("file: reference resolving to a map argument errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
meta:
  thing:
    nested: value
result: (( file meta.thing ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "tried to read file meta.thing, which is not a string scalar")
	})

	Convey("file: reference resolving to a list argument errors with spruce's exact wording", t, func() {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
meta:
  items: [1, 2, 3]
result: (( file meta.items ))
`))
		So(err, ShouldBeNil)

		_, err = engine.Evaluate(context.TODO(), doc)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "tried to read file meta.items, which is not a string scalar")
	})
}

// indexOfSubstring returns the index of the first occurrence of substr in s,
// or -1 if not present. Local helper to avoid pulling in strings just for
// this one check across the double-wrap regression test.
func indexOfSubstring(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
