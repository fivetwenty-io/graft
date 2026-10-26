package yq

import (
	"errors"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// constant is an argument that gives one value.
type constant struct{ v *value.Value }

// Each emits the value.
func (c constant) Each(_ *value.Value, emit runtime.Emit) error { return emit(c.v, nil) }

// parseYAML reads text as one YAML document with its presentation, or as
// a plain scalar when scalar is set, and gives null for no document.
func parseYAML(text string, scalar bool) (*value.Value, error) {
	docs, err := parseDocuments(text)
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

// parseDocuments reads every YAML document of text with its presentation.
func parseDocuments(text string) ([]*value.Value, error) {
	r := load.Open(load.Source{Name: "", Reader: strings.NewReader(text)}, 0, load.LoadOptions{Format: load.FormatYAML, KeepMeta: true})
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
