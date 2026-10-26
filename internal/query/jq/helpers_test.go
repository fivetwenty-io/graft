package jq_test

import (
	"bufio"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// j1Names is the J1 builtin list from the spec as name/arity tokens. The
// regex family has its own tests, the dates wait for J2, and the formats
// are checked under their @ names.
const j1Names = `
length/0 utf8bytelength/0 not/0 keys/0 keys_unsorted/0 has/1 in/1 map/1
map_values/1 path/1 paths/0 paths/1 leaf_paths/0 getpath/1 setpath/2
delpaths/1 del/1 to_entries/0 from_entries/0 with_entries/1 select/1
empty/0 recurse/0 recurse/1 recurse/2 add/0 add/1 any/0 any/1 any/2
all/0 all/1 all/2 flatten/0 flatten/1 range/1 range/2 range/3 floor/0
ceil/0 round/0 trunc/0 fabs/0 abs/0 sqrt/0 pow/2 log/0 log2/0 log10/0
exp/0 exp2/0 exp10/0 tostring/0 tonumber/0 toboolean/0 type/0 arrays/0
objects/0 iterables/0 booleans/0 numbers/0 normals/0 finites/0
strings/0 nulls/0 values/0 scalars/0 infinite/0 nan/0 isinfinite/0
isnan/0 isnormal/0 isvalid/1 sort/0 sort_by/1 group_by/1 unique/0
unique_by/1 min/0 max/0 min_by/1 max_by/1 reverse/0 contains/1
inside/1 indices/1 index/1 rindex/1 startswith/1 endswith/1 ltrimstr/1
rtrimstr/1 trimstr/1 trim/0 ltrim/0 rtrim/0 explode/0 implode/0 split/1
join/1 ascii_downcase/0 ascii_upcase/0 ascii/0 tojson/0 fromjson/0
tostream/0 fromstream/1 truncate_stream/1 limit/2 skip/2 first/0
first/1 last/0 last/1 nth/1 nth/2 until/2 while/2 repeat/1 isempty/1
combinations/0 combinations/1 walk/1 transpose/0 pick/1 toarray/0 env/0
builtins/0 input_line_number/0 have_literal_numbers/0 have_decnum/0
error/0 error/1 halt/0 halt_error/0 halt_error/1 debug/0 debug/1
stderr/0 input/0 inputs/0 input_filename/0 splits/1 format/1
@text/0 @json/0 @base64/0 @base64d/0 @html/0 @uri/0 @urid/0 @base32/0
@base32d/0 @csv/0 @tsv/0 @sh/0
`

// splitArity splits a name/arity token.
func splitArity(tok string) (string, int) {
	i := strings.LastIndexByte(tok, '/')
	return tok[:i], int(tok[i+1] - '0')
}

// mustJSON reads one JSON value.
func mustJSON(t testing.TB, s string) *value.Value {
	t.Helper()
	v, _, err := load.NewJSONReader(bufio.NewReader(strings.NewReader(s)), "test", load.LoadOptions{}).Next()
	if err != nil {
		t.Fatalf("reading %.40q: %v", s, err)
	}
	return v
}

// run compiles prog and runs it on in under the number mode, returning
// every output as JSON text and the error that ended the run.
func run(t testing.TB, prog string, in *value.Value, numbers runtime.NumberMode, stderr io.Writer) ([]string, error) {
	t.Helper()
	p, err := jq.Compile(prog)
	if err != nil {
		return nil, err
	}
	ctx := runtime.NewCtx(jq.New(), numbers)
	ctx.Stderr = stderr
	var out []string
	err = p.Run(ctx, p.Env, []*value.Value{in}, func(v *value.Value, _ value.Path) error {
		out = append(out, runtime.JSON(v))
		return nil
	})
	return out, err
}

func checkOutputs(t *testing.T, numbers runtime.NumberMode, prog, in string, want []string) {
	t.Helper()
	got, err := run(t, prog, mustJSON(t, in), numbers, io.Discard)
	if err != nil {
		t.Errorf("%s on %.40s: unexpected error: %v", prog, in, err)
		return
	}
	if len(got)+len(want) > 0 && !reflect.DeepEqual(got, want) {
		t.Errorf("%s on %.40s:\n got %.200q\nwant %.200q", prog, in, got, want)
	}
}

// wantOutputs runs prog on the JSON text in with exact numbers and checks
// that it prints exactly the JSON texts in want.
func wantOutputs(t *testing.T, prog, in string, want ...string) {
	t.Helper()
	checkOutputs(t, runtime.NumbersExact, prog, in, want)
}

// wantOutputsFloat is wantOutputs with float numbers, as --float-numbers
// runs.
func wantOutputsFloat(t *testing.T, prog, in string, want ...string) {
	t.Helper()
	checkOutputs(t, runtime.NumbersFloat, prog, in, want)
}

// message is the text a user sees for err, without a compile error's
// position.
func message(err error) string {
	var ce *query.CompileError
	if errors.As(err, &ce) {
		return ce.Msg
	}
	return err.Error()
}

// wantError checks that prog fails on in with the message msg.
func wantError(t *testing.T, prog, in, msg string) {
	t.Helper()
	err := runErr(t, prog, in)
	if err == nil {
		return
	}
	if got := message(err); got != msg {
		t.Errorf("%s on %.40s:\n got error %q\nwant error %q", prog, in, got, msg)
	}
}

// runErr runs prog on in and returns its error, failing the test when
// there is none.
func runErr(t *testing.T, prog, in string) error {
	t.Helper()
	out, err := run(t, prog, mustJSON(t, in), runtime.NumbersExact, io.Discard)
	if err == nil {
		t.Errorf("%s on %.40s: want an error, got %q", prog, in, out)
	}
	return err
}

// runOne runs prog on in for its outputs alone.
func runOne(t testing.TB, prog string, in *value.Value) {
	t.Helper()
	if _, err := run(t, prog, in, runtime.NumbersExact, io.Discard); err != nil {
		t.Fatalf("%s: %v", prog, err)
	}
}

func wantEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}
