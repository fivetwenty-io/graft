package eval

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestPathsAndAssignment(t *testing.T) {
	wantOutputs(t, `path(.a[0].b)`, `null`, `["a",0,"b"]`)
	wantOutputs(t, `.a.b = 1`, `null`, `{"a":{"b":1}}`)
	wantOutputs(t, `.[] |= . + 1`, `[1,2]`, `[2,3]`)
	wantOutputs(t, `.a += 1`, `{"a":1}`, `{"a":2}`)
	wantOutputs(t, `.a //= 3`, `{"a":null}`, `{"a":3}`)
	wantOutputs(t, `(.a, .b) = (1, 2)`, `null`, `{"a":1,"b":1}`, `{"a":2,"b":2}`)
	wantOutputs(t, `.a = (1, 2)`, `{}`, `{"a":1}`, `{"a":2}`)
	wantOutputs(t, `.a += (1, 2)`, `{"a":1}`, `{"a":2}`, `{"a":3}`)
	wantOutputs(t, `.[] |= empty`, `[1,2,3]`, `[]`)   // jq 1.8.2 gathers the deletions and applies them once
	wantOutputs(t, `.[] |= (., .)`, `[1,2]`, `[1,2]`) // |= takes the first output
	wantOutputs(t, `path(.[1:3]), path(.[2:])`, `null`, `[{"start":1,"end":3}]`, `[{"start":2,"end":null}]`)
	wantOutputs(t, `getpath([{"start":1,"end":3}])`, `[1,2,3,4]`, `[2,3]`)
	wantOutputs(t, `setpath([{"start":1,"end":3}]; ["x"])`, `[1,2,3,4]`, `[1,"x",4]`)
	wantOutputs(t, `delpaths([[{"start":1,"end":3}]])`, `[1,2,3,4]`, `[1,4]`)
	wantOutputs(t, `.[1:3] = ["x"]`, `[1,2,3,4]`, `[1,"x",4]`)
	wantError(t, `path(1)`, `null`, "Invalid path expression with result 1")
}

func TestArithmeticAssignmentForms(t *testing.T) {
	wantOutputs(t, `.a -= 1 | .b *= 2 | .c /= 2 | .d %= 3`, `{"a":5,"b":5,"c":5,"d":5}`, `{"a":4,"b":10,"c":2.5,"d":2}`)
	wantOutputs(t, `.a[] += 10`, `{"a":[1,2]}`, `{"a":[11,12]}`)
	wantOutputs(t, `.a += 1`, `null`, `{"a":1}`)
	wantOutputs(t, `.a += .b`, `{"a":1,"b":2}`, `{"a":3,"b":2}`)
	wantOutputs(t, `.[0] |= empty`, `[1,2]`, `[2]`)
	wantOutputs(t, `.a |= (.b = 1)`, `{}`, `{"a":{"b":1}}`)
	wantError(t, `.[-2] = 1`, `[1]`, "Out of bounds negative array index")
	wantError(t, `.[1:] = 3`, `[1,2]`, "A slice of an array can only be assigned another array")
	wantError(t, `(1) = 2`, `null`, "Invalid path expression with result 1")
}

func TestPathTracking(t *testing.T) {
	wantPaths(t, `.a[1:2]`, `null`, `["a",{"start":1,"end":2}]`)
	wantPaths(t, `.[1:][0]`, `[1,2,3]`, `[{"start":1,"end":null},0]`)
	wantPaths(t, `.[]?`, `1`)
	wantPaths(t, `.[]`, `{"a":1,"b":2}`, `["a"]`, `["b"]`)
	wantPaths(t, `..`, `{"a":[1]}`, `[]`, `["a"]`, `["a",0]`)
	wantPaths(t, `if .a then .b else .c end`, `{"a":true}`, `["b"]`)
	wantPaths(t, `.a // .b`, `{"a":null}`, `["b"]`)
	wantPaths(t, `first(.a, .b)`, `{}`, `["a"]`)
	wantPaths(t, `.a | getpath(["b","c"])`, `null`, `["a","b","c"]`)
	wantPaths(t, `def f: .a; f | f`, `null`, `["a","a"]`)
	// reduce and foreach track a path only while the state is the very
	// value their init produced, as jq 1.8.2 does.
	wantPaths(t, `reduce ("a","b") as $k (.; .[$k])`, `null`, `[]`)
	wantPaths(t, `reduce empty as $k (.a; .b)`, `null`, `["a"]`)
	wantPaths(t, `foreach ("a","b") as $k (.; .[$k])`, `null`, `["a"]`, `["b"]`)
	wantPaths(t, `.a as $x | .b`, `null`, `["b"]`)
	// A destructuring bind keeps the input's path too. This departs from
	// jq 1.8.2, which raises an invalid path error for the object form and
	// gives [0,"b"] for the array form.
	wantPaths(t, `. as {a: $v} | .b`, `{"a":1}`, `["b"]`)
	wantPaths(t, `. as [$a] | .b`, `null`, `["b"]`)
	wantPaths(t, `try .a catch .b`, `null`, `["a"]`)
	wantPaths(t, `label $f | .a, break $f`, `null`, `["a"]`)
	wantPaths(t, `empty`, `null`)
}

