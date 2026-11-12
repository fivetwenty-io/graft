package oploc

import (
	"fmt"
	"reflect"

	"github.com/fivetwenty-io/graft/internal/literal"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// RefuseAncestor returns a cycle error when here, the path of a running
// operator, sits under target, the path that operator reads and hands on
// whole. The value it would hand on is the container that holds the
// operator, so the document would end up holding itself. Both paths are
// compared in canonical form, so a name or predicate segment that picks
// the same list item as an index is caught too, while the error names
// both paths as they are spelled. A target that does not resolve against
// root is left to the caller's not-found error.
func RefuseAncestor(here, target *tree.Cursor, root interface{}) error {
	if here == nil || target == nil {
		return nil
	}
	canon, err := target.Canonical(root)
	if err != nil {
		return nil //nolint:nilerr // an unresolved target gets the caller's not-found error
	}
	if !Canonical(here, root).Under(canon) {
		return nil
	}
	return cycleError(here, target)
}

// RefuseSelfHolding returns a cycle error when val, the value an
// operator at here is about to write, is the very map or list of one of
// here's ancestors in root. Writing it would leave that container
// holding itself. It is the evaluator's last guard for any operator
// whose read RefuseAncestor did not see.
func RefuseSelfHolding(here *tree.Cursor, val, root interface{}) error {
	if here == nil {
		return nil
	}
	ptr, ok := containerPointer(val)
	if !ok {
		return nil
	}
	for n := len(here.Nodes) - 1; n > 0; n-- {
		anc := &tree.Cursor{Nodes: here.Nodes[:n]}
		o, err := anc.Resolve(root)
		if err != nil {
			continue
		}
		if p, ok := containerPointer(o); ok && p == ptr {
			return cycleError(here, anc)
		}
	}
	return nil
}

// cycleError is the error both guards return. It names the two paths and
// quotes no value.
func cycleError(here, target *tree.Cursor) error {
	return fmt.Errorf("cycle detected: $.%s grabs $.%s, which contains it", here, target)
}

// containerPointer returns the address that identifies v when v is a map
// or a non-empty list, reading through a literal wrapper. An empty list
// holds nothing, so it never identifies an ancestor.
func containerPointer(v interface{}) (uintptr, bool) {
	v, _ = literal.Unwrap(v)
	switch c := v.(type) {
	case map[string]interface{}:
		if c == nil {
			return 0, false
		}
		return reflect.ValueOf(c).Pointer(), true
	case []interface{}:
		if len(c) == 0 {
			return 0, false
		}
		return reflect.ValueOf(&c[0]).Pointer(), true
	}
	return 0, false
}
