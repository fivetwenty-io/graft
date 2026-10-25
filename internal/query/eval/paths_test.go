package eval

import "testing"

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
}
