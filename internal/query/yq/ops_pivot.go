package yq

import (
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// registerPivots adds the operators that rebuild a collection by its
// children: pivot, which turns rows into columns, and array_to_map.
func registerPivots(r *runtime.Registry) {
	r.RegisterSingle("pivot", 0, pivot)
	r.RegisterSingle("array_to_map", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		switch in.Kind() {
		case value.KindObject:
			return emit(in, nil)
		case value.KindArray:
		default:
			return emit(value.ObjEntries(), nil)
		}
		entries := make([]value.Entry, 0, in.Len())
		for i, e := range in.Elems() {
			if tagOf(e) == "!!null" {
				continue
			}
			entries = append(entries, value.Entry{Key: value.Num(float64(i)), Value: e})
		}
		return emit(value.ObjEntries(entries...), nil)
	})
}

// pivot turns a sequence of sequences, or of maps, on its side. Row i of
// the result holds the ith element of each input sequence, or the values
// of each key from each input map, with null where one is missing.
func pivot(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
	if in.Kind() != value.KindArray {
		return &query.RuntimeError{Msg: "cannot pivot node of type " + tagOf(in)}
	}
	rows := in.Elems()
	if len(rows) == 0 {
		return &query.RuntimeError{Msg: "can only pivot elements of !!seq or !!map types, received "}
	}
	first := tagOf(rows[0])
	if first != "!!seq" && first != "!!map" {
		return &query.RuntimeError{Msg: "can only pivot elements of !!seq or !!map types, received " + first}
	}
	for _, r := range rows[1:] {
		if t := tagOf(r); t != first {
			return &query.RuntimeError{Msg: "sequence contains elements of " + first + " and " + t + " types"}
		}
	}
	if first == "!!seq" {
		width := 0
		for _, r := range rows {
			width = max(width, r.Len())
		}
		cols := make([]*value.Value, width)
		for j := range cols {
			col := make([]*value.Value, len(rows))
			for i, r := range rows {
				col[i] = emptyNull()
				if e, ok := r.Index(j); ok {
					col[i] = e
				}
			}
			cols[j] = value.Arr(col...)
		}
		return emit(value.Arr(cols...), nil)
	}
	var keys []*value.Value
	seen := map[string]bool{}
	for _, r := range rows {
		for _, e := range r.Entries() {
			if t := numberOrText(e.Key); !seen[t] {
				seen[t] = true
				keys = append(keys, e.Key)
			}
		}
	}
	entries := make([]value.Entry, len(keys))
	for j, k := range keys {
		col := make([]*value.Value, len(rows))
		for i, r := range rows {
			col[i] = emptyNull()
			if e, ok := r.Get(numberOrText(k)); ok {
				col[i] = e
			}
		}
		entries[j] = value.Entry{Key: k, Value: value.Arr(col...)}
	}
	return emit(value.ObjEntries(entries...), nil)
}

// numberOrText is a map key as the map looks it up: its text, or for a
// number the literal it was written as.
func numberOrText(k *value.Value) string {
	if k.Kind() == value.KindNumber {
		return numberText(k)
	}
	return k.Str()
}
