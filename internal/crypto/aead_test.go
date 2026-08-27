package crypto

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	key, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey: %v", err)
	}
	plaintext := []byte("hello, context commander")
	aad := []byte("aad")

	ciphertext, nonce, err := seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	got, err := open(key, ciphertext, nonce, aad)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("open returned %q, want %q", got, plaintext)
	}
}

func TestOpenFailsOnWrongKey(t *testing.T) {
	key1, _ := generateKey()
	key2, _ := generateKey()

	ciphertext, nonce, err := seal(key1, []byte("secret"), nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := open(key2, ciphertext, nonce, nil); err != ErrAuthenticationFailed {
		t.Fatalf("open with wrong key: got err=%v, want ErrAuthenticationFailed", err)
	}
}

func TestOpenFailsOnMismatchedAAD(t *testing.T) {
	key, _ := generateKey()

	ciphertext, nonce, err := seal(key, []byte("secret"), []byte("project-a"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := open(key, ciphertext, nonce, []byte("project-b")); err != ErrAuthenticationFailed {
		t.Fatalf("open with wrong AAD: got err=%v, want ErrAuthenticationFailed", err)
	}
}

func TestOpenFailsOnTamperedCiphertext(t *testing.T) {
	key, _ := generateKey()

	ciphertext, nonce, err := seal(key, []byte("secret"), nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	tampered := append([]byte(nil), ciphertext...)
	tampered[0] ^= 0xFF

	if _, err := open(key, tampered, nonce, nil); err != ErrAuthenticationFailed {
		t.Fatalf("open tampered ciphertext: got err=%v, want ErrAuthenticationFailed", err)
	}
}

// TestNonceUniqueness is the acceptance test docs/adr/0005 calls for: a
// large-N check that seal() never reuses a nonce. Reusing a (key, nonce)
// pair with AES-GCM is catastrophic for confidentiality.
func TestNonceUniqueness(t *testing.T) {
	const n = 10000
	key, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey: %v", err)
	}

	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		_, nonce, err := seal(key, []byte("x"), nil)
		if err != nil {
			t.Fatalf("seal #%d: %v", i, err)
		}
		s := string(nonce)
		if _, dup := seen[s]; dup {
			t.Fatalf("nonce reused at iteration %d", i)
		}
		seen[s] = struct{}{}
	}
}

func TestPackUnpackWrapped(t *testing.T) {
	key, _ := generateKey()
	ciphertext, nonce, err := seal(key, []byte("project key material"), nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	blob := packWrapped(ciphertext, nonce)
	gotCiphertext, gotNonce, err := unpackWrapped(blob)
	if err != nil {
		t.Fatalf("unpackWrapped: %v", err)
	}
	if !bytes.Equal(gotCiphertext, ciphertext) {
		t.Error("unpacked ciphertext does not match original")
	}
	if !bytes.Equal(gotNonce, nonce) {
		t.Error("unpacked nonce does not match original")
	}
}

func TestUnpackWrappedRejectsTooShort(t *testing.T) {
	if _, _, err := unpackWrapped([]byte("short")); err == nil {
		t.Fatal("expected an error for a blob shorter than the nonce size")
	}
}
