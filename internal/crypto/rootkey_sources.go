package crypto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Where a root key can come from (docs/adr/0037). Exactly one of these may
// be set for a given name, e.g. CC_ROOT_KEY:
//
//	CC_ROOT_KEY                   the key, base64
//	CC_ROOT_KEY_FILE              a file holding it: a Docker or Kubernetes
//	                              secret, or a systemd credential
//	CC_ROOT_KEY_VAULT_CIPHERTEXT  the key encrypted by HashiCorp Vault's
//	                              Transit engine, decrypted at startup
//
// The fingerprint is a hash of the key itself, so the same key read from
// another source has the same ref: moving from one source to another is not
// a rotation.
const (
	suffixFile  = "_FILE"
	suffixVault = "_VAULT_CIPHERTEXT"
)

// Vault settings, shared by every root key read from Vault. The VAULT_*
// names are the ones Vault's own CLI reads.
const (
	vaultAddrEnv      = "VAULT_ADDR"
	vaultTokenEnv     = "VAULT_TOKEN"
	vaultTokenFileEnv = "VAULT_TOKEN_FILE"
	vaultNamespaceEnv = "VAULT_NAMESPACE"
	vaultCACertEnv    = "VAULT_CACERT"
	vaultKeyEnv       = "CC_ROOT_KEY_VAULT_KEY"
	vaultMountEnv     = "CC_ROOT_KEY_VAULT_MOUNT"

	defaultVaultKey   = "context-commander"
	defaultVaultMount = "transit"
)

// StaticRootKeyProvider holds a key read once, at startup, from whichever
// source was configured. Source says which, for logs; never the key.
type StaticRootKeyProvider struct {
	key    [KeySize]byte
	ref    string
	Source string
}

func (p *StaticRootKeyProvider) CurrentKey(_ context.Context) ([KeySize]byte, string, error) {
	return p.key, p.ref, nil
}

// LoadRootKey reads the root key named name (CC_ROOT_KEY, or
// CC_ROOT_KEY_PREVIOUS during a rotation) from the one source configured
// for it.
func LoadRootKey(ctx context.Context, name string) (*StaticRootKeyProvider, error) {
	direct, file, vault := os.Getenv(name), os.Getenv(name+suffixFile), os.Getenv(name+suffixVault)
	set := 0
	for _, v := range []string{direct, file, vault} {
		if v != "" {
			set++
		}
	}
	switch {
	case set == 0:
		return nil, fmt.Errorf("crypto: no root key: set %s, %s or %s", name, name+suffixFile, name+suffixVault)
	case set > 1:
		return nil, fmt.Errorf("crypto: more than one of %s, %s and %s is set; set exactly one", name, name+suffixFile, name+suffixVault)
	case direct != "":
		return fromBase64(direct, name, "environment")
	case file != "":
		raw, err := os.ReadFile(file) //nolint:gosec // operator-supplied path to the key file
		if err != nil {
			return nil, fmt.Errorf("crypto: read %s: %w", name+suffixFile, err)
		}
		return fromBase64(strings.TrimSpace(string(raw)), name+suffixFile, "file "+file)
	default:
		plain, err := vaultDecrypt(ctx, strings.TrimSpace(vault))
		if err != nil {
			return nil, fmt.Errorf("crypto: %s: %w", name+suffixVault, err)
		}
		return fromBase64(plain, name+suffixVault, "vault transit")
	}
}

func fromBase64(encoded, what, source string) (*StaticRootKeyProvider, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode %s: %w", what, err)
	}
	if len(raw) != KeySize {
		return nil, fmt.Errorf("crypto: %s must decode to %d bytes, got %d", what, KeySize, len(raw))
	}
	p := &StaticRootKeyProvider{Source: source}
	copy(p.key[:], raw)
	sum := sha256.Sum256(p.key[:])
	p.ref = "env:" + hex.EncodeToString(sum[:8])
	return p, nil
}

// vaultDecrypt asks Vault's Transit engine to decrypt ciphertext and returns
// the plaintext as Vault gives it, base64. The key was encrypted with
//
//	vault write -field=ciphertext transit/encrypt/context-commander plaintext="$CC_ROOT_KEY"
//
// so the plaintext is the base64 key itself.
func vaultDecrypt(ctx context.Context, ciphertext string) (string, error) {
	addr := strings.TrimRight(os.Getenv(vaultAddrEnv), "/")
	if addr == "" {
		return "", fmt.Errorf("%s is not set", vaultAddrEnv)
	}
	if !strings.HasPrefix(ciphertext, "vault:") {
		return "", errors.New(`not a Vault Transit ciphertext (it starts with "vault:v1:")`)
	}
	token := os.Getenv(vaultTokenEnv)
	if token == "" {
		if f := os.Getenv(vaultTokenFileEnv); f != "" {
			raw, err := os.ReadFile(f) //nolint:gosec // operator-supplied path to the token file
			if err != nil {
				return "", fmt.Errorf("read %s: %w", vaultTokenFileEnv, err)
			}
			token = strings.TrimSpace(string(raw))
		}
	}
	if token == "" {
		return "", fmt.Errorf("set %s or %s", vaultTokenEnv, vaultTokenFileEnv)
	}
	mount := envOr(vaultMountEnv, defaultVaultMount)
	key := envOr(vaultKeyEnv, defaultVaultKey)

	client := &http.Client{Timeout: 10 * time.Second}
	if ca := os.Getenv(vaultCACertEnv); ca != "" {
		pem, err := os.ReadFile(ca) //nolint:gosec // operator-supplied CA bundle
		if err != nil {
			return "", fmt.Errorf("read %s: %w", vaultCACertEnv, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return "", fmt.Errorf("%s holds no PEM certificate", vaultCACertEnv)
		}
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	}

	body, _ := json.Marshal(map[string]string{"ciphertext": ciphertext})
	endpoint := addr + "/v1/" + url.PathEscape(mount) + "/decrypt/" + url.PathEscape(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body)) //nolint:gosec // G704: VAULT_ADDR is the operator's own configuration, not request input
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("Content-Type", "application/json")
	if ns := os.Getenv(vaultNamespaceEnv); ns != "" {
		req.Header.Set("X-Vault-Namespace", ns)
	}
	resp, err := client.Do(req) //nolint:gosec // G704: as above
	if err != nil {
		return "", fmt.Errorf("reach Vault at %s: %w", addr, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Errors []string `json:"errors"`
		}
		_ = json.Unmarshal(raw, &e)
		return "", fmt.Errorf("vault answered %d on %s/decrypt/%s: %s", resp.StatusCode, mount, key, strings.Join(e.Errors, "; "))
	}
	var out struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Data.Plaintext == "" {
		return "", errors.New("vault's answer has no plaintext")
	}
	return out.Data.Plaintext, nil
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
