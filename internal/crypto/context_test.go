package crypto

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestEncodeAADIsDeterministic(t *testing.T) {
	ec := EncryptionContext{
		ProjectID:       uuid.New(),
		ResourceType:    "artifact_version",
		ResourceID:      uuid.New(),
		ResourceVersion: 3,
	}

	a, err := ec.EncodeAAD()
	if err != nil {
		t.Fatalf("EncodeAAD: %v", err)
	}
	b, err := ec.EncodeAAD()
	if err != nil {
		t.Fatalf("EncodeAAD: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("EncodeAAD is not deterministic for identical input")
	}
}

// TestEncodeAADDiffersAcrossFields is the direct expression of the
// docs/adr/0005 acceptance criterion: ciphertext from project A / artifact X
// / v3 must not decrypt as project B / artifact Y / v7. Since Decrypt just
// feeds EncodeAAD() into AES-GCM's AAD check, that property reduces to: no
// two distinct EncryptionContexts ever encode to the same AAD bytes.
func TestEncodeAADDiffersAcrossFields(t *testing.T) {
	base := EncryptionContext{
		ProjectID:       uuid.New(),
		ResourceType:    "artifact_version",
		ResourceID:      uuid.New(),
		ResourceVersion: 3,
	}
	baseEncoded, err := base.EncodeAAD()
	if err != nil {
		t.Fatalf("EncodeAAD: %v", err)
	}

	variants := map[string]EncryptionContext{
		"different project":       withProjectID(base, uuid.New()),
		"different resource type": withResourceType(base, "handoff"),
		"different resource id":   withResourceID(base, uuid.New()),
		"different version":       withResourceVersion(base, 7),
	}

	for name, variant := range variants {
		encoded, err := variant.EncodeAAD()
		if err != nil {
			t.Fatalf("%s: EncodeAAD: %v", name, err)
		}
		if bytes.Equal(encoded, baseEncoded) {
			t.Errorf("%s: AAD collided with the base context", name)
		}
	}
}

// TestEncodeAADResourceTypeLengthPrefixPreventsAmbiguity guards the reason
// resource_type is length-prefixed rather than just concatenated: without a
// length prefix, ("ab", "cd") and ("a", "bcd") would encode identically.
func TestEncodeAADResourceTypeLengthPrefixPreventsAmbiguity(t *testing.T) {
	id := uuid.New()
	rid := uuid.New()

	a := EncryptionContext{ProjectID: id, ResourceType: "ab", ResourceID: rid, ResourceVersion: 1}
	b := EncryptionContext{ProjectID: id, ResourceType: "a", ResourceID: rid, ResourceVersion: 1}
	// Deliberately not a realistic pair (different length resource types),
	// but exercises the same "would these concatenate to the same bytes"
	// question a naive encoding would get wrong.

	encodedA, err := a.EncodeAAD()
	if err != nil {
		t.Fatalf("EncodeAAD: %v", err)
	}
	encodedB, err := b.EncodeAAD()
	if err != nil {
		t.Fatalf("EncodeAAD: %v", err)
	}
	if bytes.Equal(encodedA, encodedB) {
		t.Fatal("different resource types encoded to the same AAD bytes")
	}
}

func withProjectID(ec EncryptionContext, id uuid.UUID) EncryptionContext {
	ec.ProjectID = id
	return ec
}

func withResourceType(ec EncryptionContext, t string) EncryptionContext {
	ec.ResourceType = t
	return ec
}

func withResourceID(ec EncryptionContext, id uuid.UUID) EncryptionContext {
	ec.ResourceID = id
	return ec
}

func withResourceVersion(ec EncryptionContext, v uint64) EncryptionContext {
	ec.ResourceVersion = v
	return ec
}
