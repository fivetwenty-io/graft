package graft

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// TestDocumentGetterErrorsIsSentinels verifies errors.Is(err, ErrNotFound),
// errors.Is(err, ErrTypeMismatch), and errors.Is(err, ErrInvalidPath) all
// succeed against errors returned from Document's Get/GetString/GetInt/
// GetInt64/GetFloat64/GetBool/GetSlice/GetMap/GetStringSlice/
// GetMapStringString/Set/Delete, covering both the *GraftError-based call
// sites and the ones that previously returned a bare fmt.Errorf.
func TestDocumentGetterErrorsIsSentinels(t *testing.T) {
	doc := NewDocument(map[string]interface{}{
		"string": "hello",
		"int":    42,
		"float":  3.14,
		"list":   []interface{}{"a", 1, true},
		"map":    map[string]interface{}{"a": "b", "n": 1},
	})

	const invalidPath = "a]" // unmatched ']' -> tree.SyntaxError

	t.Run("ErrNotFound", func(t *testing.T) {
		cases := map[string]func() error{
			"Get":                func() error { _, err := doc.Get("does.not.exist"); return err },
			"GetString":          func() error { _, err := doc.GetString("does.not.exist"); return err },
			"GetInt":             func() error { _, err := doc.GetInt("does.not.exist"); return err },
			"GetInt64":           func() error { _, err := doc.GetInt64("does.not.exist"); return err },
			"GetFloat64":         func() error { _, err := doc.GetFloat64("does.not.exist"); return err },
			"GetBool":            func() error { _, err := doc.GetBool("does.not.exist"); return err },
			"GetSlice":           func() error { _, err := doc.GetSlice("does.not.exist"); return err },
			"GetMap":             func() error { _, err := doc.GetMap("does.not.exist"); return err },
			"GetStringSlice":     func() error { _, err := doc.GetStringSlice("does.not.exist"); return err },
			"GetMapStringString": func() error { _, err := doc.GetMapStringString("does.not.exist"); return err },
			// Delete's own cursor walk (tree.Cursor.Delete) returns a bare
			// tree.NotFoundError directly for a missing key, with no
			// document.go wrapping needed: NotFoundError's own Is method
			// (added in tree/types.go) already makes it match ErrNotFound.
			"Delete does-not-exist": func() error {
				cloned := doc.Clone()
				return cloned.Delete("does.not.exist")
			},
		}
		for name, fn := range cases {
			t.Run(name, func(t *testing.T) {
				err := fn()
				if err == nil {
					t.Fatalf("%s: expected an error", name)
				}
				if !errors.Is(err, ErrNotFound) {
					t.Errorf("%s: errors.Is(%v, ErrNotFound) = false, want true", name, err)
				}
			})
		}
	})

	t.Run("ErrTypeMismatch", func(t *testing.T) {
		cases := map[string]func() error{
			"GetString on int":             func() error { _, err := doc.GetString("int"); return err },
			"GetInt on string":             func() error { _, err := doc.GetInt("string"); return err },
			"GetInt on non-whole float":    func() error { _, err := doc.GetInt("float"); return err },
			"GetInt64 on string":           func() error { _, err := doc.GetInt64("string"); return err },
			"GetInt64 on non-whole float":  func() error { _, err := doc.GetInt64("float"); return err },
			"GetFloat64 on string":         func() error { _, err := doc.GetFloat64("string"); return err },
			"GetBool on string":            func() error { _, err := doc.GetBool("string"); return err },
			"GetSlice on string":           func() error { _, err := doc.GetSlice("string"); return err },
			"GetMap on string":             func() error { _, err := doc.GetMap("string"); return err },
			"GetStringSlice on string":     func() error { _, err := doc.GetStringSlice("string"); return err },
			"GetStringSlice on mixed list": func() error { _, err := doc.GetStringSlice("list"); return err },
			"GetMapStringString on string": func() error { _, err := doc.GetMapStringString("string"); return err },
			"GetMapStringString mixed map": func() error { _, err := doc.GetMapStringString("map"); return err },
		}
		for name, fn := range cases {
			t.Run(name, func(t *testing.T) {
				err := fn()
				if err == nil {
					t.Fatalf("%s: expected an error", name)
				}
				if !errors.Is(err, ErrTypeMismatch) {
					t.Errorf("%s: errors.Is(%v, ErrTypeMismatch) = false, want true", name, err)
				}
			})
		}
	})

	t.Run("ErrInvalidPath", func(t *testing.T) {
		cases := map[string]func() error{
			"Get":    func() error { _, err := doc.Get(invalidPath); return err },
			"Set":    func() error { return doc.Clone().Set(invalidPath, "x") },
			"Delete": func() error { return doc.Clone().Delete(invalidPath) },
			// The Get*/checked-getter family all delegate to Get first,
			// so an invalid path surfaces identically through them.
			"GetString": func() error { _, err := doc.GetString(invalidPath); return err },
		}
		for name, fn := range cases {
			t.Run(name, func(t *testing.T) {
				err := fn()
				if err == nil {
					t.Fatalf("%s: expected an error", name)
				}
				if !errors.Is(err, ErrInvalidPath) {
					t.Errorf("%s: errors.Is(%v, ErrInvalidPath) = false, want true", name, err)
				}
			})
		}
	})

	t.Run("sentinels do not cross-match", func(t *testing.T) {
		_, err := doc.GetString("int") // ErrTypeMismatch
		if errors.Is(err, ErrNotFound) {
			t.Error("expected a type-mismatch error to not match ErrNotFound")
		}
		if errors.Is(err, ErrInvalidPath) {
			t.Error("expected a type-mismatch error to not match ErrInvalidPath")
		}
	})

	t.Run("graft sentinels are the tree package sentinels", func(t *testing.T) {
		// pkg/graft/errors.go re-exports these; they must be the exact
		// same values tree.NotFoundError.Is/TypeMismatchError.Is/
		// SyntaxError.Is compare against, not lookalikes with the same
		// text (see tree/types.go for why: an import cycle rules out
		// tree referencing pkg/graft directly).
		if ErrNotFound == nil || ErrTypeMismatch == nil || ErrInvalidPath == nil {
			t.Fatal("sentinel values must not be nil")
		}
	})
}

