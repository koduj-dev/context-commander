package secrets_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/secrets"
)

func TestReferenceStringParseRoundTrip(t *testing.T) {
	ref := secrets.NewProjectReference(uuid.New(), "github")

	got, err := secrets.Parse(ref.String())
	if err != nil {
		t.Fatalf("Parse(%q): %v", ref.String(), err)
	}
	if got != ref {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, ref)
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []string{
		"",
		"not-a-secret-uri",
		"secret://project",
		"secret://project/" + uuid.New().String(),
		"secret:///" + uuid.New().String() + "/name",
		"secret://project//name",
		"secret://project/not-a-uuid/name",
	}
	for _, s := range cases {
		if _, err := secrets.Parse(s); err == nil {
			t.Errorf("Parse(%q): expected error, got nil", s)
		}
	}
}
