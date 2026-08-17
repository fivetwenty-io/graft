package graft

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/cppforlife/go-patch/patch"
	"github.com/goccy/go-yaml"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// RootIsArrayError indicates a YAML document's root value is an array
// rather than a map/hash - the shape `graft merge --go-patch` (and
// ParseFile's own always-on equivalent) retries as a go-patch operation
// list instead of failing outright. Exported, with a private message
// field, so every caller's exact wording goes through NewRootIsArrayError
// rather than duplicating this type.
type RootIsArrayError struct {
	msg string
}

// Error implements the error interface.
func (r RootIsArrayError) Error() string {
	return r.msg
}

// NewRootIsArrayError constructs a RootIsArrayError with msg as its
// Error() text. Exported so callers with their own root-type probe and
// message wording (e.g. cmd/graft's parseYAML) can still produce the one
// shared type IsArrayError recognizes, instead of defining their own.
func NewRootIsArrayError(msg string) error {
	return RootIsArrayError{msg: msg}
}

// IsArrayError reports whether err is (or wraps) a RootIsArrayError.
func IsArrayError(err error) bool {
	var rootArrayErr RootIsArrayError
	return errors.As(err, &rootArrayErr)
}

// DetectArrayRoot performs a lightweight generic YAML unmarshal of data
// solely to classify its root value, the same probe cmd/graft's
// parseOneYamlFile has always done before deciding whether to retry a
// file as go-patch. It returns nil for a map root or a blank/null
// document (neither is an array), RootIsArrayError for an array root, and
// a plain error for a YAML syntax error or any other non-map root shape
// (string, number, bool). It exists because ParseYAML's own non-map-root
// error does not distinguish "array" from other invalid root shapes, and
// a go-patch retry (ParseGoPatch) only makes sense for an array root.
func DetectArrayRoot(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	var raw interface{}
	if err := yaml.Unmarshal(QuoteInjectKeys(data), &raw); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	switch raw.(type) {
	case map[string]interface{}, nil:
		return nil
	case []interface{}:
		return RootIsArrayError{msg: "root of YAML document is not a hash/map: root is an array"}
	default:
		return fmt.Errorf("root of YAML document is not a hash/map: found %T", raw)
	}
}

// ParseGoPatch parses data as a go-patch operation list - an array of
// go-patch op definitions - the format an array-rooted document is
// retried as once DetectArrayRoot/IsArrayError signal that retry is
// worth attempting. The returned patch.Ops is normally wrapped with
// NewGoPatchDocument before being merged.
func ParseGoPatch(data []byte) (patch.Ops, error) {
	opdefs := []patch.OpDefinition{}
	if err := yaml.Unmarshal(data, &opdefs); err != nil {
		// Wording, capitalization, and the trailing newline ("Root of YAML
		// document is not a hash/map. Tried parsing it as go-patch, but
		// got: <err>\n") are pinned by cmd/graft/main_test.go's
		// `graft merge --go-patch` stderr assertion - keep them
		// byte-identical even though the rest of this package favors a
		// lowercase "root of...". This is verbatim the HEAD (064cdea)
		// cmd/graft parseGoPatch call: ansi.Errorf with a *balanced*
		// @R{...got} brace. In default/off/auto (non-TTY) mode that
		// resolves to the same plain text a plain fmt.Errorf would
		// produce, but under --color=on it renders real ANSI codes around
		// "...but got" (F13) - a plain fmt.Errorf can't reproduce that, so
		// this must stay ansi.Errorf, not be simplified to fmt.Errorf.
		return nil, ansi.Errorf("@R{Root of YAML document is not a hash/map. Tried parsing it as go-patch, but got}: %s\n", err)
	}

	ops, err := patch.NewOpsFromDefinitions(opdefs)
	if err != nil {
		// "@R{" prefix and capital "Unable" are pinned by
		// cmd/graft/main_test.go's `graft merge --go-patch` stderr
		// assertion. HEAD (064cdea) cmd/graft's parseGoPatch built this
		// with ansi.Errorf("@R{Unable to parse go-patch definitions: %s\n",
		// err) - an unbalanced brace (no closing "}") that ansi's markup
		// parser can never resolve, so it always emitted the literal
		// "@R{Unable..." text verbatim, in every color/TTY mode. That
		// literal, brace-bug-and-all text IS the observable contract; keep
		// it byte-identical, including the trailing "\n" the old format
		// string carried.
		//nolint:staticcheck,revive // capitalized "@R{Unable" is the pinned literal HEAD text, brace bug included
		return nil, fmt.Errorf("@R{Unable to parse go-patch definitions: %w\n", err)
	}
	return ops, nil
}
