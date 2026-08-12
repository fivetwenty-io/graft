package graft

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWithVaultTarget_UnconfiguredTargetIsAConfigurationError proves an
// "@target" that has no matching WithVaultTarget call fails with a
// configuration error distinct from ErrBackendNotFound - it is a setup
// mistake, not a missing secret.
func TestWithVaultTarget_UnconfiguredTargetIsAConfigurationError(t *testing.T) {
	b := newVaultOptionBackend()
	b.setConfig("", VaultConfig{Address: "http://127.0.0.1:1", Token: "t"})

	_, err := b.GetWithTarget(context.Background(), "staging", "secret/x:y")
	if err == nil {
		t.Fatal("expected an error for an unconfigured target, got nil")
	}
	if errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("expected a configuration error, not ErrBackendNotFound: %v", err)
	}
}

// TestVaultOptionBackend_GetBatch proves GetBatch (via SequentialGetBatch)
// fetches every path and omits not-found entries rather than aborting the
// whole batch.
func TestVaultOptionBackend_GetBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/secret/a":
			_, _ = w.Write([]byte(`{"data": {"k": "va"}}`))
		case "/v1/secret/b":
			_, _ = w.Write([]byte(`{"data": {"k": "vb"}}`))
		default:
			// Empty-body 404, matching real Vault's shape for a missing
			// path - see TestWithVault_NotFoundMapsToContractShape's
			// comment for why net/http's default 404 text body breaks the
			// vault SDK's JSON decode instead of reaching ErrBackendNotFound.
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	b := newVaultOptionBackend()
	b.setConfig("", VaultConfig{Address: srv.URL, Token: "t"})

	got, err := b.GetBatch(context.Background(), []string{"secret/a:k", "secret/b:k", "secret/missing:k"})
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}
	if got["secret/a:k"] != "va" || got["secret/b:k"] != "vb" {
		t.Fatalf("expected both found entries, got: %#v", got)
	}
	if _, ok := got["secret/missing:k"]; ok {
		t.Fatalf("expected the not-found path to be omitted, got: %#v", got)
	}
}

// TestVaultOptionBackend_HealthUsesDefaultTarget proves Health reports the
// error from the "" target's client construction when no WithVault default
// was configured (only a WithVaultTarget), rather than silently succeeding
// or picking an arbitrary target.
func TestVaultOptionBackend_HealthUsesDefaultTarget(t *testing.T) {
	b := newVaultOptionBackend()
	b.setConfig("staging", VaultConfig{Address: "http://127.0.0.1:1", Token: "t"})

	if err := b.Health(context.Background()); err == nil {
		t.Fatal("expected Health to fail with no default (\"\") target configured, got nil")
	}
}

// TestVaultOptionBackend_Close proves Close is a benign no-op.
func TestVaultOptionBackend_Close(t *testing.T) {
	b := newVaultOptionBackend()
	if err := b.Close(); err != nil {
		t.Fatalf("expected Close to always succeed, got: %v", err)
	}
}

// TestBuildVaultTransport_PoolSize proves VaultConfig.PoolSize's entire
// documented effect: non-positive leaves Go's http.Transport zero-value
// defaults untouched; positive sets both MaxIdleConnsPerHost and
// MaxIdleConns.
func TestBuildVaultTransport_PoolSize(t *testing.T) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		t.Fatalf("SystemCertPool failed: %v", err)
	}

	zero := buildVaultTransport(0, false, roots)
	if zero.MaxIdleConnsPerHost != 0 || zero.MaxIdleConns != 0 {
		t.Fatalf("expected PoolSize<=0 to leave the zero-value transport defaults, got MaxIdleConnsPerHost=%d MaxIdleConns=%d", zero.MaxIdleConnsPerHost, zero.MaxIdleConns)
	}

	sized := buildVaultTransport(17, false, roots)
	if sized.MaxIdleConnsPerHost != 17 || sized.MaxIdleConns != 17 {
		t.Fatalf("expected PoolSize=17 to set both pool fields to 17, got MaxIdleConnsPerHost=%d MaxIdleConns=%d", sized.MaxIdleConnsPerHost, sized.MaxIdleConns)
	}

	skip := buildVaultTransport(0, true, roots)
	if !skip.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("expected SkipVerify=true to set TLSClientConfig.InsecureSkipVerify")
	}
}

