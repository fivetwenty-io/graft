package runtime

import "github.com/fivetwenty-io/graft/pkg/query"

// Capabilities is the security boundary of a run. Env allows reading the
// process environment, Files allows reading files, and System is recorded
// for yq's flag and enables nothing. Every nested evaluation inherits the
// capabilities of the Ctx it runs under, so none can regain one.
type Capabilities struct {
	Env, Files, System bool
}

// RequireEnv returns the runtime error for a read of the environment when
// Env is off.
func (c Capabilities) RequireEnv() error {
	if c.Env {
		return nil
	}
	return &query.RuntimeError{Msg: "env operations are disabled"}
}
