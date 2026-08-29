// Package secrets defines the secret:// reference model — secrets are
// never stored as ordinary artifacts, handoffs, or other CONTENT PAYLOAD,
// only referenced via a Reference resolved through a Backend — see
// docs/architecture-cookbook.md §15 and
// docs/adr/0011-encryption-classification.md's SECRET classification.
package secrets

import (
	"errors"
)

// ErrNotFound is returned when a Reference has no stored value.
var ErrNotFound = errors.New("secrets: not found")

// ErrForbidden is returned when AuthzService.Can denies the requested
// capability, or when a Reference's scope doesn't match the project a
// caller is actually authorized against — see
// internal/artifacts.ErrForbidden for the same reasoning.
var ErrForbidden = errors.New("secrets: forbidden")
