package yamlnode_test

import (
	"errors"
	"math"
	"time"
)

// This file is copied verbatim into graft as
// internal/yamlnode/catalog_test.go with its package line changed, so
// both sides marshal exactly the same Go values.

type catalogInner struct {
	X int
}

type catalogSample struct {
	Name    string       `yaml:"name"`
	Port    int          `yaml:"port,omitempty"`
	Tags    []string     `yaml:"tags"`
	Renamed bool         `yaml:"on"`
	Hidden  string       `yaml:"-"`
	Inner   catalogInner `yaml:",inline"`
	private int
}

type catalogText struct{ text string }

func (c catalogText) MarshalText() ([]byte, error) { return []byte(c.text), nil }

type catalogBadText struct{}

func (catalogBadText) MarshalText() ([]byte, error) {
	return nil, errors.New("text marshaling failed")
}

func valueCatalog() map[string]interface{} {
	when := time.Date(2001, 12, 14, 21, 59, 43, 100000000, time.UTC)
	return map[string]interface{}{
		"nil":              nil,
		"nil-pointer":      (*int)(nil),
		"bool":             true,
		"int":              42,
		"int8-negative":    int8(-5),
		"uint64-max":       uint64(math.MaxUint64),
		"float-1.0":        1.0,
		"float-123456.0":   123456.0,
		"float-neg-zero":   math.Copysign(0, -1),
		"float-1e8":        1e8,
		"float-0.1":        0.1,
		"float32-0.1":      float32(0.1),
		"float-inf":        math.Inf(1),
		"float-neg-inf":    math.Inf(-1),
		"float-nan":        math.NaN(),
		"string-plain":     "hello",
		"string-yes":       "yes",
		"string-on":        "on",
		"string-1":         "1",
		"string-null":      "null",
		"string-empty":     "",
		"string-merge":     "<<",
		"string-multiline": "a\nb\n",
		"string-invalid":   "\xff\xfe",
		"string-base60":    "1:20",
		"string-date":      "2001-12-14",
		"string-space":     " x",
		"string-dash":      "-x",
		"time":             when,
		"time-pointer":     &when,
		"duration":         90 * time.Second,
		"text":             catalogText{"hello"},
		"text-merge":       catalogText{"<<"},
		"text-error":       catalogBadText{},
		"map-key-order": map[string]int{
			"_u": 1, "1k": 2, "10k": 3, "B": 4, "a": 5, "a1": 6, "a01": 7, "a001": 8,
			"x-1": 9, "x-2": 10, "x-10": 11, "z9": 12, "z09": 13, "é": 14,
		},
		"map-mixed-keys": map[interface{}]interface{}{1: "a", "b": 2, true: "c", 2.5: "d"},
		"map-merge-key":  map[string]interface{}{"x": map[string]interface{}{"<<": map[string]interface{}{"a": 1}, "b": 2}},
		"slice":          []interface{}{1, "two", nil, []int{3}},
		"bytes":          []byte("hi"),
		"array":          [2]string{"a", "b"},
		"struct":         catalogSample{Name: "n", Tags: []string{"x"}, Renamed: true, Hidden: "h", Inner: catalogInner{X: 7}, private: 3},
		"empty-map":      map[string]interface{}{},
		"empty-slice":    []interface{}{},
		"chan":           make(chan int),
		"func":           func() {},
		"complex":        complex(1, 2),
	}
}
