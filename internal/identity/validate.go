package identity

import (
	"errors"
	"time"
)

var ErrInvalidExpiry = errors.New("identity: expires_at must be in the future")

// ValidateExpiresAt rejects a PAT expiry in the past. A nil expiresAt is
// fine — a non-expiring PAT is a deliberate, supported choice (ADR-0009),
// not the thing being validated here.
func ValidateExpiresAt(expiresAt *time.Time) error {
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return ErrInvalidExpiry
	}
	return nil
}
