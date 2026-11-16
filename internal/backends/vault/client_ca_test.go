package vault

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/vaultca/vaultcatest"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

const caLeakMarker = "TOPSECRET-PAYLOAD-7f3a"

func readPassword(t *testing.T, reader Reader, path string) (string, error) {
	t.Helper()

	data, err := reader.ReadSecret(context.Background(), path)
	if err != nil {
		return "", err
	}

	s, _ := data["password"].(string)

	return s, nil
}

func newTestPool() *ClientPool {
	return &ClientPool{
		clients: make(map[string]Reader),
		configs: make(map[string]*Target),
	}
}

func expectCAError(t *testing.T, err error, field string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected a CA configuration error")
	}

	if !errors.Is(err, graft.ErrVaultCA) {
		t.Fatalf("errors.Is(err, graft.ErrVaultCA) is false: %v", err)
	}

	var caErr *graft.VaultCAError
	if !errors.As(err, &caErr) || caErr.Field != field {
		t.Fatalf("expected a *graft.VaultCAError for %s, got: %v", field, err)
	}

	if strings.Contains(err.Error(), caLeakMarker) {
		t.Fatalf("error repeats CA content: %v", err)
	}
}

func TestInitializeClientHonorsCAEnv(t *testing.T) {
	ca := vaultcatest.NewCA(t, "env-ca")
	other := vaultcatest.NewCA(t, "other-ca")
	srv := ca.Server(t, "secret/env-ca", "password", "env-ca-ok")
	dir := t.TempDir()
	file := vaultcatest.WriteFile(t, dir, "ca.pem", ca.PEM)
	caDir := filepath.Dir(vaultcatest.WriteFile(t, dir, "certs/nested/ca.pem", ca.PEM))
	caDir = filepath.Dir(caDir)
	otherDir := filepath.Dir(vaultcatest.WriteFile(t, dir, "other/ca.pem", other.PEM))

	cases := []struct {
		name string
		env  map[string]string
	}{
		{"VAULT_CACERT", map[string]string{"VAULT_CACERT": file}},
		{"VAULT_CAPATH", map[string]string{"VAULT_CAPATH": caDir}},
		{"VAULT_CACERT beats VAULT_CAPATH",
			map[string]string{"VAULT_CACERT": file, "VAULT_CAPATH": otherDir}},
		{"empty VAULT_CACERT is unset",
			map[string]string{"VAULT_CACERT": "", "VAULT_CAPATH": caDir}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateVaultEnv(t)
			t.Setenv("VAULT_ADDR", srv.URL)
			t.Setenv("VAULT_TOKEN", "tok")

			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			reader, err := DefaultReader()
			if err != nil {
				t.Fatalf("DefaultReader: %v", err)
			}

			got, err := readPassword(t, reader, "secret/env-ca")
			if err != nil || got != "env-ca-ok" {
				t.Fatalf("got %q, %v; want env-ca-ok", got, err)
			}
		})
	}
}

func TestInitializeClientWithoutCAFailsVerification(t *testing.T) {
	ca := vaultcatest.NewCA(t, "env-ca")
	srv := ca.Server(t, "secret/env-no-ca", "password", "unreachable")

	isolateVaultEnv(t) // VAULT_CACERT and VAULT_CAPATH are set but empty
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "tok")

	reader, err := DefaultReader()
	if err != nil {
		t.Fatalf("DefaultReader: %v", err)
	}

	_, err = readPassword(t, reader, "secret/env-no-ca")
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected a certificate verification error, got: %v", err)
	}
}

func TestInitializeClientBadCAEnvFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	bad := vaultcatest.WriteFile(t, dir, "bad.pem", []byte(caLeakMarker))
	emptyDir := t.TempDir()

	cases := []struct {
		name  string
		env   string
		value string
	}{
		{"missing VAULT_CACERT", "VAULT_CACERT", filepath.Join(dir, "missing.pem")},
		{"bad VAULT_CACERT", "VAULT_CACERT", bad},
		{"empty VAULT_CAPATH dir", "VAULT_CAPATH", emptyDir},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateVaultEnv(t)
			t.Setenv("VAULT_ADDR", "https://127.0.0.1:1")
			t.Setenv("VAULT_TOKEN", "tok")
			t.Setenv(tc.env, tc.value)

			reader, err := DefaultReader()
			expectCAError(t, err, tc.env)

			if reader != nil || GlobalReader != nil {
				t.Fatal("a failed CA load must not leave a reader behind")
			}
		})
	}
}

