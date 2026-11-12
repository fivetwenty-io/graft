package graft

import "os"

// SubstituteEnvNodes replaces each node of the form $NAME with the value
// of the environment variable NAME when it is set and not empty. A lone
// "$" and a node whose variable is unset or empty stay as written. It
// returns a new slice and never changes nodes.
func SubstituteEnvNodes(nodes []string) []string {
	out := make([]string, len(nodes))
	for i, node := range nodes {
		out[i] = node
		if len(node) > 1 && node[0] == '$' {
			if val := os.Getenv(node[1:]); val != "" {
				out[i] = val
			}
		}
	}
	return out
}

// substituteReferenceEnv replaces the $NAME nodes of a parsed reference
// with their values, so the dependency graph sees the path the operator
// will read. A bracket node that a variable replaced is a key from then
// on, not a path to resolve, so its flag in bracketed is cleared.
func substituteReferenceEnv(nodes []string, bracketed []bool) []string {
	out := SubstituteEnvNodes(nodes)
	for i := range out {
		if out[i] != nodes[i] && i < len(bracketed) {
			bracketed[i] = false
		}
	}
	return out
}
