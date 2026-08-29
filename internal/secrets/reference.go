package secrets

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// scheme is the fixed secret:// prefix — see docs/architecture-cookbook.md
// §15's `secret://project/xyz/github` example.
const scheme = "secret://"

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
