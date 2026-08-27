package crypto

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
)

// RootKeyProvider supplies the root/installation key that wraps project
// keys (docs/adr/0005-key-hierarchy-and-artifact-size-limit.md). The root
// key itself never gets stored in the database — see
// docs/adr/0002-non-recursive-rls.md's role-model note: this is the
// application-level equivalent, "the DB never holds the key that would let
// it decrypt its own ciphertexts unassisted."
//
// Ref identifies *which* root key was used to wrap a given project key
// (project_keys.root_key_ref) without ever exposing key material — needed
// so a future key-provider swap (env -> Vault/KMS) can tell old-key-wrapped
// rows apart from new ones during a rotation.
type RootKeyProvider interface {
	CurrentKey(ctx context.Context) (key [KeySize]byte, ref string, err error)
}

// EnvRootKeyProvider reads a base64-encoded 32-byte key from an environment
// variable — the "lokálně env-based v1" provider from
// docs/architecture-cookbook.md §14.2. A Vault/KMS-backed RootKeyProvider is
// a drop-in replacement; nothing above this interface changes.
type EnvRootKeyProvider struct {
	key [KeySize]byte
	ref string
}

// NewEnvRootKeyProvider reads envVar, expecting a standard-base64-encoded
// 32-byte (AES-256) key. ref is derived from a hash of the key material —
// never the key itself — so it's safe to log or store in root_key_ref.
func NewEnvRootKeyProvider(envVar string) (*EnvRootKeyProvider, error) {
	encoded := os.Getenv(envVar)
	if encoded == "" {
		return nil, fmt.Errorf("crypto: environment variable %s is not set", envVar)
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode %s: %w", envVar, err)
	}
	if len(raw) != KeySize {
		return nil, fmt.Errorf("crypto: %s must decode to %d bytes, got %d", envVar, KeySize, len(raw))
	}

	var key [KeySize]byte
	copy(key[:], raw)

	sum := sha256.Sum256(key[:])
	ref := "env:" + hex.EncodeToString(sum[:8])

	return &EnvRootKeyProvider{key: key, ref: ref}, nil
}

func (p *EnvRootKeyProvider) CurrentKey(_ context.Context) ([KeySize]byte, string, error) {
	return p.key, p.ref, nil
}

// GenerateRootKeyBase64 produces a fresh random key in the form
// NewEnvRootKeyProvider expects — a helper for `ccd bootstrap`-style setup
// and for tests, not used on any request path.
func GenerateRootKeyBase64() (string, error) {
	key, err := generateKey()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key[:]), nil
}
