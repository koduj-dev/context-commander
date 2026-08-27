package identity_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/identity"
)

func TestGenerateAndVerifyToken(t *testing.T) {
	credentialID := uuid.New()

	token, storedHash, err := identity.GenerateToken(credentialID)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	gotID, secret, err := identity.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if gotID != credentialID {
		t.Fatalf("ParseToken id = %s, want %s", gotID, credentialID)
	}

	if !identity.VerifySecret(secret, storedHash) {
		t.Fatal("VerifySecret: expected match for the freshly generated token")
	}
}

func TestVerifySecretRejectsWrongSecret(t *testing.T) {
	_, storedHash, err := identity.GenerateToken(uuid.New())
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	if identity.VerifySecret("not-the-right-secret", storedHash) {
		t.Fatal("VerifySecret: expected mismatch for a wrong secret")
	}
}

func TestParseTokenRejectsMalformedInput(t *testing.T) {
	cases := []string{
		"",
		"not-a-token",
		"cc_only-one-part",
		"wrongprefix_" + uuid.New().String() + "_secret",
		"cc_not-a-uuid_secret",
		"cc_" + uuid.New().String() + "_", // empty secret
	}

	for _, tc := range cases {
		if _, _, err := identity.ParseToken(tc); err == nil {
			t.Errorf("ParseToken(%q): expected error, got nil", tc)
		}
	}
}

func TestGenerateTokenProducesUniqueSecrets(t *testing.T) {
	id := uuid.New()

	tokenA, hashA, err := identity.GenerateToken(id)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	tokenB, hashB, err := identity.GenerateToken(id)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	if tokenA == tokenB || hashA == hashB {
		t.Fatal("two calls to GenerateToken produced the same secret")
	}
}
