package crypto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clearRootKeyEnv(t *testing.T) {
	for _, n := range []string{"CC_TEST_KEY", "CC_TEST_KEY_FILE", "CC_TEST_KEY_VAULT_CIPHERTEXT",
		vaultAddrEnv, vaultTokenEnv, vaultTokenFileEnv, vaultNamespaceEnv, vaultCACertEnv, vaultKeyEnv, vaultMountEnv} {
		t.Setenv(n, "")
	}
}

// The same key from the environment, a file or Vault has the same
// fingerprint, so changing where it comes from is not a rotation.
func TestLoadRootKeySources(t *testing.T) {
	ctx := context.Background()
	key, _ := GenerateRootKeyBase64()

	clearRootKeyEnv(t)
	t.Setenv("CC_TEST_KEY", key)
	fromEnv, err := LoadRootKey(ctx, "CC_TEST_KEY")
	if err != nil || fromEnv.Source != "environment" {
		t.Fatalf("env: %+v, %v", fromEnv, err)
	}

	clearRootKeyEnv(t)
	path := filepath.Join(t.TempDir(), "root-key")
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_TEST_KEY_FILE", path)
	fromFile, err := LoadRootKey(ctx, "CC_TEST_KEY")
	if err != nil || fromFile.ref != fromEnv.ref || !strings.HasPrefix(fromFile.Source, "file ") {
		t.Fatalf("file: %+v, %v", fromFile, err)
	}

	var gotToken, gotNS, gotPath, gotCiphertext string
	vault := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken, gotNS, gotPath = r.Header.Get("X-Vault-Token"), r.Header.Get("X-Vault-Namespace"), r.URL.Path
		var body struct{ Ciphertext string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotCiphertext = body.Ciphertext
		if gotToken != "s.good" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"plaintext":"` + key + `"}}`))
	}))
	defer vault.Close()

	clearRootKeyEnv(t)
	t.Setenv("CC_TEST_KEY_VAULT_CIPHERTEXT", "vault:v1:abc")
	t.Setenv(vaultAddrEnv, vault.URL+"/")
	t.Setenv(vaultTokenEnv, "s.good")
	t.Setenv(vaultNamespaceEnv, "team")
	t.Setenv(vaultKeyEnv, "cc-root")
	fromVault, err := LoadRootKey(ctx, "CC_TEST_KEY")
	if err != nil || fromVault.ref != fromEnv.ref || fromVault.Source != "vault transit" {
		t.Fatalf("vault: %+v, %v", fromVault, err)
	}
	if gotPath != "/v1/transit/decrypt/cc-root" || gotNS != "team" || gotCiphertext != "vault:v1:abc" {
		t.Errorf("vault request: path %q, namespace %q, ciphertext %q", gotPath, gotNS, gotCiphertext)
	}

	t.Setenv(vaultTokenEnv, "s.bad")
	if _, err := LoadRootKey(ctx, "CC_TEST_KEY"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("a refused token: %v", err)
	}
	t.Setenv(vaultTokenEnv, "")
	tokenFile := filepath.Join(t.TempDir(), "token")
	_ = os.WriteFile(tokenFile, []byte("s.good\n"), 0o600)
	t.Setenv(vaultTokenFileEnv, tokenFile)
	if _, err := LoadRootKey(ctx, "CC_TEST_KEY"); err != nil {
		t.Errorf("token from a file: %v", err)
	}
}

func TestLoadRootKeyRefusals(t *testing.T) {
	ctx := context.Background()
	key, _ := GenerateRootKeyBase64()
	cases := map[string]map[string]string{
		"nothing set":         {},
		"two sources":         {"CC_TEST_KEY": key, "CC_TEST_KEY_FILE": "/nonexistent"},
		"not base64":          {"CC_TEST_KEY": "not base64!"},
		"wrong length":        {"CC_TEST_KEY": "c2hvcnQ="},
		"missing file":        {"CC_TEST_KEY_FILE": "/nonexistent/root-key"},
		"vault without addr":  {"CC_TEST_KEY_VAULT_CIPHERTEXT": "vault:v1:abc", vaultTokenEnv: "t"},
		"vault without token": {"CC_TEST_KEY_VAULT_CIPHERTEXT": "vault:v1:abc", vaultAddrEnv: "http://127.0.0.1:1"},
		"not a ciphertext":    {"CC_TEST_KEY_VAULT_CIPHERTEXT": key, vaultAddrEnv: "http://127.0.0.1:1", vaultTokenEnv: "t"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			clearRootKeyEnv(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if p, err := LoadRootKey(ctx, "CC_TEST_KEY"); err == nil {
				t.Errorf("accepted: %+v", p)
			}
		})
	}
}