func TestTargetHonorsCAEnv(t *testing.T) {
	ca := vaultcatest.NewCA(t, "target-ca")
	srv := ca.Server(t, "secret/target-ca", "password", "target-ca-ok")
	dir := t.TempDir()
	file := vaultcatest.WriteFile(t, dir, "ca.pem", ca.PEM)
	caDir := filepath.Dir(vaultcatest.WriteFile(t, dir, "certs/ca.pem", ca.PEM))

	cases := []struct {
		name   string
		suffix string
		value  string
	}{
		{"CACERT", "CACERT", file},
		{"CAPATH", "CAPATH", caDir},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateVaultEnv(t)
			t.Setenv("VAULT_CATARGET_ADDR", srv.URL)
			t.Setenv("VAULT_CATARGET_TOKEN", "tok")
			t.Setenv("VAULT_CATARGET_"+tc.suffix, tc.value)

			reader, err := newTestPool().GetClient("catarget", nil)
			if err != nil {
				t.Fatalf("GetClient: %v", err)
			}

			got, err := readPassword(t, reader, "secret/target-ca")
			if err != nil || got != "target-ca-ok" {
				t.Fatalf("got %q, %v; want target-ca-ok", got, err)
			}
		})
	}
}

func TestTargetWithoutCAFailsVerification(t *testing.T) {
	ca := vaultcatest.NewCA(t, "target-ca")
	srv := ca.Server(t, "secret/target-no-ca", "password", "unreachable")

	isolateVaultEnv(t)
	t.Setenv("VAULT_CATARGET_ADDR", srv.URL)
	t.Setenv("VAULT_CATARGET_TOKEN", "tok")
	t.Setenv("VAULT_CATARGET_CACERT", "")

	reader, err := newTestPool().GetClient("catarget", nil)
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}

	_, err = readPassword(t, reader, "secret/target-no-ca")
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected a certificate verification error, got: %v", err)
	}
}

func TestTargetBadCAEnvFailsLoudly(t *testing.T) {
	bad := vaultcatest.WriteFile(t, t.TempDir(), "bad.pem", []byte(caLeakMarker))

	for _, suffix := range []string{"CACERT", "CAPATH"} {
		t.Run(suffix, func(t *testing.T) {
			isolateVaultEnv(t)
			t.Setenv("VAULT_CATARGET_ADDR", "https://127.0.0.1:1")
			t.Setenv("VAULT_CATARGET_TOKEN", "tok")
			t.Setenv("VAULT_CATARGET_"+suffix, bad)

			_, err := newTestPool().GetClient("catarget", nil)
			expectCAError(t, err, "VAULT_CATARGET_"+suffix)
		})
	}
}

func TestTargetSkipVerifyWinsOverWrongCA(t *testing.T) {
	right := vaultcatest.NewCA(t, "right-ca")
	wrong := vaultcatest.NewCA(t, "wrong-ca")
	srv := right.Server(t, "secret/target-skip", "password", "skip-ok")
	wrongFile := vaultcatest.WriteFile(t, t.TempDir(), "wrong.pem", wrong.PEM)

	isolateVaultEnv(t)
	t.Setenv("VAULT_CATARGET_ADDR", srv.URL)
	t.Setenv("VAULT_CATARGET_TOKEN", "tok")
	t.Setenv("VAULT_CATARGET_CACERT", wrongFile)
	t.Setenv("VAULT_CATARGET_SKIP_VERIFY", "true")

	reader, err := newTestPool().GetClient("catarget", nil)
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}

	got, err := readPassword(t, reader, "secret/target-skip")
	if err != nil || got != "skip-ok" {
		t.Fatalf("got %q, %v; want skip-ok", got, err)
	}
}
