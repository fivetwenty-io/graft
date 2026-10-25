package builtin

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// registerCollections adds sorting, grouping, the extremes, and indices.
// Every comparison goes through ctx.Compare and ctx.Equal, so the number
// mode changes ordering, grouping, and uniqueness together.
func registerCollections(r *runtime.Registry) {
	unary(r, "sort", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		if err := sortable(ctx, in); err != nil {
			return nil, err
		}
		return value.Arr(sortedBy(ctx, in.Elems(), in.Elems())...), nil
	})
	byKeys(r, "_sort_by", func(ctx *runtime.Ctx, elems, keys []*value.Value) *value.Value {
		return value.Arr(sortedBy(ctx, elems, keys)...)
	})
	byKeys(r, "_group_by", func(ctx *runtime.Ctx, elems, keys []*value.Value) *value.Value {
		return groups(ctx, elems, keys, false)
	})
	byKeys(r, "_unique_by", func(ctx *runtime.Ctx, elems, keys []*value.Value) *value.Value {
		return groups(ctx, elems, keys, true)
	})
	byKeys(r, "_min_by", func(ctx *runtime.Ctx, elems, keys []*value.Value) *value.Value {
		return extreme(ctx, elems, keys, false)
	})
	byKeys(r, "_max_by", func(ctx *runtime.Ctx, elems, keys []*value.Value) *value.Value {
		return extreme(ctx, elems, keys, true)
	})
	unary(r, "unique", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		if err := sortable(ctx, in); err != nil {
			return nil, err
		}
		return groups(ctx, in.Elems(), in.Elems(), true), nil
	})
	unary(r, "min", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		if err := sortable(ctx, in); err != nil {
			return nil, err
		}
		return extreme(ctx, in.Elems(), in.Elems(), false), nil
	})
	unary(r, "max", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		if err := sortable(ctx, in); err != nil {
			return nil, err
		}
		return extreme(ctx, in.Elems(), in.Elems(), true), nil
	})
	binary(r, "indices", indices)
}

// byKeys registers one of the _by natives, which take the input array and
// an array holding each element's key, as the prelude's wrappers build it.
func byKeys(r *runtime.Registry, name string, f func(ctx *runtime.Ctx, elems, keys []*value.Value) *value.Value) {
	binary(r, name, func(ctx *runtime.Ctx, in, keys *value.Value) (*value.Value, error) {
		if err := sortable(ctx, in); err != nil {
			return nil, err
		}
		if err := sortable(ctx, keys); err != nil {
			return nil, err
		}
		return f(ctx, in.Elems(), keys.Elems()), nil
	})
}

// sortable checks that v is an array whose elements may be compared,
// which rules out a number past the bounds under exact numbers.
func sortable(ctx *runtime.Ctx, v *value.Value) error {
	if v.Kind() != value.KindArray {
		return fail("%s cannot be sorted, as it is not an array", describe(v))
	}
	for _, e := range v.Elems() {
		if err := ctx.CheckNumbers(e); err != nil {
			return err
		}
	}
	return nil
}

// sortedBy returns elems stably sorted by their keys.
func sortedBy(ctx *runtime.Ctx, elems, keys []*value.Value) []*value.Value {
	order := make([]int, len(elems))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return ctx.Compare(keys[a], keys[b]) })
	out := make([]*value.Value, len(order))
	for i, j := range order {
		out[i] = elems[j]
	}
	return out
}

// groups sorts elems by their keys and splits them where the key changes.
// With first set it keeps the first element of each group, which is
// unique_by, and otherwise it keeps every group, which is group_by.
func groups(ctx *runtime.Ctx, elems, keys []*value.Value, first bool) *value.Value {
	order := make([]int, len(elems))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return ctx.Compare(keys[a], keys[b]) })
	out := value.NewArrBuilder(0)
	var group *value.ArrBuilder
	for n, i := range order {
		if n == 0 || ctx.Compare(keys[order[n-1]], keys[i]) != 0 {
			if group != nil {
				out.Append(group.Seal())
			}
			if first {
				out.Append(elems[i])
				group = nil
				continue
			}
			group = value.NewArrBuilder(1)
		}
		if group != nil {
			group.Append(elems[i])
		}
	}
	if group != nil {
		out.Append(group.Seal())
	}
	return out.Seal()
}

// extreme returns the element with the least key, the first of several,
// or with greatest set the greatest, the last of several, and null for
// none.
func extreme(ctx *runtime.Ctx, elems, keys []*value.Value, greatest bool) *value.Value {
	best := -1
	for i := range elems {
		if best < 0 {
			best = i
			continue
		}
		c := ctx.Compare(keys[i], keys[best])
		if (greatest && c >= 0) || (!greatest && c < 0) {
			best = i
		}
	}
	if best < 0 {
		return value.Null()
	}
	return elems[best]
}

// indices finds where x occurs in the input: as a substring of a string,
// counted in codepoints, as a run of elements when x is an array, or as an
// element. A null input gives null.
func indices(ctx *runtime.Ctx, in, x *value.Value) (*value.Value, error) {
	switch {
	case in.Kind() == value.KindNull:
		return value.Null(), nil
	case in.Kind() == value.KindString && x.Kind() == value.KindString:
		return stringIndices(in.Str(), x.Str()), nil
	case in.Kind() == value.KindArray && x.Kind() == value.KindArray:
		return indicesOf(in.Elems(), x.Elems(), ctx.Equal), nil
	case in.Kind() == value.KindArray:
		return indicesOf(in.Elems(), []*value.Value{x}, ctx.Equal), nil
	}
	return Index(in, x)
}

// stringIndices returns the codepoint offsets where sub starts in s,
// overlaps included.
func stringIndices(s, sub string) *value.Value {
	b := value.NewArrBuilder(0)
	if sub == "" {
		return b.Seal()
	}
	runes := 0
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], sub) {
			b.Append(value.Num(float64(runes)))
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		runes++
	}
	return b.Seal()
}
