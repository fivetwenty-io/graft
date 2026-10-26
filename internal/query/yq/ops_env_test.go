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

func TestYQBareEnvIsNotAnOperator(t *testing.T) {
	t.Setenv("YQ_T", "v")
	wantYQError(t, `env | .YQ_T`, "null\n", `1:1: lexer: invalid input text "env | .YQ_T"`)
	wantYQError(t, `[env]`, "null\n", `1:2: lexer: invalid input text "env]"`)
	wantYQError(t, `"\(1 + env.YQ_T) x"`, "null\n", `1:5: lexer: invalid input text "env.YQ_T"`)
	wantYQError(t, `eval("env")`, "null\n", `1:1: lexer: invalid input text "env"`)
	wantYQErrorOpts(t, Options{}, `env`, "null\n", `1:1: lexer: invalid input text "env"`)
	wantYQ(t, `.env, env(YQ_T)`, "env: 1\n", "1\nv\n")
}

func TestYQEnvOperatorsHonorTheCapability(t *testing.T) {
	t.Setenv("YQ_T", "v")
	off := Options{Caps: runtime.Capabilities{Files: true}}
	for _, expr := range []string{`env(YQ_T)`, `strenv(YQ_T)`, `"${YQ_T}" | envsubst`, `"${YQ_T}" | envsubst(nu)`, `eval("strenv(YQ_T)")`} {
		wantYQErrorOpts(t, off, expr, "null\n", "env operations have been disabled")
	}
}

func TestYQEnvOfAnEmptyVariable(t *testing.T) {
	t.Setenv("YQ_E", "")
	wantYQError(t, `env(YQ_E)`, "null\n", "value for env variable 'YQ_E' not provided in env()")
	wantYQ(t, `strenv(YQ_E) | tag`, "null\n", "!!str\n")
}
