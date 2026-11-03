package crypto

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"
)

// Purpose names what a derived key is for. Each one is an HKDF "info"
// string, and two purposes never share a key: that string is the domain
// separation, so a MAC computed for one cannot be replayed as another's.
// Versioned, so a purpose that must change its format gets a new key rather
// than one that verifies both.
type Purpose string

// PurposeOIDCAttemptCookie signs the cc_oidc_* cookie that carries an OIDC
// sign-in attempt from /start to the callback (docs/adr/0023).
const PurposeOIDCAttemptCookie Purpose = "context-commander/oidc-attempt-cookie/v1"

// DeriveKey derives a KeySize-byte key for one purpose from the root key,
// with HKDF-SHA256 (RFC 5869).
//
// The root key is otherwise used for one thing, wrapping project keys
// (docs/adr/0005). It is not used as a MAC key directly, because a key with
// two jobs is a key whose two jobs can be played against each other; HKDF
// with a distinct info string per purpose is the standard way to get any
// number of independent keys from it without storing more secrets. A derived
// key changes with the root key: after a root key rotation, whatever it
// signed stops verifying, which for a ten-minute sign-in attempt is the
// right lifetime anyway.
func DeriveKey(ctx context.Context, root RootKeyProvider, purpose Purpose) ([KeySize]byte, error) {
	if purpose == "" {
		return [KeySize]byte{}, errors.New("crypto: derive key: a purpose is required")
	}
	key, _, err := root.CurrentKey(ctx)
	if err != nil {
		return [KeySize]byte{}, fmt.Errorf("crypto: derive key: %w", err)
	}
	derived, err := hkdf.Key(sha256.New, key[:], nil, string(purpose), KeySize)
	if err != nil {
		return [KeySize]byte{}, fmt.Errorf("crypto: derive key: %w", err)
	}
	var out [KeySize]byte
	copy(out[:], derived)
	return out, nil
}
