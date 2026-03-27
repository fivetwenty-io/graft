package vault

import (
	"context"
	"fmt"

	api "github.com/hashicorp/vault/api"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// ErrNotFound is returned when a secret path does not exist in Vault.
type ErrNotFound struct {
	Path string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("secret %s not found", e.Path)
}

// VaultReader abstracts Vault secret reading for both KV v1 and v2 mounts.
type VaultReader interface {
	ReadSecret(ctx context.Context, path string) (map[string]interface{}, error)
}

// vaultAPIReader implements VaultReader using hashicorp/vault/api.
type vaultAPIReader struct {
	client *api.Client
}

// NewVaultReader creates a VaultReader wrapping the given api.Client.
func NewVaultReader(client *api.Client) VaultReader {
	return &vaultAPIReader{client: client}
}

// ReadSecret reads a secret from the given path, handling both KV v1 and v2
// response structures transparently.
func (r *vaultAPIReader) ReadSecret(ctx context.Context, path string) (map[string]interface{}, error) {
	graft.DEBUG("vault/api: reading secret at `%s'", path)

	secret, err := r.client.Logical().ReadWithContext(ctx, path)
	if err != nil {
		graft.DEBUG("  failure: %s", err)
		return nil, err
	}

	if secret == nil || secret.Data == nil {
		graft.DEBUG("  not found (nil response)")
		return nil, &ErrNotFound{Path: path}
	}

	// Detect KV v2 response structure: has "data" and "metadata" keys
	if innerData, ok := secret.Data["data"].(map[string]interface{}); ok {
		if _, hasMetadata := secret.Data["metadata"]; hasMetadata {
			graft.DEBUG("  success (KV v2)")
			return innerData, nil
		}
	}

	// KV v1 response: data is the secret map directly
	graft.DEBUG("  success (KV v1)")
	return secret.Data, nil
}
