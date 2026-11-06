package natsbackend

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/fivetwenty-io/graft/internal/errtext"
	"github.com/fivetwenty-io/graft/internal/yamlcodec"
)

// dataProbe is planted in values these tests fail to decode. Unlike
// errProbe, which also names the store and key, it appears only in the
// data, so no error text may hold it, plain or detailed.
const dataProbe = "hunter2-s3cr3t"

// fakeKVStore serves one entry from memory. Methods it does not override
// panic through the nil interface.
type fakeKVStore struct {
	jetstream.KeyValue
	value []byte
}

func (f *fakeKVStore) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	return &fakeKVEntry{key: key, value: f.value}, nil
}

type fakeKVEntry struct {
	jetstream.KeyValueEntry
	key   string
	value []byte
}

func (e *fakeKVEntry) Key() string   { return e.key }
func (e *fakeKVEntry) Value() []byte { return e.value }

// fakeKVJetStream serves a single KV store holding value.
type fakeKVJetStream struct {
	jetstream.JetStream
	kv *fakeKVStore
}

func (f *fakeKVJetStream) KeyValue(context.Context, string) (jetstream.KeyValue, error) {
	return f.kv, nil
}

// TestDecodeGuardedReadsLikeAMerge checks that a value from a NATS store
// decodes the way a merge document does, with YAML 1.1 booleans and
// octal numbers, only the first document, and JSON in any of its usual
// layouts.
func TestDecodeGuardedReadsLikeAMerge(t *testing.T) {
	cases := []struct {
		data string
		want any
	}{
		{"name: web\nports:\n- 80\n- 443\n---\nname: other\n", map[string]any{"name": "web", "ports": []any{80, 443}}},
		{"enabled: yes\nmode: 010\n", map[string]any{"enabled": true, "mode": 8}},
		{"quoted: \"yes\"\n", map[string]any{"quoted": "yes"}},
		{"a: &x [1, 2]\nb: *x\n", map[string]any{"a": []any{1, 2}, "b": []any{1, 2}}},
		{`{"a":1,"b":"yes","c":[1.5,null,true]}`, map[string]any{"a": 1, "b": "yes", "c": []any{1.5, nil, true}}},
		{"{\n\t\"a\": 1,\n\t\"b\": [\n\t\t\"x\"\n\t]\n}\n", map[string]any{"a": 1, "b": []any{"x"}}},
		{"[1, \"two\", {\"three\": 3}]", []any{1, "two", map[string]any{"three": 3}}},
		{"plain text", "plain text"},
	}
	for _, tc := range cases {
		got, err := decodeGuarded([]byte(tc.data))
		if err != nil {
			t.Errorf("decodeGuarded(%q): %v", tc.data, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("decodeGuarded(%q) = %#v, want %#v", tc.data, got, tc.want)
		}
	}
}

// TestDecodeGuardedRejectsDeepNesting feeds the guard a value that nests
// 20,000 flow sequences deep, which goccy's decoder alone spends close
// to a gigabyte on. The guard has to refuse it, and quickly.
func TestDecodeGuardedRejectsDeepNesting(t *testing.T) {
	data := []byte("a: " + strings.Repeat("[", 20000))
	start := time.Now()
	got, err := decodeGuarded(data)
	if err == nil {
		t.Fatalf("decodeGuarded accepted 20,000 nested [, got %T", got)
	}
	if !errors.Is(err, yamlcodec.ErrExternalValue) {
		t.Errorf("error is %T, want ErrExternalValue", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s to refuse the value, want a fast failure", elapsed)
	}
}

// TestDecodeGuardedRefusesAliasBlowup checks that a value whose aliases
// would expand past the merge's node cap fails instead of expanding.
func TestDecodeGuardedRefusesAliasBlowup(t *testing.T) {
	var b strings.Builder
	b.WriteString("a: &a [x, x, x, x, x, x, x, x, x, x]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		b.WriteString(name + ": &" + name + " [")
		for j := 0; j < 10; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*" + prev)
		}
		b.WriteString("]\n")
		prev = name
	}
	if got, err := decodeGuarded([]byte(b.String())); err == nil {
		t.Fatalf("decodeGuarded expanded an alias-heavy value to %T", got)
	}
}

// TestDecodeGuardedErrorsQuoteNoData checks that a value which fails to
// decode gives an error that names no part of it.
func TestDecodeGuardedErrorsQuoteNoData(t *testing.T) {
	for _, data := range []string{
		"a: [" + dataProbe + "\nb: c\n",
		"{\"a\": \"" + dataProbe,
		"k: " + dataProbe + "\nk: 2\n",
		"\t" + dataProbe,
	} {
		_, err := decodeGuarded([]byte(data))
		if err == nil {
			t.Errorf("decodeGuarded accepted a bad value")
			continue
		}
		if strings.Contains(err.Error(), "hunter") || strings.Contains(errtext.Detail(err), "hunter") {
			t.Errorf("the error quotes the data: %q", errtext.Detail(err))
		}
	}
}

// TestFetchFromObjectParseErrorQuotesNoData checks that an object of a
// YAML or JSON content type that fails to decode gives an error whose
// detailed text names the object but no part of its data.
func TestFetchFromObjectParseErrorQuotesNoData(t *testing.T) {
	for _, contentType := range []string{"text/yaml", "application/json"} {
		t.Run(contentType, func(t *testing.T) {
			obj := &fakeObjectStore{contentType: contentType, data: []byte("a: [" + dataProbe + "\nb: c\n")}
			_, err := FetchFromObject(&fakeJetStream{obj: obj}, "bucket/"+errProbe, &Config{StreamingThreshold: -1})
			if err == nil {
				t.Fatal("the call must fail")
			}
			detail := errtext.Detail(err)
			if strings.Contains(detail, "hunter") || strings.Contains(err.Error(), "hunter") {
				t.Errorf("the error quotes the data:\n%s", detail)
			}
			if !strings.Contains(detail, errProbe) {
				t.Errorf("the detailed text lost the object name:\n%s", detail)
			}
		})
	}
}

// TestFetchFromObjectDecodesLikeAMerge checks that a YAML object reads
// plain yes as a boolean and 010 as octal, as a merge document does.
func TestFetchFromObjectDecodesLikeAMerge(t *testing.T) {
	for _, tc := range []struct{ contentType, name string }{
		{"text/yaml", "app"},
		{"", "app.yml"},
	} {
		obj := &fakeObjectStore{contentType: tc.contentType, data: []byte("enabled: yes\nmode: 010\n")}
		got, err := FetchFromObject(&fakeJetStream{obj: obj}, "bucket/"+tc.name, &Config{StreamingThreshold: -1})
		if err != nil {
			t.Fatalf("FetchFromObject(%q): %v", tc.name, err)
		}
		if want := map[string]any{"enabled": true, "mode": 8}; !reflect.DeepEqual(got, want) {
			t.Errorf("FetchFromObject(%q) = %#v, want %#v", tc.name, got, want)
		}
	}
}

// TestFetchFromKVDecodesLikeAMerge checks the KV store's policy, which
// decodes only a value of more than one line, and that such a value
// decodes the way a merge document does.
func TestFetchFromKVDecodesLikeAMerge(t *testing.T) {
	cases := []struct {
		value string
		want  any
	}{
		{"enabled: yes\nmode: 010\n", map[string]any{"enabled": true, "mode": 8}},
		{"yes", "yes"},
		{`{"a": 1}`, `{"a": 1}`},
		{"a: [" + dataProbe + "\nb: c\n", "a: [" + dataProbe + "\nb: c\n"},
		{"first\nsecond\n", "first\nsecond\n"},
	}
	for _, tc := range cases {
		js := &fakeKVJetStream{kv: &fakeKVStore{value: []byte(tc.value)}}
		got, err := FetchFromKV(js, "store/key", &Config{})
		if err != nil {
			t.Fatalf("FetchFromKV(%q): %v", tc.value, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("FetchFromKV(%q) = %#v, want %#v", tc.value, got, tc.want)
		}
	}
}
