package load

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func wantEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func wantErrContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one containing %q", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Errorf("error %q does not contain %q", err, sub)
	}
}

func wantKinds(t *testing.T, v *value.Value, want map[string]value.Kind) {
	t.Helper()
	for k, kind := range want {
		got, ok := v.Get(k)
		if !ok {
			t.Errorf("key %q missing", k)
			continue
		}
		if got.Kind() != kind {
			t.Errorf("key %q has kind %d, want %d", k, got.Kind(), kind)
		}
	}
}

func mustGet(v *value.Value, key string) *value.Value {
	got, ok := v.Get(key)
	if !ok {
		panic(fmt.Sprintf("key %q missing", key))
	}
	return got
}

func mustParseYAML(t *testing.T, text string) *yamlnode.Node {
	t.Helper()
	docs, err := yamlnode.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("no documents")
	}
	return docs[0]
}

func mustReadJSON(t *testing.T, text string) *value.Value {
	t.Helper()
	d, err := Open(Source{"f", strings.NewReader(text)}, 0, LoadOptions{Format: FormatJSON}).Next()
	if err != nil {
		t.Fatal(err)
	}
	return d.Value
}

func mustReadAll(t *testing.T, text string, opts LoadOptions) []Document {
	t.Helper()
	r := Open(Source{"f", strings.NewReader(text)}, 0, opts)
	var docs []Document
	for {
		d, err := r.Next()
		if errors.Is(err, io.EOF) {
			return docs
		}
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, d)
	}
}

// billionLaughs builds a document whose last anchor expands to 9^n nodes.
func billionLaughs(n int) string {
	var b strings.Builder
	b.WriteString("l0: &l0 [\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\",\"lol\"]\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "l%d: &l%d [", i, i)
		for j := 0; j < 9; j++ {
			if j > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "*l%d", i-1)
		}
		b.WriteString("]\n")
	}
	return b.String()
}
