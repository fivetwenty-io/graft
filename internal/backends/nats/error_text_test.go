package natsbackend

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/fivetwenty-io/graft/internal/errtext"
)

// errProbe names every store, key, bucket, and object in these tests,
// and is planted in the data they fail to parse. No Error() text may
// hold it, and the detailed text keeps it.
const errProbe = "probezq81x"

// fakeJetStream serves one KV store and one object store from memory.
// Methods it does not override panic through the nil interface.
type fakeJetStream struct {
	jetstream.JetStream
	kvErr  error
	objErr error
	obj    *fakeObjectStore
}

func (f *fakeJetStream) KeyValue(context.Context, string) (jetstream.KeyValue, error) {
	return nil, f.kvErr
}

func (f *fakeJetStream) ObjectStore(context.Context, string) (jetstream.ObjectStore, error) {
	if f.objErr != nil {
		return nil, f.objErr
	}
	return f.obj, nil
}

// fakeObjectStore holds a single object with the given content type.
type fakeObjectStore struct {
	jetstream.ObjectStore
	contentType string
	data        []byte
}

func (f *fakeObjectStore) GetInfo(_ context.Context, name string, _ ...jetstream.GetObjectInfoOpt) (*jetstream.ObjectInfo, error) {
	return &jetstream.ObjectInfo{
		ObjectMeta: jetstream.ObjectMeta{Name: name, Headers: nats.Header{"Content-Type": []string{f.contentType}}},
		Size:       uint64(len(f.data)),
	}, nil
}

func (f *fakeObjectStore) GetBytes(context.Context, string, ...jetstream.GetObjectOpt) ([]byte, error) {
	return f.data, nil
}

// assertPlainText checks that err's Error() text, and the text of every
// error on its Unwrap chain, holds neither the probe nor a source line,
// and that its detailed text holds the probe.
func assertPlainText(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatal("the call must fail")
	}
	if err.Error() != want {
		t.Errorf("Error() is %q, want %q", err.Error(), want)
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		text := e.Error()
		if strings.Contains(text, errProbe) || strings.Contains(text, "\n") {
			t.Errorf("a %T on the chain quotes the data:\n%s", e, text)
		}
	}
	if detail := errtext.Detail(err); !strings.Contains(detail, errProbe) {
		t.Errorf("the detailed text lost the name or data:\n%s", detail)
	}
}

func TestParsePathStoreTypeErrorIsPlain(t *testing.T) {
	_, _, err := ParsePath(errProbe + ":a/b")
	assertPlainText(t, err, "invalid store type, must be 'kv' or 'obj'")
}

func TestFetchFromKVErrorIsPlain(t *testing.T) {
	js := &fakeJetStream{kvErr: jetstream.ErrBucketNotFound}
	_, err := FetchFromKV(js, errProbe+"/"+errProbe, &Config{})
	assertPlainText(t, err, "failed to get the key from the KV store after 1 attempts: nats: bucket not found")
	if !errors.Is(err, jetstream.ErrBucketNotFound) {
		t.Errorf("errors.Is(ErrBucketNotFound) is false for %v", err)
	}
}

func TestFetchFromObjectErrorIsPlain(t *testing.T) {
	js := &fakeJetStream{objErr: jetstream.ErrBucketNotFound}
	_, err := FetchFromObject(js, errProbe+"/"+errProbe, &Config{})
	assertPlainText(t, err, "failed to get the object from the bucket after 1 attempts: nats: bucket not found")
	if !errors.Is(err, jetstream.ErrBucketNotFound) {
		t.Errorf("errors.Is(ErrBucketNotFound) is false for %v", err)
	}
}

func TestFetchFromObjectParseErrorIsPlain(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
	}{
		{contentType: "text/yaml", want: "failed to parse YAML from the object"},
		{contentType: "application/json", want: "failed to parse JSON from the object"},
	}
	for _, tc := range tests {
		t.Run(tc.contentType, func(t *testing.T) {
			obj := &fakeObjectStore{contentType: tc.contentType, data: []byte("a: [" + errProbe + "\nb: c\n")}
			_, err := FetchFromObject(&fakeJetStream{obj: obj}, "bucket/"+errProbe, &Config{StreamingThreshold: -1})
			assertPlainText(t, err, tc.want)
		})
	}
}
