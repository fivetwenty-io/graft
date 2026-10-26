package query_test

import (
	"errors"
	"fmt"

	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func ExampleCompileError() {
	var err error = &query.CompileError{Msg: "syntax error, unexpected end of file", Line: 1, Col: 3}
	var ce *query.CompileError
	if errors.As(err, &ce) {
		fmt.Println(ce.Line, ce.Col)
	}
	// Output: 1 3
}

func ExampleCompileError_Error() {
	err := &query.CompileError{Msg: "$x is not defined", Line: 2, Col: 5}
	fmt.Println(err)
	// Output: $x is not defined at line 2, column 5
}

func ExampleRuntimeError() {
	var err error = &query.RuntimeError{Msg: `{"code":7} (not a string)`, Value: value.Obj(value.Pair{Key: "code", Value: value.Num(7)})}
	var re *query.RuntimeError
	if errors.As(err, &re) {
		code, _ := re.Value.Get("code")
		fmt.Println(code.Float())
	}
	// Output: 7
}

func ExampleRuntimeError_Error() {
	err := &query.RuntimeError{Msg: "Cannot index number with \"a\""}
	fmt.Println(err)
	// Output: Cannot index number with "a"
}

func ExampleHaltError() {
	var err error = &query.HaltError{Code: 2, Message: value.Str("stopping\n")}
	var he *query.HaltError
	if errors.As(err, &he) {
		fmt.Printf("exit %d, print %q\n", he.Code, he.Message.Str())
	}
	// Output: exit 2, print "stopping\n"
}

func ExampleHaltError_Error() {
	fmt.Println(&query.HaltError{Code: 1, Message: value.Str("done")})
	fmt.Println(&query.HaltError{Code: 3, Message: value.Arr()})
	// Output:
	// done
	// halted with exit code 3
}

func ExampleStatus() {
	st := query.Status{Code: 3, Message: "Compile error: syntax error"}
	if st.Message != "" {
		fmt.Println(st.Message)
	}
	fmt.Println("exit", st.Code)
	// Output:
	// Compile error: syntax error
	// exit 3
}
