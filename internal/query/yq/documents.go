package yq

import (
	"errors"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// constant is an argument that gives one value.
type constant struct{ v *value.Value }

// Each emits the value.
func (c constant) Each(_ *value.Value, emit runtime.Emit) error { return emit(c.v, nil) }

// parseYAML reads text as one YAML document with its presentation, or as
// a plain scalar when scalar is set, and gives null for no document.
func parseYAML(text string, scalar bool) (*value.Value, error) {
	docs, err := parseDocuments(text, false)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return value.Null(), nil
	}
	v := docs[0]
	if scalar && (v.Kind() == value.KindArray || v.Kind() == value.KindObject || v.Kind() == value.KindString) {
		return value.Str(text), nil
	}
	return v, nil
}

// fromYAML reads text as from_yaml does: one YAML document with its
// presentation, but for comments above its first node, which yq keeps
// only as the leading content of a document it prints. Text with no node
// reads as an empty null, and no text at all fails.
func fromYAML(text string) (*value.Value, error) {
	if text == "" {
		return nil, &query.RuntimeError{Msg: "EOF"}
	}
	docs, err := parseDocuments(text, false)
	switch {
	case err != nil:
		return nil, err
	case len(docs) == 0 || docs[0].Kind() == value.KindNull && docs[0].Meta() == nil:
		return emptyNull(), nil
	default:
		return docs[0], nil
	}
}

// emptyNull is a null written as nothing at all, as in "a:".
func emptyNull() *value.Value { return value.Null().WithMeta(&value.Meta{}) }

// parseDocuments reads every YAML document of text with its presentation.
// With nodeComments set, comments above the first node stay with it, as
// yq's load keeps them, rather than going to leading content.
func parseDocuments(text string, nodeComments bool) ([]*value.Value, error) {
	opts := load.LoadOptions{Format: load.FormatYAML, KeepMeta: true, NodeComments: nodeComments}
	r := load.Open(load.Source{Name: "", Reader: strings.NewReader(text)}, 0, opts)
	var out []*value.Value
	for {
		d, err := r.Next()
		if err != nil {
			if isEOF(err) {
				return out, nil
			}
			return nil, err
		}
		out = append(out, d.Value)
	}
}

// isEOF reports whether err ends a stream of documents.
func isEOF(err error) bool { return errors.Is(err, io.EOF) }
