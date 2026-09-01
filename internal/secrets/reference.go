package secrets

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// scheme is the fixed secret:// prefix — see docs/architecture-cookbook.md
// §15's `secret://project/xyz/github` example.
const scheme = "secret://"

// maxNameLength keeps a secret name comfortably bounded — it has no other
// natural limit (unlike, say, a database column) since it only ever
// appears inside a Reference string and Backend keys.
const maxNameLength = 200

// ErrInvalidName is returned by ValidateName. Service.Create/Delete and
// NewProjectReference's callers must all go through ValidateName rather
// than each re-implementing the rule — see the P3 finding this fixes: a
// name like "team/github" used to be accepted by Create and rendered into
// a Reference string that Parse itself then rejected.
var ErrInvalidName = errors.New("secrets: invalid secret name")

// ValidateName enforces the one constraint Parse's fixed three-segment
// secret://scope/scope_ref/name format actually requires — no "/" in name,
// since Parse splits on it and a Reference is a tuple, not a path — plus an
// explicit length/character policy so names round-trip through String/Parse
// and through every Backend's own key-naming rules unchanged.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidName)
	}
	if len(name) > maxNameLength {
		return fmt.Errorf("%w: name exceeds %d characters", ErrInvalidName, maxNameLength)
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf(`%w: name must not contain "/" — a Reference is a fixed three-segment tuple, not a path`, ErrInvalidName)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: name must not contain control characters", ErrInvalidName)
		}
	}
	return nil
}

// Reference is the stable, backend-independent identity of a secret —
// what every other domain stores/passes around instead of a plaintext
// value. Scope is "project" in v1 (secrets are project metadata, per
// §5.4); the field exists for a future installation/organization scope
// without changing the wire format.
type Reference struct {
	Scope    string
	ScopeRef uuid.UUID
	Name     string
}

// NewProjectReference builds the v1 (and so far only) scope of Reference.
func NewProjectReference(projectID uuid.UUID, name string) Reference {
	return Reference{Scope: "project", ScopeRef: projectID, Name: name}
}

// String renders r as secret://<scope>/<scope_ref>/<name>.
func (r Reference) String() string {
	return fmt.Sprintf("%s%s/%s/%s", scheme, r.Scope, r.ScopeRef, r.Name)
}

// Parse reverses String. Name must not itself contain "/" — a Reference is
// a fixed three-segment tuple, not a path.
func Parse(s string) (Reference, error) {
	rest, ok := strings.CutPrefix(s, scheme)
	if !ok {
		return Reference{}, fmt.Errorf("secrets: parse %q: missing %q prefix", s, scheme)
	}

	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return Reference{}, fmt.Errorf("secrets: parse %q: expected %sscope/scope_ref/name", s, scheme)
	}
	scope, scopeRefStr, name := parts[0], parts[1], parts[2]
	if scope == "" || scopeRefStr == "" || name == "" {
		return Reference{}, fmt.Errorf("secrets: parse %q: empty component", s)
	}

	scopeRef, err := uuid.Parse(scopeRefStr)
	if err != nil {
		return Reference{}, fmt.Errorf("secrets: parse %q: invalid scope ref: %w", s, err)
	}

	return Reference{Scope: scope, ScopeRef: scopeRef, Name: name}, nil
}
