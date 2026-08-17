package vault

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	api "github.com/hashicorp/vault/api"
)

func newTestReader(t *testing.T, url string) VaultReader {
	t.Helper()
	cfg := api.DefaultConfig()
	cfg.Address = url
	client, err := api.NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to build vault client: %v", err)
	}
	client.SetToken("test-token")
	return NewVaultReader(client)
}

// TestReadSecretKV2MountDetection pins the spruce-parity behavior against a
// KV v2 mount: a logical path like secret/foo must be read via
// /v1/secret/data/foo, discovered through /v1/sys/internal/ui/mounts/<path>,
// and a leading slash on the logical path must be tolerated.
func TestReadSecretKV2MountDetection(t *testing.T) {
	var mountCalls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/sys/internal/ui/mounts/"):
			atomic.AddInt64(&mountCalls, 1)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"data":{"path":"secret/","type":"kv","options":{"version":"2"}}}`))
		case r.URL.Path == "/v1/secret/data/config/env/thing":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"data":{"data":{"bucket":"my-bucket"},"metadata":{"version":1}}}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"errors":[]}`))
		}
	}))
	defer srv.Close()

	reader := newTestReader(t, srv.URL)

	secret, err := reader.ReadSecret(context.Background(), "secret/config/env/thing")
	if err != nil {
		t.Fatalf("expected KV v2 read to succeed, got error: %v", err)
	}
	if secret["bucket"] != "my-bucket" {
		t.Fatalf("expected bucket=my-bucket, got %v", secret)
	}

	// Leading slash on the logical path must not break mount detection or
	// the read itself (spruce tolerates "/secret/...").
	secret, err = reader.ReadSecret(context.Background(), "/secret/config/env/thing")
	if err != nil {
		t.Fatalf("expected leading-slash KV v2 read to succeed, got error: %v", err)
	}
	if secret["bucket"] != "my-bucket" {
		t.Fatalf("expected bucket=my-bucket, got %v", secret)
	}

	// Mount version discovery must be cached per mount, not re-queried per
	// secret read.
	if atomic.LoadInt64(&mountCalls) != 1 {
		t.Fatalf("expected exactly 1 mounts lookup (cached afterwards), got %d", mountCalls)
	}
}

// TestReadSecretPathVariants pins two path-handling behaviors: a caller
// that already wrote the KV v2 data/ prefix into the path must not have it
// doubled, and a vault without the mounts introspection endpoint (or a KV
// v1 mount) is read straight at the logical path.
func TestReadSecretPathVariants(t *testing.T) {
	cases := []struct {
		name        string
		mountStatus int
		mountBody   string
		dataPath    string
		dataBody    string
		readPath    string
		wantKey     string
		wantVal     string
	}{
		{
			name:        "explicit data/ prefix on a KV v2 mount is not doubled",
			mountStatus: 200,
			mountBody:   `{"data":{"path":"secret/","type":"kv","options":{"version":"2"}}}`,
			dataPath:    "/v1/secret/data/thing",
			dataBody:    `{"data":{"data":{"key":"val"},"metadata":{"version":3}}}`,
			readPath:    "secret/data/thing",
			wantKey:     "key",
			wantVal:     "val",
		},
		{
			name:        "no mounts endpoint falls back to a direct KV v1 read",
			mountStatus: 404,
			mountBody:   `{"errors":["unsupported path"]}`,
			dataPath:    "/v1/secret/legacy",
			dataBody:    `{"data":{"key":"legacy-val"}}`,
			readPath:    "secret/legacy",
			wantKey:     "key",
			wantVal:     "legacy-val",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasPrefix(r.URL.Path, "/v1/sys/internal/ui/mounts/"):
					w.WriteHeader(tc.mountStatus)
					_, _ = w.Write([]byte(tc.mountBody))
				case r.URL.Path == tc.dataPath:
					w.WriteHeader(200)
					_, _ = w.Write([]byte(tc.dataBody))
				default:
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"errors":[]}`))
				}
			}))
			defer srv.Close()

			reader := newTestReader(t, srv.URL)
			secret, err := reader.ReadSecret(context.Background(), tc.readPath)
			if err != nil {
				t.Fatalf("expected read of %q to succeed, got error: %v", tc.readPath, err)
			}
			if secret[tc.wantKey] != tc.wantVal {
				t.Fatalf("expected %s=%s, got %v", tc.wantKey, tc.wantVal, secret)
			}
		})
	}
}

// TestReadSecretKV2NotFound pins the not-found error shape on a KV v2
// mount: the error must carry the caller's original logical path, not the
// rewritten data/ path.
func TestReadSecretKV2NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/sys/internal/ui/mounts/"):
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"data":{"path":"secret/","type":"kv","options":{"version":"2"}}}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"errors":[]}`))
		}
	}))
	defer srv.Close()

	reader := newTestReader(t, srv.URL)
	_, err := reader.ReadSecret(context.Background(), "secret/missing/thing")
	if err == nil {
		t.Fatalf("expected not-found error")
	}
	var nf *ErrNotFound
	if !errors.As(err, &nf) {
		t.Fatalf("expected *ErrNotFound, got %T: %v", err, err)
	}
	if nf.Path != "secret/missing/thing" {
		t.Fatalf("expected original logical path in error, got %q", nf.Path)
	}
}
