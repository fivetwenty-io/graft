package graft_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fivetwenty-io/graft/internal/vaultca/vaultcatest"
	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // vault operator
)

const caLeakMarker = "TOPSECRET-PAYLOAD-7f3a"

// evalVaultCA evaluates `(( vault "path:password" ))` through an engine
// built with opts plus the backend registry, and returns the value.
func evalVaultCA(t *testing.T, path string, opts ...graft.EngineOption) (string, error) {
	t.Helper()

	opts = append([]graft.EngineOption{graft.WithBackendRegistry(true)}, opts...)

	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( vault "` + path + `:password" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		return "", err
	}

	got, err := evaluated.Get("secret")
	if err != nil {
		t.Fatalf("Get(secret): %v", err)
	}

	return fmt.Sprint(got), nil
}

func expectValue(t *testing.T, got string, err error, want string) {
	t.Helper()

	if err != nil {
		t.Fatalf("expected %q, got error: %v", want, err)
	}

	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func expectCertFailure(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected certificate verification to fail")
	}

	if errors.Is(err, graft.ErrVaultCA) {
		t.Fatalf("expected a TLS verification failure, got a CA config error: %v", err)
	}

	// The vault SDK flattens the TLS error into text, so match the
	// detailed message rather than a type.
	if detail := graft.DetailedError(err); !strings.Contains(detail, "x509:") {
		t.Fatalf("expected a certificate verification error, got: %s", detail)
	}
}

func TestWithVaultCATrust_EachSourceTrustsPrivateCA(t *testing.T) {
	ca := vaultcatest.NewCA(t, "private-ca")
	srv := ca.Server(t, "secret/ca-each", "password", "from-private-ca")
	dir := t.TempDir()
	vaultcatest.WriteFile(t, dir, "ca.pem", ca.PEM)
	vaultcatest.WriteFile(t, dir, "bundle/ca.pem", ca.PEM)
	t.Setenv("GRAFT_TEST_CA_DIR", dir)

	cases := []struct {
		name string
		cfg  graft.VaultConfig
	}{
		{"CACert", graft.VaultConfig{CACert: "${GRAFT_TEST_CA_DIR}/ca.pem"}},
		{"CACertBytes", graft.VaultConfig{CACertBytes: ca.PEM}},
		{"CAPath", graft.VaultConfig{CAPath: "$GRAFT_TEST_CA_DIR/bundle"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.Address, cfg.Token = srv.URL, "tok"

			got, err := evalVaultCA(t, "secret/ca-each", graft.WithVault(cfg))
			expectValue(t, got, err, "from-private-ca")
		})
	}
}

func TestWithVaultCATrust_NoCAFailsVerification(t *testing.T) {
	ca := vaultcatest.NewCA(t, "private-ca")
	srv := ca.Server(t, "secret/ca-none", "password", "unreachable")

	_, err := evalVaultCA(t, "secret/ca-none",
		graft.WithVault(graft.VaultConfig{Address: srv.URL, Token: "tok"}))
	expectCertFailure(t, err)
}

// A configured CA replaces the system roots rather than adding to them:
// trusting CA A must not trust httptest's own certificate, which CA A did
// not sign.
func TestWithVaultCATrust_ReplacesSystemRoots(t *testing.T) {
	a := vaultcatest.NewCA(t, "ca-a")
	srv := httptest.NewUnstartedServer(vaultcatest.KVHandler("secret/ca-replace", "password", "unreachable"))
	vaultcatest.Quiet(srv)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	_, err := evalVaultCA(t, "secret/ca-replace",
		graft.WithVault(graft.VaultConfig{Address: srv.URL, Token: "tok", CACertBytes: a.PEM}))
	expectCertFailure(t, err)
}

func TestWithVaultCATrust_Precedence(t *testing.T) {
	right := vaultcatest.NewCA(t, "right-ca")
	wrong := vaultcatest.NewCA(t, "wrong-ca")
	srv := right.Server(t, "secret/ca-precedence", "password", "precedence-ok")
	dir := t.TempDir()
	rightFile := vaultcatest.WriteFile(t, dir, "right.pem", right.PEM)
	wrongFile := vaultcatest.WriteFile(t, dir, "wrong.pem", wrong.PEM)
	rightDir := filepath.Dir(vaultcatest.WriteFile(t, dir, "right-dir/ca.pem", right.PEM))
	wrongDir := filepath.Dir(vaultcatest.WriteFile(t, dir, "wrong-dir/ca.pem", wrong.PEM))

	cases := []struct {
		name    string
		cfg     graft.VaultConfig
		trusted bool
	}{
		{"CACert beats wrong CACertBytes",
			graft.VaultConfig{CACert: rightFile, CACertBytes: wrong.PEM}, true},
		{"CACert beats wrong CAPath",
			graft.VaultConfig{CACert: rightFile, CAPath: wrongDir}, true},
		{"CACertBytes beats wrong CAPath",
			graft.VaultConfig{CACertBytes: right.PEM, CAPath: wrongDir}, true},
		{"wrong CACert hides right CACertBytes",
			graft.VaultConfig{CACert: wrongFile, CACertBytes: right.PEM}, false},
		{"wrong CACertBytes hides right CAPath",
			graft.VaultConfig{CACertBytes: wrong.PEM, CAPath: rightDir}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.Address, cfg.Token = srv.URL, "tok"

			got, err := evalVaultCA(t, "secret/ca-precedence", graft.WithVault(cfg))
			if tc.trusted {
				expectValue(t, got, err, "precedence-ok")
			} else {
				expectCertFailure(t, err)
			}
		})
	}
}

