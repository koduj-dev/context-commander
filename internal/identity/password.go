package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

var (
	ErrWeakPassword    = errors.New("identity: password too short")
	ErrNoLocalPassword = errors.New("identity: user has no local password")
	errMalformedHash   = errors.New("identity: malformed password hash")
)

// Argon2id cost. Deliberately slow — the opposite of HashSecret, which hashes
// a 256-bit random PAT secret with plain SHA-256 and is right to. A password
// is chosen by a person, so it carries far less entropy than it looks like it
// does, and the only defence against an offline attack on a leaked hash is
// making each guess expensive.
//
// 64 MiB × 3 passes is comfortably above OWASP's floor (19 MiB, 2 passes) and
// costs roughly 50–100 ms per attempt on this class of hardware. That is a
// cost per LOGIN, not per request — a session is issued once and verified
// thereafter by the PAT path's cheap hash.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

// MinPasswordLength is a length floor and nothing else: no required character
// classes, no forbidden substrings. Composition rules push people towards
// "Password1!" and are no longer recommended by NIST (SP 800-63B) or OWASP;
// length is the property that actually buys entropy. Measured in runes rather
// than bytes so a passphrase in a non-Latin script is not quietly held to a
// stricter rule than an ASCII one.
const MinPasswordLength = 12

// ValidatePassword checks the one property worth enforcing before hashing.
func ValidatePassword(plaintext string) error {
	if utf8.RuneCountInString(plaintext) < MinPasswordLength {
		return fmt.Errorf("%w: need at least %d characters", ErrWeakPassword, MinPasswordLength)
	}
	return nil
}

// HashPassword returns a PHC string carrying the algorithm, its parameters
// and the salt alongside the hash:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
//
// Self-describing on purpose. Raising the cost later is then a re-hash on the
// owner's next successful login — the stored string says what it was made
// with, so old and new hashes can coexist without a migration or a second
// column to interpret.
func HashPassword(plaintext string) (string, error) {
	if err := ValidatePassword(plaintext); err != nil {
		return "", err
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: password salt: %w", err)
	}
	key := argon2.IDKey([]byte(plaintext), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword recomputes the hash with the parameters recorded in the
// stored string — not with the constants above — so a hash made under an
// older cost still verifies after the cost is raised.
//
// A malformed stored hash is a false, not an error: a caller deciding whether
// to admit someone has exactly one useful answer, and "this row is corrupt"
// must not be distinguishable from "wrong password" by anyone presenting a
// guess.
func VerifyPassword(plaintext, stored string) bool {
	memory, passes, threads, salt, want, err := parsePHC(stored)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(plaintext), salt, passes, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func parsePHC(stored string) (memory, passes uint32, threads uint8, salt, hash []byte, err error) {
	parts := strings.Split(stored, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return 0, 0, 0, nil, nil, errMalformedHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return 0, 0, 0, nil, nil, errMalformedHash
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return 0, 0, 0, nil, nil, errMalformedHash
	}
	if hash, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return 0, 0, 0, nil, nil, errMalformedHash
	}
	if len(salt) == 0 || len(hash) == 0 {
		return 0, 0, 0, nil, nil, errMalformedHash
	}
	return memory, passes, threads, salt, hash, nil
}

// decoyHash is a real argon2id hash of a value nobody can present, used to
// spend the same CPU on a login for an account that does not exist as on one
// that does. Without it, the response time answers "is there an account with
// this email?" to anyone who asks — an enumeration oracle that a rate limit
// slows down but does not remove.
//
// Built once at package init: generating it per attempt would itself be the
// timing difference it exists to hide.
var decoyHash = func() string {
	h, err := HashPassword(strings.Repeat("x", MinPasswordLength))
	if err != nil {
		panic("identity: decoy hash: " + err.Error())
	}
	return h
}()

// VerifyAgainstDecoy burns the cost of a verification against a hash that
// cannot match. Call it on the paths where no stored hash was found, and
// ignore the (always false) result.
func VerifyAgainstDecoy(plaintext string) {
	_ = VerifyPassword(plaintext, decoyHash)
}
