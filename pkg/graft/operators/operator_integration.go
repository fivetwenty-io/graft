package operators

import (
	"github.com/fivetwenty-io/graft/pkg/graft"
)

//nolint:gochecknoinits // Parser function registration must happen at package load time
func init() {
	// Register the unified parser function with the graft package
	graft.ParseOpcallFunc = parseOpcallWithParser
}

// parseOpcallWithParser uses the unified Parser implementation.
func parseOpcallWithParser(phase graft.OperatorPhase, src string) (*graft.Opcall, error) {
	return graft.ParseOpcallWithParser(phase, src)
}

// ParseOpcall provides access to the parser from within the operators package.
// It delegates to the unified parser via graft.ParseOpcall.
func ParseOpcall(phase OperatorPhase, src string) (*graft.Opcall, error) {
	return graft.ParseOpcall(phase, src)
}

// Opcall is a type alias for graft.Opcall.
type Opcall = graft.Opcall
