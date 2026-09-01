package secrets_test

import (
	"errors"
	"strings"
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

// TestValidateNameAccepts round-trips every accepted name through
// NewProjectReference/String/Parse — regression-guarding the P3 finding
// where a name Create accepted (e.g. "team/github") produced a Reference
// string Parse itself then rejected.
func TestValidateNameAccepts(t *testing.T) {
	names := []string{
		"github",
		"github-token",
		"github_token",
		"github.token",
		"a",
		strings.Repeat("x", 200), // exactly at the length limit
	}
	for _, name := range names {
		if err := secrets.ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q): got err=%v, want nil", name, err)
			continue
		}
		ref := secrets.NewProjectReference(uuid.New(), name)
		got, err := secrets.Parse(ref.String())
		if err != nil {
			t.Errorf("Parse(String(%q)): %v", name, err)
			continue
		}
		if got != ref {
			t.Errorf("round trip mismatch for %q: got %+v, want %+v", name, got, ref)
		}
	}
}

func TestValidateNameRejects(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"slash":     "team/github",
		"oversized": strings.Repeat("x", 201),
		"control":   "github\ntoken",
	}
	for label, name := range cases {
		if err := secrets.ValidateName(name); !errors.Is(err, secrets.ErrInvalidName) {
			t.Errorf("%s: ValidateName(%q): got err=%v, want ErrInvalidName", label, name, err)
		}
	}
}
