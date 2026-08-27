package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	tokenPrefix    = "cc"
	secretByteSize = 32 // 256 bits of entropy, base64url-encoded below
)

var ErrMalformedToken = errors.New("identity: malformed PAT")

// GenerateToken produces a new PAT for an existing credential row
// (credentialID = client_credentials.id) in the format
// cc_<credential-id>_<random-secret> (docs/adr/0009). It returns the full
// token to hand to the caller exactly once, and the hash to persist as
// ClientCredential.TokenHash — the raw secret itself is never stored.
func GenerateToken(credentialID uuid.UUID) (token string, tokenHash string, err error) {
	secretBytes := make([]byte, secretByteSize)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", fmt.Errorf("identity: generate secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)

	token = fmt.Sprintf("%s_%s_%s", tokenPrefix, credentialID.String(), secret)
	return token, HashSecret(secret), nil
}

// HashSecret hashes only the random-secret part of a PAT (never the
// credential ID, which is public and used for lookup, not comparison).
// A random, high-entropy secret does not need a slow password hash (unlike
// a human-chosen password) — SHA-256 is standard practice for API tokens
// (GitHub, Stripe, ...) and keeps verification cheap.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// ParseToken splits a presented token into its credential ID (for an O(1)
// `WHERE id = $1` lookup — never "load every credential and compare") and
// its secret part (to hash and compare against the stored TokenHash).
func ParseToken(token string) (credentialID uuid.UUID, secret string, err error) {
	parts := strings.SplitN(token, "_", 3)
	if len(parts) != 3 || parts[0] != tokenPrefix {
		return uuid.Nil, "", ErrMalformedToken
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return uuid.Nil, "", ErrMalformedToken
	}
	if parts[2] == "" {
		return uuid.Nil, "", ErrMalformedToken
	}
	return id, parts[2], nil
}

// VerifySecret compares a presented secret against a stored hash in
// constant time, to avoid a timing side-channel on the comparison.
func VerifySecret(secret, storedHash string) bool {
	got := HashSecret(secret)
	return subtle.ConstantTimeCompare([]byte(got), []byte(storedHash)) == 1
}
