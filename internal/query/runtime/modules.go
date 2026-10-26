package runtime

import (
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// ModuleLibrary is where a run finds the modules that modulemeta and its
// siblings describe. The jq dialect supplies one, and a run without one
// gets the dialect's defaults.
type ModuleLibrary interface {
	// SearchList is the list of library directories as the command gave
	// them, which get_search_list returns.
	SearchList() []string
	// ProgOrigin is the directory of the main program.
	ProgOrigin() string
	// JQOrigin is the directory that $ORIGIN stands for.
	JQOrigin() string
	// Meta reads the module named name and returns its metadata object
	// with the deps and defs that describe it.
	Meta(name string) (*value.Value, error)
}

// RequireFiles returns the runtime error for a read of a file when Files
// is off.
func (c Capabilities) RequireFiles() error {
	if c.Files {
		return nil
	}
	return &query.RuntimeError{Msg: "file operations are disabled"}
}
