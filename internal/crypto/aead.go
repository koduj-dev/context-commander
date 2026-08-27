package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

const KeySize = 32 // AES-256

var ErrAuthenticationFailed = errors.New("crypto: authentication failed (wrong key, tampered ciphertext, or mismatched AAD)")

// seal encrypts plaintext with AES-256-GCM under key, binding aad as
// additional authenticated data. A fresh random 96-bit nonce is generated
// per call and returned alongside the ciphertext — reusing a (key, nonce)
// pair with GCM is catastrophic for confidentiality, so this is the only
// function in the package allowed to produce one, and it always draws from
// crypto/rand.
func seal(key [KeySize]byte, plaintext, aad []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("crypto: new gcm: %w", err)
	}

	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("crypto: generate nonce: %w", err)
	}

	ciphertext = gcm.Seal(nil, nonce, plaintext, aad)
	return ciphertext, nonce, nil
}

// open decrypts and authenticates ciphertext, verifying it was produced for
// exactly this aad. A mismatched aad (wrong project/resource/version) fails
// exactly like a tampered ciphertext or wrong key — ErrAuthenticationFailed,
// not a distinct error, so callers can't be tricked into treating an
// AAD mismatch as "just needs the right context" and retrying with one.
func open(key [KeySize]byte, ciphertext, nonce, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, ErrAuthenticationFailed
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrAuthenticationFailed
	}
	return plaintext, nil
}

// generateKey returns a fresh random AES-256 key from crypto/rand — used
// both for per-resource DEKs and for a new project key on rotation.
func generateKey() ([KeySize]byte, error) {
	var key [KeySize]byte
	if _, err := rand.Read(key[:]); err != nil {
		return key, fmt.Errorf("crypto: generate key: %w", err)
	}
	return key, nil
}
