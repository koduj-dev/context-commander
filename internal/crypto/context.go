package crypto

import (
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
)

// FormatVersion identifies the AAD encoding + AEAD scheme in force. Bump it
// if either ever changes incompatibly — old ciphertexts stay decryptable by
// keeping the old version's decode path around, never by silently
// reinterpreting bytes under a new format.
const FormatVersion uint8 = 1

// EncryptionContext binds a ciphertext to the resource it belongs to via
// AES-GCM's additional authenticated data (AAD) — see
// docs/adr/0005-key-hierarchy-and-artifact-size-limit.md §10. A ciphertext
// produced for one (ProjectID, ResourceType, ResourceID, ResourceVersion)
// fails to decrypt under any other, even with the correct key.
type EncryptionContext struct {
	ProjectID       uuid.UUID
	ResourceType    string // "artifact_version" | "handoff" | "history_object" | ...
	ResourceID      uuid.UUID
	ResourceVersion uint64
}

// EncodeAAD produces the canonical, deterministic byte encoding used as
// AEAD's associated data — fixed field order and fixed/length-prefixed
// widths, never ad-hoc string concatenation (which would let two different
// (field) tuples collide on the same bytes).
//
// Layout: format_version(1) | project_id(16) | len(resource_type)(1) |
// resource_type(n) | resource_id(16) | resource_version(8, big-endian).
func (ec EncryptionContext) EncodeAAD() ([]byte, error) {
	resourceTypeLen := len(ec.ResourceType)
	if resourceTypeLen > 255 {
		return nil, fmt.Errorf("crypto: resource type %q too long for AAD encoding", ec.ResourceType)
	}

	buf := make([]byte, 0, 1+16+1+resourceTypeLen+16+8)
	buf = append(buf, FormatVersion)
	buf = append(buf, ec.ProjectID[:]...)
	buf = append(buf, byte(resourceTypeLen)) //nolint:gosec // bounds-checked above (resourceTypeLen <= 255)
	buf = append(buf, ec.ResourceType...)
	buf = append(buf, ec.ResourceID[:]...)
	buf = binary.BigEndian.AppendUint64(buf, ec.ResourceVersion)
	return buf, nil
}
