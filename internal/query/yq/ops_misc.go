package yq

import (
	"math/rand/v2"
	"slices"
	"strconv"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// registerMisc adds the operators that stand on their own: shuffle, and
// the _yq_hex that rewrite puts in place of a hex literal.
func registerMisc(r *runtime.Registry) {
	r.RegisterSingle("shuffle", 0, shuffle)
	r.RegisterSingle("_yq_hex", 1, hexNumber)
}

// hexNumber is the integer a literal such as 0x10 stands for, which
// prints as written until it is changed, as an integer in a document does.
func hexNumber(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	text, _, err := argText(in, args[0])
	if err != nil {
		return err
	}
	n, err := strconv.ParseInt(text[2:], 16, 64)
	if err != nil {
		return &query.RuntimeError{Msg: err.Error()}
	}
	return emit(value.NumDec(decimal.FromInt64(n)).WithMeta(&value.Meta{Text: text}), nil)
}

// shuffleOrder permutes n elements through swap. It is rand.Shuffle, and
// a test replaces it with a seeded source to get a fixed order.
var shuffleOrder = rand.Shuffle

// shuffle gives a sequence with its elements in a random order, in the
// sequence's style with its comments.
func shuffle(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
	if in.Kind() != value.KindArray {
		return &query.RuntimeError{Msg: "node at path " + cursorPath(ctx) + " is not an array (it's a " + tagOf(in) + ")"}
	}
	out := slices.Clone(in.Elems())
	shuffleOrder(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return emit(like(in, value.Arr(out...)), nil)
}