// TestBuildVaultClient_RequiresAddressAndToken proves buildVaultClient
// rejects an incomplete VaultConfig rather than silently building a client
// that will fail unhelpfully on first use.
func TestBuildVaultClient_RequiresAddressAndToken(t *testing.T) {
	if _, err := buildVaultClient(VaultConfig{Token: "t"}); err == nil {
		t.Fatal("expected an error for a missing Address, got nil")
	}
	if _, err := buildVaultClient(VaultConfig{Address: "http://127.0.0.1:1"}); err == nil {
		t.Fatal("expected an error for a missing Token, got nil")
	}
}

// TestBuildVaultClient_ExpandsEnvVars proves Address/Token/Namespace go
// through os.ExpandEnv, matching internal/backends/vault's own
// environment-expansion behavior.
func TestBuildVaultClient_ExpandsEnvVars(t *testing.T) {
	t.Setenv("C1B_VAULT_TEST_ADDR", "http://127.0.0.1:1")
	t.Setenv("C1B_VAULT_TEST_TOKEN", "expanded-token")

	client, err := buildVaultClient(VaultConfig{
		Address: "$C1B_VAULT_TEST_ADDR",
		Token:   "$C1B_VAULT_TEST_TOKEN",
	})
	if err != nil {
		t.Fatalf("buildVaultClient failed: %v", err)
	}
	if client.Token() != "expanded-token" {
		t.Fatalf("expected the expanded token to reach the client, got %q", client.Token())
	}
}

// TestSplitVaultOptionPath pins the last-colon split, matching
// internal/backends/vault.ParsePath's algorithm.
func TestSplitVaultOptionPath(t *testing.T) {
	cases := []struct {
		path       string
		wantSecret string
		wantKey    string
	}{
		{"secret/db:password", "secret/db", "password"},
		{"secret/db:with:colon:password", "secret/db:with:colon", "password"},
		{"no-colon-here", "no-colon-here", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		secret, key := splitVaultOptionPath(c.path)
		if secret != c.wantSecret || key != c.wantKey {
			t.Errorf("splitVaultOptionPath(%q) = (%q, %q), want (%q, %q)", c.path, secret, key, c.wantSecret, c.wantKey)
		}
	}
}

// TestWithVaultTarget_EmptyNameIsNoOp proves WithVaultTarget("", ...) never
// registers a target reachable through GetWithTarget("", ...) - a graft
// operator never calls GetWithTarget with an empty target (see
// TargetedBackend's doc comment), so an empty name here would otherwise be
// unreachable dead configuration.
func TestWithVaultTarget_EmptyNameIsNoOp(t *testing.T) {
	opts := &EngineOptions{}
	WithVaultTarget("", VaultConfig{Address: "http://127.0.0.1:1", Token: "t"})(opts)
	if opts.Backends != nil {
		t.Fatalf("expected WithVaultTarget(\"\", ...) to register nothing, got: %#v", opts.Backends)
	}
}

// TestVaultOptionBackendFor_ReplacesNonVaultBackend proves a "vault"-named
// Backend registered via WithBackend (not *vaultOptionBackend) is replaced
// by a later WithVault call, matching WithBackend/RegisterBackend's
// documented "last registration for a given name wins" rule.
type fakeVaultBackend struct{}

func (fakeVaultBackend) Name() string { return "vault" }
func (fakeVaultBackend) Get(context.Context, string) (interface{}, error) {
	return nil, ErrBackendNotFound
}
func (fakeVaultBackend) GetBatch(context.Context, []string) (map[string]interface{}, error) {
	return nil, nil
}
func (fakeVaultBackend) Health(context.Context) error { return nil }
func (fakeVaultBackend) Close() error                 { return nil }

func TestVaultOptionBackendFor_ReplacesNonVaultBackend(t *testing.T) {
	opts := &EngineOptions{}
	WithBackend(fakeVaultBackend{})(opts)
	WithVault(VaultConfig{Address: "http://127.0.0.1:1", Token: "t"})(opts)

	if _, ok := opts.Backends["vault"].(*vaultOptionBackend); !ok {
		t.Fatalf("expected WithVault to replace the earlier WithBackend registration, got %T", opts.Backends["vault"])
	}
}