func TestWithVaultCATrust_BadCAFailsLoudly(t *testing.T) {
	var contacted atomic.Bool
	ca := vaultcatest.NewCA(t, "private-ca")
	kv := vaultcatest.KVHandler("secret/ca-bad", "password", "unreachable")
	srv := ca.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacted.Store(true)
		kv.ServeHTTP(w, r)
	}))

	dir := t.TempDir()
	badPEM := []byte("-----BEGIN CERTIFICATE-----\nVE9QU0VDUkVU\n" +
		"-----END CERTIFICATE-----\n" + caLeakMarker + "\n")
	badFile := vaultcatest.WriteFile(t, dir, "bad.pem", badPEM)
	missing := filepath.Join(dir, "missing.pem")
	emptyDir := filepath.Join(dir, "empty")
	if err := os.Mkdir(emptyDir, 0o750); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		cfg   graft.VaultConfig
		field string
	}{
		{"bad PEM file", graft.VaultConfig{CACert: badFile}, "VaultConfig.CACert"},
		{"missing file", graft.VaultConfig{CACert: missing}, "VaultConfig.CACert"},
		{"bad PEM bytes", graft.VaultConfig{CACertBytes: badPEM}, "VaultConfig.CACertBytes"},
		{"empty CAPath", graft.VaultConfig{CAPath: emptyDir}, "VaultConfig.CAPath"},
		{"missing CAPath", graft.VaultConfig{CAPath: missing}, "VaultConfig.CAPath"},
		{"bad CA with SkipVerify", graft.VaultConfig{CACert: badFile, SkipVerify: true}, "VaultConfig.CACert"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.Address, cfg.Token = srv.URL, "tok"

			_, err := evalVaultCA(t, "secret/ca-bad", graft.WithVault(cfg))
			if err == nil {
				t.Fatal("expected a CA configuration error")
			}

			if !errors.Is(err, graft.ErrVaultCA) {
				t.Fatalf("errors.Is(err, graft.ErrVaultCA) is false: %v", err)
			}

			var caErr *graft.VaultCAError
			if !errors.As(err, &caErr) || caErr.Field != tc.field {
				t.Fatalf("expected a *graft.VaultCAError for %s, got: %v", tc.field, err)
			}

			for _, msg := range []string{err.Error(), graft.DetailedError(err)} {
				if strings.Contains(msg, caLeakMarker) || strings.Contains(msg, "VE9QU0VD") {
					t.Fatalf("error repeats CA content: %s", msg)
				}
			}

			if detail := graft.DetailedError(err); !strings.Contains(detail, tc.field) {
				t.Fatalf("detailed error does not name %s: %s", tc.field, detail)
			}
		})
	}

	if contacted.Load() {
		t.Fatal("a bad CA configuration must fail before any request is sent")
	}
}

func TestWithVaultCATrust_SkipVerifyWinsOverWrongCA(t *testing.T) {
	right := vaultcatest.NewCA(t, "right-ca")
	wrong := vaultcatest.NewCA(t, "wrong-ca")
	srv := right.Server(t, "secret/ca-skip", "password", "skip-ok")

	got, err := evalVaultCA(t, "secret/ca-skip", graft.WithVault(graft.VaultConfig{
		Address: srv.URL, Token: "tok", CACertBytes: wrong.PEM, SkipVerify: true,
	}))
	expectValue(t, got, err, "skip-ok")
}

// A CAPath tree holding two CAs in separate files, one of them in a
// nested subdirectory, trusts servers signed by either. The second server
// goes through WithVaultTarget, which shares the client builder.
func TestWithVaultCATrust_CAPathNestedTrustsBoth(t *testing.T) {
	a := vaultcatest.NewCA(t, "ca-a")
	b := vaultcatest.NewCA(t, "ca-b")
	srvA := a.Server(t, "secret/ca-nested", "password", "signed-by-a")
	srvB := b.Server(t, "secret/ca-nested", "password", "signed-by-b")

	dir := t.TempDir()
	vaultcatest.WriteFile(t, dir, "a.pem", a.PEM)
	vaultcatest.WriteFile(t, dir, "nested/deeper/b.pem", b.PEM)

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: srvA.URL, Token: "tok", CAPath: dir}),
		graft.WithVaultTarget("other", graft.VaultConfig{Address: srvB.URL, Token: "tok", CAPath: dir}),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(
		`a: (( vault "secret/ca-nested:password" ))` + "\n" +
			`b: (( vault@other "secret/ca-nested:password" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	for key, want := range map[string]string{"a": "signed-by-a", "b": "signed-by-b"} {
		got, err := evaluated.Get(key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}

		if got != want {
			t.Fatalf("%s = %v, want %q", key, got, want)
		}
	}
}
