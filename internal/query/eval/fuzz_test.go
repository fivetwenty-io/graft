package eval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func FuzzEval(f *testing.F) {
	for _, s := range []string{`.a[0] | {b: .c}`, `[.[] | . + 1]`, `reduce .[] as $x (0; . + $x)`, `def f: f; f`, `[range(0; 100)] | length`, `.. | numbers`, `try error("x") catch .`, `label $o | 1, break $o`, `path(..)`, `.[1:3] = ["x"]`} {
		f.Add(s, `{"a":[1,2,3],"c":4}`)
	}
	f.Add(`. as [$a, {b: $c}] | $a, $c`, `[1,{"b":2}]`)
	f.Fuzz(func(t *testing.T, src, input string) {
		prog, err := parser.Parse(src, parser.JQ())
		if err != nil {
			return
		}
		dec := json.NewDecoder(strings.NewReader(input))
		dec.UseNumber()
		in, err := readJSON(dec)
		if err != nil {
			return
		}
		deadline, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		ctx := newCtx()
		ctx.Deadline = deadline
		ctx.DepthLimit = 256
		n := 0
		_ = Run(ctx, prog, NewEnv(ctx.Dialect.Builtins()), []*value.Value{in}, func(*value.Value, value.Path) error {
			n++
			if n > 10000 {
				cancel()
			}
			return nil
		})
	})
}