func TestInvalidPathExpressions(t *testing.T) {
	wantError(t, `path(1 | .a)`, `null`, `Invalid path expression near attempt to access element "a" of 1`)
	wantError(t, `path([1] | .[])`, `null`, `Invalid path expression near attempt to iterate through [1]`)
	wantError(t, `path(1, 2)`, `null`, "Invalid path expression with result 1")
	wantError(t, `path(try error("x") catch .)`, `null`, `Invalid path expression with result "x"`)
	wantError(t, `path("abcdefghijklmnopqrstuvwxyz0123456789")`, `null`, `Invalid path expression with result "abcdefghijklmnopqrstuvwx..."`)
	wantError(t, `path(.a | . + 1)`, `{"a":1}`, "Invalid path expression with result 2")
	wantError(t, `path(reduce 1 as $k (.; .a))`, `{"a":{"b":1}}`, `Invalid path expression with result {"b":1}`)
	wantError(t, `[path(foreach (1,2) as $k (.; .a))]`, `{"a":{"b":1}}`, `Invalid path expression near attempt to access element "a" of {"b":1}`)
	wantError(t, `path(.a)`, `1`, `Cannot index number with string ("a")`)
	// getpath's own errors come before the invalid path.
	wantError(t, `path(1 | getpath(["a"]))`, `null`, `Cannot index number with string ("a")`)
	wantError(t, `path(getpath("a"))`, `null`, "Path must be specified as an array")
}

// cursorCtx returns a context that tracks cursors, with a native "here"
// that emits the root and the path of the cursor it is called with.
func cursorCtx() *runtime.Ctx {
	ctx := newCtx()
	ctx.Cursors = true
	ctx.Dialect.Builtins().Register("here", 0, func(ctx *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		c := ctx.Cursor
		return emit(value.Arr(c.Root, value.Arr(c.Path...), value.Num(float64(c.Doc))), nil)
	})
	return ctx
}

func TestCursorsTrackValues(t *testing.T) {
	ctx := cursorCtx()
	wantOutputsWithCtx(t, ctx, `here`, `{"a":[1]}`, `[{"a":[1]},[],0]`)
	wantOutputsWithCtx(t, ctx, `.a[0] | here`, `{"a":[1]}`, `[{"a":[1]},["a",0],0]`)
	wantOutputsWithCtx(t, ctx, `.a[] | here`, `{"a":[1]}`, `[{"a":[1]},["a",0],0]`)
	wantOutputsWithCtx(t, ctx, `.. | here`, `[1]`, `[[1],[],0]`, `[[1],[0],0]`)
	wantOutputsWithCtx(t, ctx, `[.a] | .[0] | here`, `{"a":1}`, `[[1],[0],0]`)
	wantOutputsWithCtx(t, ctx, `1 | here`, `null`, `[1,[],0]`)
	wantOutputsWithCtx(t, ctx, `.a as $x | .b | here`, `{"a":1,"b":2}`, `[{"a":1,"b":2},["b"],0]`)
	wantOutputsWithCtx(t, ctx, `.[1:] | .[0] | here`, `[1,2]`, `[[2],[0],0]`)
	wantOutputsWithCtx(t, ctx, `(.a = 1) | .a | here`, `{}`, `[{"a":1},["a"],0]`)
	outs, err := mustEval(t, ctx, `.a | here`, []string{`{"a":1}`, `{"a":2}`})
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, `.a | here`, outs, []string{`[{"a":1},["a"],0]`, `[{"a":2},["a"],1]`})
}

func TestCursorPathsStayOutOfPathMode(t *testing.T) {
	ctx := cursorCtx()
	wantOutputsWithCtx(t, ctx, `path(.a[0])`, `null`, `["a",0]`)
	wantOutputsWithCtx(t, ctx, `[.a] | path(.[0])`, `{"a":1}`, `[0]`)
	wantOutputsWithCtx(t, ctx, `.a = 1 | .a`, `{}`, `1`)
	wantErrorWithCtx(t, ctx, `path(1)`, `null`, "Invalid path expression with result 1")
}
