package operators

import (
	"context"
	"path/filepath"
	"testing"

	vaultbackend "github.com/fivetwenty-io/graft/internal/backends/vault"
	"github.com/fivetwenty-io/graft/internal/vaultca/vaultcatest"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// evalVaultLine evaluates one `key: (( ... ))` document on an engine with
// no vault configured, so the operator falls back to the environment.
func evalVaultLine(t *testing.T, yaml string) interface{} {
	t.Helper()

	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(yaml + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	got, err := evaluated.Get("secret")
	if err != nil {
		t.Fatalf("Get(secret): %v", err)
	}

	return got
}

// The default-reader fallback builds its client from VAULT_ADDR and
// friends, so VAULT_CACERT must reach that client.
func TestVaultDefaultReaderHonorsVaultCACert(t *testing.T) {
	ca := vaultcatest.NewCA(t, "env-ca")
	srv := ca.Server(t, "secret/op-env-cacert", "password", "op-env-ca-ok")
	file := vaultcatest.WriteFile(t, t.TempDir(), "ca.pem", ca.PEM)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("VAULT_ADDR", srv.URL)
	t.Setenv("VAULT_TOKEN", "tok")
	t.Setenv("VAULT_NAMESPACE", "")
	t.Setenv("VAULT_SKIP_VERIFY", "")
	t.Setenv("VAULT_CAPATH", "")
	t.Setenv("VAULT_CACERT", file)

	previous := vaultbackend.GlobalReader
	vaultbackend.GlobalReader = nil
	t.Cleanup(func() { vaultbackend.GlobalReader = previous })

	got := evalVaultLine(t, `secret: (( vault "secret/op-env-cacert:password" ))`)
	if got != "op-env-ca-ok" {
		t.Fatalf("got %v, want op-env-ca-ok", got)
	}
}

func TestVaultTargetHonorsTargetCAPath(t *testing.T) {
	ca := vaultcatest.NewCA(t, "target-ca")
	srv := ca.Server(t, "secret/op-target-capath", "password", "op-target-ca-ok")
	dir := filepath.Join(t.TempDir(), "certs")
	vaultcatest.WriteFile(t, dir, "nested/ca.pem", ca.PEM)

	t.Setenv("VAULT_OPCATARGET_ADDR", srv.URL)
	t.Setenv("VAULT_OPCATARGET_TOKEN", "tok")
	t.Setenv("VAULT_OPCATARGET_CAPATH", dir)

	got := evalVaultLine(t, `secret: (( vault@opcatarget "secret/op-target-capath:password" ))`)
	if got != "op-target-ca-ok" {
		t.Fatalf("got %v, want op-target-ca-ok", got)
	}
}

func TestVaultTargetHonorsTargetCACert(t *testing.T) {
	ca := vaultcatest.NewCA(t, "target-ca")
	srv := ca.Server(t, "secret/op-target-cacert", "password", "op-target-cacert-ok")
	file := vaultcatest.WriteFile(t, t.TempDir(), "ca.pem", ca.PEM)

	t.Setenv("VAULT_OPCACERTTARGET_ADDR", srv.URL)
	t.Setenv("VAULT_OPCACERTTARGET_TOKEN", "tok")
	t.Setenv("VAULT_OPCACERTTARGET_CACERT", file)

	got := evalVaultLine(t, `secret: (( vault@opcacerttarget "secret/op-target-cacert:password" ))`)
	if got != "op-target-cacert-ok" {
		t.Fatalf("got %v, want op-target-cacert-ok", got)
	}
}
