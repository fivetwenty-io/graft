package yq

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
)

func TestYQControl(t *testing.T) {
	wantYQ(t, `with(.a; . = 2)`, "a: 1\n", "a: 2\n")
	wantYQ(t, `.[] as $i ireduce (0; . + $i)`, "- 1\n- 2\n", "3\n")
	wantYQ(t, `eval(".a")`, "a: 1\n", "1\n")
	wantYQ(t, `.[] | split_doc`, "[1, 2]\n", "1\n---\n2\n")
	wantYQError(t, `error("boom")`, "{}", "boom")
}

func TestYQLoad(t *testing.T) {
	f := writeTemp(t, "b: 2\n")
	fs := writeTemp(t, "text")
	wantYQ(t, `.a = load("`+f+`") | .c = load_str("`+fs+`")`, "{}", "a:\n  b: 2\nc: text\n")
	wantYQErrorOpts(t, Options{Caps: runtime.Capabilities{Env: true}}, `load("`+f+`")`, "{}", "file operations have been disabled")
}
