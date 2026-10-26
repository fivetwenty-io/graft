package yq

import (
	"math/rand/v2"
	"slices"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// registerMisc adds the operators that stand on their own: shuffle.
func registerMisc(r *runtime.Registry) {
	r.RegisterSingle("shuffle", 0, shuffle)
}

// shuffle gives a sequence with its elements in a random order, in the
// sequence's style with its comments.
func shuffle(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
	if in.Kind() != value.KindArray {
		return &query.RuntimeError{Msg: "node at path " + cursorPath(ctx) + " is not an array (it's a " + tagOf(in) + ")"}
	}
	out := slices.Clone(in.Elems())
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return emit(like(in, value.Arr(out...)), nil)
}
