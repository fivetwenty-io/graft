// Package vaultkvtest serves a fake Vault for tests that read KV v1 and
// KV v2 mounts. It answers sys/internal/ui/mounts/<path> the way Vault
// does, refuses sys/mounts with a 403 so no test can come to depend on
// it, and serves each mount's secrets at the API path its KV version
// uses. A KV v2 secret read without the data/ segment comes back 404, as
// it does from a real Vault.
package vaultkvtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// keyData and keyErrors are the Vault response body keys the fake
// writes.
const (
	keyData   = "data"
	keyErrors = "errors"
)

// Fake is a running fake Vault. Its URL is the address to configure.
type Fake struct {
	// URL is the fake's base address, such as "http://127.0.0.1:1234".
	URL string

	mounts  map[string]int
	secrets map[string]map[string]interface{}

	mu            sync.Mutex
	denyLookups   bool
	failLookups   int
	mountLookups  int
	sysMountsHits int
	reads         []string
}

// New starts a fake Vault that is closed when t finishes. mounts maps a
// mount path with its trailing slash, such as "secret/", to its KV
// version, 1 or 2. secrets maps a logical path, such as "secret/app",
// to the secret's data; the fake serves it at "<mount>data/<rest>" on a
// KV v2 mount and at the logical path on a KV v1 mount.
func New(t testing.TB, mounts map[string]int, secrets map[string]map[string]interface{}) *Fake {
	t.Helper()

	f := &Fake{mounts: mounts, secrets: secrets}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.URL = srv.URL

	return f
}

// DenyMountLookups makes sys/internal/ui/mounts/<path> answer 403, as
// it does for a token whose policy grants nothing on that path.
func (f *Fake) DenyMountLookups() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.denyLookups = true
}

// FailMountLookups makes the next n sys/internal/ui/mounts/<path>
// requests answer 503, as a sealed, overloaded, or restarting Vault
// does, after which lookups answer normally again.
func (f *Fake) FailMountLookups(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failLookups = n
}

// MountLookups returns how many sys/internal/ui/mounts requests the
// fake has answered.
func (f *Fake) MountLookups() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mountLookups
}

// SysMountsHits returns how many sys/mounts requests the fake has
// refused.
func (f *Fake) SysMountsHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sysMountsHits
}

// Reads returns the API paths of every secret read so far, in order.
func (f *Fake) Reads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reads...)
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/")

	switch {
	case path == "sys/mounts" || strings.HasPrefix(path, "sys/mounts/"):
		f.mu.Lock()
		f.sysMountsHits++
		f.mu.Unlock()
		writeJSON(w, http.StatusForbidden, map[string]interface{}{keyErrors: []string{"permission denied"}})
	case strings.HasPrefix(path, "sys/internal/ui/mounts/"):
		f.serveMountLookup(w, strings.TrimPrefix(path, "sys/internal/ui/mounts/"))
	default:
		f.serveRead(w, path)
	}
}

func (f *Fake) serveMountLookup(w http.ResponseWriter, logical string) {
	f.mu.Lock()
	f.mountLookups++
	deny := f.denyLookups
	fail := f.failLookups > 0
	if fail {
		f.failLookups--
	}
	f.mu.Unlock()

	if fail {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{keyErrors: []string{"unavailable"}})
		return
	}

	if deny {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{keyErrors: []string{"permission denied"}})
		return
	}

	mount, version, ok := f.mountOf(logical)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{keyErrors: []string{"no mount"}})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		keyData: map[string]interface{}{
			"path":    mount,
			"type":    "kv",
			"options": map[string]interface{}{"version": map[int]string{1: "1", 2: "2"}[version]},
		},
	})
}

func (f *Fake) serveRead(w http.ResponseWriter, path string) {
	f.mu.Lock()
	f.reads = append(f.reads, path)
	f.mu.Unlock()

	mount, version, ok := f.mountOf(path)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if version == 1 {
		data, found := f.secrets[path]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{keyData: data})
		return
	}

	rest := strings.TrimPrefix(path, mount)
	if !strings.HasPrefix(rest, "data/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	data, found := f.secrets[mount+strings.TrimPrefix(rest, "data/")]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		keyData: map[string]interface{}{
			keyData:    data,
			"metadata": map[string]interface{}{"version": 1},
		},
	})
}

// mountOf returns the longest configured mount that path falls under.
func (f *Fake) mountOf(path string) (string, int, bool) {
	best, version := "", 0
	for mount, v := range f.mounts {
		if strings.HasPrefix(path+"/", mount) && len(mount) > len(best) {
			best, version = mount, v
		}
	}
	return best, version, best != ""
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
