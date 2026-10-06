package identity

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	const pw = "correct horse battery staple"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash is not a PHC argon2id string: %q", hash)
	}
	if strings.Contains(hash, pw) {
		t.Fatal("the stored hash contains the plaintext")
	}
	if !VerifyPassword(pw, hash) {
		t.Error("the password it was made from does not verify")
	}
	if VerifyPassword(pw+"x", hash) {
		t.Error("a different password verified")
	}
}

func TestHashPasswordSaltsEachHash(t *testing.T) {
	// Two hashes of one password must differ, or the stored value is a
	// lookup key: equal hashes reveal which accounts share a password, and
	// one cracked hash cracks all of them.
	a, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Error("the same password hashed to the same string twice — the salt is not random")
	}
}

func TestVerifyPasswordHonoursStoredCost(t *testing.T) {
	// A hash made under a lower cost must still verify after the package
	// constants are raised, or raising them locks every existing user out.
	const pw = "correct horse battery staple"
	salt := []byte("0123456789abcdef")
	const (
		oldMemory  = 19 * 1024
		oldPasses  = 2
		oldThreads = 1
	)
	key := argon2.IDKey([]byte(pw), salt, oldPasses, oldMemory, oldThreads, argonKeyLen)
	stored := phc(oldMemory, oldPasses, oldThreads, salt, key)

	if !VerifyPassword(pw, stored) {
		t.Error("a hash made at the old cost did not verify")
	}
	if VerifyPassword("something else", stored) {
		t.Error("the wrong password verified against the old-cost hash")
	}
}

func TestVerifyPasswordRejectsMalformedStoredHash(t *testing.T) {
	// Each of these must be indistinguishable from a wrong password: a
	// caller admitting someone has one useful answer, and "this row is
	// corrupt" must not be a reply anybody can provoke.
	for name, stored := range map[string]string{
		"empty":            "",
		"not PHC":          "deadbeef",
		"bcrypt":           "$2y$10$abcdefghijklmnopqrstuv",
		"wrong algorithm":  "$argon2i$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaGhhc2g",
		"wrong version":    "$argon2id$v=16$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaGhhc2g",
		"no parameters":    "$argon2id$v=19$$c2FsdHNhbHQ$aGFzaGhhc2g",
		"bad base64 salt":  "$argon2id$v=19$m=65536,t=3,p=4$!!!!$aGFzaGhhc2g",
		"empty salt":       "$argon2id$v=19$m=65536,t=3,p=4$$aGFzaGhhc2g",
		"empty hash":       "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$",
		"truncated fields": "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ",
	} {
		if VerifyPassword("correct horse battery staple", stored) {
			t.Errorf("%s: a malformed stored hash verified", name)
		}
	}
}

func TestValidatePasswordEnforcesLengthInRunes(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLength-1)); err == nil {
		t.Error("a password one character under the floor was accepted")
	}
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLength)); err != nil {
		t.Errorf("a password exactly at the floor was rejected: %v", err)
	}
	// Counted in runes, not bytes: this is 12 characters and 36 bytes, and
	// rejecting it would hold a non-Latin passphrase to a stricter rule.
	if err := ValidatePassword("日本語のパスワード合言葉"); err != nil {
		t.Errorf("a 12-rune non-Latin passphrase was rejected: %v", err)
	}
}

func TestHashPasswordRefusesWeak(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Error("HashPassword accepted a password below the floor")
	}
}

func TestVerifyAgainstDecoyNeverPanics(t *testing.T) {
	// Its only job is to spend the time; it has no result worth asserting
	// beyond not blowing up on the empty string.
	VerifyAgainstDecoy("")
	VerifyAgainstDecoy("correct horse battery staple")
}

// phc builds the same string HashPassword does, so a hash can be made at a
// cost the package no longer uses.
func phc(memory, passes uint32, threads uint8, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, passes, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}
