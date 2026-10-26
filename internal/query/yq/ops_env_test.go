package yq

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
)

func TestYQEnv(t *testing.T) {
	t.Setenv("YQ_T", "v")
	t.Setenv("YQ_M", "{a: 1}")
	wantYQ(t, `env(YQ_T), strenv(YQ_T), ("${YQ_T}-${NOPE}" | envsubst)`, "null\n", "v\nv\nv-\n")
	wantYQ(t, `env(YQ_M) | .a`, "null\n", "1\n")
	wantYQ(t, `"${NOPE:-d}-${YQ_T:+s}-$$" | envsubst`, "null\n", "d-s-$\n")
	wantYQError(t, `"${NOPE}" | envsubst(nu)`, "null\n", "variable ${NOPE} not set")
	wantYQError(t, `env(NOPE)`, "null\n", "value for env variable 'NOPE' not provided in env()")
	wantYQErrorOpts(t, Options{Caps: runtime.Capabilities{Files: true}}, `env(YQ_T)`, "null\n", "env operations have been disabled")
}
