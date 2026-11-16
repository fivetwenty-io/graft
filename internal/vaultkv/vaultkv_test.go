package vaultkv_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	api "github.com/hashicorp/vault/api"

	"github.com/fivetwenty-io/graft/internal/errtext"
	"github.com/fivetwenty-io/graft/internal/vaultkv"
	"github.com/fivetwenty-io/graft/internal/vaultkv/vaultkvtest"
)

func newReader(t *testing.T, addr string) *vaultkv.Reader {
	t.Helper()
	cfg := api.DefaultConfig()
	cfg.Address = addr
	client, err := api.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	client.SetToken("t")
	return vaultkv.New(client, nil)
}

func twoMounts(t *testing.T) *vaultkvtest.Fake {
	t.Helper()
	return vaultkvtest.New(t, map[string]int{"kv1/": 1, "kv2/": 2}, map[string]map[string]interface{}{
		"kv1/app/db": {"password": "v1-password"},
		"kv2/app/db": {"password": "v2-password"},
	})
}

// TestReadKV1AndKV2 reads whole secrets from both mount versions, by
// logical path, with a leading slash, and with data/ spelled out, and
// checks each mount is looked up once and sys/mounts never.
func TestReadKV1AndKV2(t *testing.T) {
	fake := twoMounts(t)
	r := newReader(t, fake.URL)

	cases := []struct {
		path, want, mount string
		kv2               bool
	}{
		{"kv1/app/db", "v1-password", "kv1/", false},
		{"kv2/app/db", "v2-password", "kv2/", true},
		{"/kv2/app/db", "v2-password", "kv2/", true},
		{"kv2/data/app/db", "v2-password", "kv2/", true},
		{"/kv1/app/db", "v1-password", "kv1/", false},
	}
	for _, tc := range cases {
		res, err := r.Read(context.Background(), tc.path)
		if err != nil {
			t.Fatalf("Read(%s): %v", tc.path, err)
		}
		if len(res.Data) != 1 || res.Data["password"] != tc.want {
			t.Errorf("Read(%s).Data = %v, want password=%s", tc.path, res.Data, tc.want)
		}
		if res.Mount != tc.mount || !res.Detected || res.KV2 != tc.kv2 {
			t.Errorf("Read(%s) = mount %q detected %v kv2 %v", tc.path, res.Mount, res.Detected, res.KV2)
		}
	}

	if n := fake.MountLookups(); n != 2 {
		t.Errorf("mount lookups = %d, want one per mount", n)
	}
	if n := fake.SysMountsHits(); n != 0 {
		t.Errorf("sys/mounts was requested %d times", n)
	}
	want := []string{"kv1/app/db", "kv2/data/app/db", "kv2/data/app/db", "kv2/data/app/db", "kv1/app/db"}
	got := fake.Reads()
	if len(got) != len(want) {
		t.Fatalf("reads = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("read %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestReadMissingOnDetectedMount checks that a missing secret on a
// detected mount gives nil Data and that NotFound leaves the caller's
// error alone.
func TestReadMissingOnDetectedMount(t *testing.T) {
	r := newReader(t, twoMounts(t).URL)

	res, err := r.Read(context.Background(), "kv2/app/missing")
	if err != nil || res.Data != nil {
		t.Fatalf("Read = %v, %v; want nil Data and no error", res.Data, err)
	}
	sentinel := errors.New("not found")
	if got := res.NotFound(sentinel); got != sentinel { //nolint:errorlint // identity is the point
		t.Errorf("NotFound wrapped the error of a detected mount: %v", got)
	}
}

// TestReadDeniedLookupFallsBackToKV1 denies the mounts lookup with a
// 403. A KV v1 secret still reads, and a KV v2 secret, read as KV v1, is
// missing with an error that names the mount and the fallback and still
// matches the caller's own error.
func TestReadDeniedLookupFallsBackToKV1(t *testing.T) {
	fake := twoMounts(t)
	fake.DenyMountLookups()
	r := newReader(t, fake.URL)

	res, err := r.Read(context.Background(), "kv1/app/db")
	if err != nil || res.Data["password"] != "v1-password" {
		t.Fatalf("KV v1 read = %v, %v", res.Data, err)
	}
	if res.Detected || res.Mount != "kv1/" {
		t.Errorf("KV v1 read = mount %q detected %v, want kv1/ undetected", res.Mount, res.Detected)
	}

	res, err = r.Read(context.Background(), "kv2/app/db")
	if err != nil || res.Data != nil {
		t.Fatalf("KV v2 read as v1 = %v, %v; want not found", res.Data, err)
	}
	sentinel := errors.New("backend: path not found")
	inner := errtext.WithPlain(errors.New("backend: path not found: kv2/app/db:password"), sentinel.Error())
	wrapped := res.NotFound(errors.Join(inner, sentinel))

	if !errors.Is(wrapped, sentinel) {
		t.Error("errors.Is lost the caller's sentinel")
	}
	var undetected *vaultkv.UndetectedError
	if !errors.As(wrapped, &undetected) || undetected.Mount != "kv2/" {
		t.Fatalf("errors.As found no *UndetectedError for kv2/: %v", wrapped)
	}
	if got := wrapped.Error(); got != "KV version undetected, read as KV v1: backend: path not found\nbackend: path not found" {
		t.Errorf("Error() = %q", got)
	}
	if got := errtext.Detail(wrapped); got != `mount "kv2/": KV version undetected, read as KV v1: backend: path not found: kv2/app/db:password`+"\nbackend: path not found" {
		t.Errorf("Detail = %q", got)
	}

	// The failed lookup is cached as the fallback, once per mount.
	_, _ = r.Read(context.Background(), "kv2/app/other")
	if n := fake.MountLookups(); n != 2 {
		t.Errorf("mount lookups = %d, want one per mount", n)
	}
	if n := fake.SysMountsHits(); n != 0 {
		t.Errorf("sys/mounts was requested %d times", n)
	}
}

// TestReadTransientLookupFailureIsNotCached fails the first mount
// lookup with a 503. That read falls back to KV v1 undetected, but the
// fallback is not cached, so the next read looks the mount up again,
// finds KV v2, and reads the secret through data/.
func TestReadTransientLookupFailureIsNotCached(t *testing.T) {
	fake := twoMounts(t)
	fake.FailMountLookups(1)
	r := newReader(t, fake.URL)
	// The client retries a 5xx on its own; with retries off, the one
	// failure reaches the reader.
	r.Client().SetMaxRetries(0)

	res, err := r.Read(context.Background(), "kv2/app/db")
	if err != nil || res.Data != nil || res.Detected {
		t.Fatalf("first read = %v, detected %v, %v; want undetected not found", res.Data, res.Detected, err)
	}

	res, err = r.Read(context.Background(), "kv2/app/db")
	if err != nil || res.Data["password"] != "v2-password" || !res.Detected {
		t.Fatalf("second read = %v, detected %v, %v; want the KV v2 secret", res.Data, res.Detected, err)
	}
	if n := fake.MountLookups(); n != 2 {
		t.Errorf("mount lookups = %d, want 2", n)
	}

	_, _ = r.Read(context.Background(), "kv2/app/other")
	if n := fake.MountLookups(); n != 2 {
		t.Errorf("mount lookups after a detected read = %d, want 2", n)
	}
}

// TestReadersKeepSeparateMountCaches reads secret/ on a vault where it
// is KV v1 and on one where it is KV v2, through two readers, so one
// reader's cached mount table must never answer for the other.
func TestReadersKeepSeparateMountCaches(t *testing.T) {
	v1 := vaultkvtest.New(t, map[string]int{"secret/": 1}, map[string]map[string]interface{}{"secret/app": {"k": "from-v1"}})
	v2 := vaultkvtest.New(t, map[string]int{"secret/": 2}, map[string]map[string]interface{}{"secret/app": {"k": "from-v2"}})
	r1, r2 := newReader(t, v1.URL), newReader(t, v2.URL)

	for range 2 {
		for _, c := range []struct {
			r    *vaultkv.Reader
			want string
		}{{r1, "from-v1"}, {r2, "from-v2"}} {
			res, err := c.r.Read(context.Background(), "secret/app")
			if err != nil || res.Data["k"] != c.want {
				t.Fatalf("Read = %v, %v; want k=%s", res.Data, err, c.want)
			}
		}
	}
}

// TestReadNestedMountsLongestWins mounts a KV v2 engine under a KV v1
// one. Once both are cached, a path under the inner mount must use the
// inner mount, whatever order the cache is walked in. The inner mount
// is read first: a cached outer mount covers every path under it, so an
// inner mount first met after the outer one is never looked up.
func TestReadNestedMountsLongestWins(t *testing.T) {
	fake := vaultkvtest.New(t, map[string]int{"secret/": 1, "secret/team/": 2}, map[string]map[string]interface{}{
		"secret/app":      {"k": "outer"},
		"secret/team/app": {"k": "inner"},
	})
	r := newReader(t, fake.URL)

	for range 20 {
		for _, c := range []struct{ path, want string }{{"secret/team/app", "inner"}, {"secret/app", "outer"}} {
			path, want := c.path, c.want
			res, err := r.Read(context.Background(), path)
			if err != nil || res.Data["k"] != want {
				t.Fatalf("Read(%s) = %v, %v; want k=%s", path, res.Data, err, want)
			}
		}
	}
}

// TestReadConcurrent reads both mount versions from many goroutines
// through one reader whose cache starts empty, for -race.
func TestReadConcurrent(t *testing.T) {
	r := newReader(t, twoMounts(t).URL)

	var wg sync.WaitGroup
	errs := make([]error, 64)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, want := "kv1/app/db", "v1-password"
			if i%2 == 1 {
				path, want = "kv2/app/db", "v2-password"
			}
			res, err := r.Read(context.Background(), path)
			switch {
			case err != nil:
				errs[i] = err
			case res.Data["password"] != want:
				errs[i] = errors.New("wrong value from " + path)
			}
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("read %d: %v", i, err)
		}
	}
}

// TestUnwrap pins the response shapes: a data map beside metadata is KV
// v2, and anything else is a KV v1 secret returned as it is.
func TestUnwrap(t *testing.T) {
	v2 := map[string]interface{}{"data": map[string]interface{}{"k": "v"}, "metadata": map[string]interface{}{}}
	if got, kv2 := vaultkv.Unwrap(v2); !kv2 || got["k"] != "v" {
		t.Errorf("Unwrap(v2) = %v, %v", got, kv2)
	}
	v1 := map[string]interface{}{"data": map[string]interface{}{"k": "v"}}
	if got, kv2 := vaultkv.Unwrap(v1); kv2 || got["data"] == nil {
		t.Errorf("Unwrap(v1 with a data key) = %v, %v", got, kv2)
	}
}