// TestDocumentGetterErrorStringsUnchanged pins the exact Error() output of
// every touched getter/setter constructor verbatim. The genesis
// compatibility contract requires these strings never change; this table
// exists independently of cmd/graft/genesis_contract_pin_test.go (which
// covers CLI stderr, not the library Document API) so a future edit to
// this package's error wrapping cannot silently alter Document's error
// text.
func TestDocumentGetterErrorStringsUnchanged(t *testing.T) {
	// NotFoundError.Error() goes through ansi.Sprintf, which consults the
	// package-level ansi.colorEnabled flag (defaults to true, but other
	// tests in this binary toggle it via ansi.Color and some do not
	// restore it). Pin our own value so this test's expectations do not
	// depend on what ran before it in the same process.
	prevColor := ansi.IsColorEnabled()
	ansi.Color(true)
	t.Cleanup(func() { ansi.Color(prevColor) })

	doc := NewDocument(map[string]interface{}{
		"string": "hello",
		"int":    42,
		"float":  3.14,
		"list":   []interface{}{"a", 1, true},
		"map":    map[string]interface{}{"a": "b", "n": 1},
	})

	cases := []struct {
		name string
		fn   func() error
		want string
	}{
		{
			name: "Get invalid path",
			fn:   func() error { _, err := doc.Get("a]"); return err },
			want: "validation_error: invalid path 'a]': syntax error: unexpected ']' at position 1",
		},
		{
			// tree.Cursor.Resolve stops at the first missing segment, so
			// NotFoundError.Path is ["does"], not the full requested
			// path; and its Error() carries ansi color codes
			// unconditionally (ansi.Sprintf, not ansi.PSprintf), both
			// pre-existing behavior this change does not alter.
			name: "Get not found",
			fn:   func() error { _, err := doc.Get("does.not.exist"); return err },
			want: "evaluation_error at does.not.exist: path not found: \x1b[31m`\x1b[0m\x1b[36m$.does\x1b[0m\x1b[31m` could not be found in the datastructure\x1b[0m",
		},
		{
			name: "GetString wrong type",
			fn:   func() error { _, err := doc.GetString("int"); return err },
			want: "validation_error: value at path 'int' is not a string (got int)",
		},
		{
			name: "GetInt wrong type",
			fn:   func() error { _, err := doc.GetInt("string"); return err },
			want: "validation_error: value at path 'string' is not a number (got string)",
		},
		{
			name: "GetInt non-whole float",
			fn:   func() error { _, err := doc.GetInt("float"); return err },
			want: "validation_error: value at path 'float' is not a whole number (got 3.140000)",
		},
		{
			name: "GetBool wrong type",
			fn:   func() error { _, err := doc.GetBool("string"); return err },
			want: "validation_error: value at path 'string' is not a boolean (got string)",
		},
		{
			name: "GetSlice wrong type",
			fn:   func() error { _, err := doc.GetSlice("string"); return err },
			want: "validation_error: value at path 'string' is not a slice (got string)",
		},
		{
			name: "GetMap wrong type",
			fn:   func() error { _, err := doc.GetMap("string"); return err },
			want: "validation_error: value at path 'string' is not a map (got string)",
		},
		{
			name: "GetInt64 float not whole",
			fn:   func() error { _, err := doc.GetInt64("float"); return err },
			want: "value at path float is a float, not an integer",
		},
		{
			name: "GetInt64 wrong type",
			fn:   func() error { _, err := doc.GetInt64("string"); return err },
			want: "value at path string is not an integer (got string)",
		},
		{
			name: "GetFloat64 wrong type",
			fn:   func() error { _, err := doc.GetFloat64("string"); return err },
			want: "value at path string is not a number (got string)",
		},
		{
			name: "GetStringSlice wrong type",
			fn:   func() error { _, err := doc.GetStringSlice("string"); return err },
			want: "value at path string is not a slice (got string)",
		},
		{
			name: "GetStringSlice mixed elements",
			fn:   func() error { _, err := doc.GetStringSlice("list"); return err },
			want: "item at index 1 in slice at path list is not a string (got int)",
		},
		{
			name: "GetMapStringString wrong type",
			fn:   func() error { _, err := doc.GetMapStringString("string"); return err },
			want: "value at path string is not a map (got string)",
		},
		{
			name: "GetMapStringString mixed values",
			fn:   func() error { _, err := doc.GetMapStringString("map"); return err },
			want: "map at path map contains non-string value for key n: 1",
		},
		{
			name: "Set root non-map",
			fn:   func() error { return doc.Clone().Set("$", "x") },
			want: "validation_error: cannot set root to non-map value",
		},
		{
			name: "Delete root",
			fn:   func() error { return doc.Clone().Delete("$") },
			want: "validation_error: cannot delete root",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.fn()
			if err == nil {
				t.Fatalf("%s: expected an error", c.name)
			}
			if got := err.Error(); got != c.want {
				t.Errorf("%s: Error() = %q, want %q", c.name, got, c.want)
			}
		})
	}
}
