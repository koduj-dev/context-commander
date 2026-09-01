package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/secrets"
	"github.com/koduj-dev/context-commander/internal/skills"
)

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	_ = json.NewEncoder(w).Encode(body)
}

// maxJSONBodyBytes bounds every JSON request body this API decodes.
// Metadata bodies (names, roles, references, small text fields) never need
// more than this; the one payload-carrying exception (artifact content) has
// its own domain-level cap (artifacts.MaxSize) applied separately, before
// this would ever be hit.
const maxJSONBodyBytes = 1 << 20 // 1 MiB

// decodeJSONBody wraps r.Body in http.MaxBytesReader before decoding, so a
// body over maxJSONBodyBytes fails the read (and json.Decode) instead of a
// caller being able to make a handler buffer an unbounded body in memory —
// see docs/gotchas.md and the P2 finding this fixes: several handlers used
// to call json.NewDecoder(r.Body) with no cap at all, others used a bare
// io.LimitReader inconsistently. Every handler in this package should call
// this instead of decoding r.Body directly.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	return json.NewDecoder(r.Body).Decode(dst)
}

// writeDomainError maps a domain error to the uniform HTTP error envelope —
// the same status/code mapping regardless of which domain service the
// error came from, and the same shape adapters/mcp uses for its own error
// mapping, so a client sees one consistent error model across transports
// (docs/architecture-cookbook.md §22: "Error model including authorization
// and write conflict").
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, artifacts.ErrNotFound), errors.Is(err, artifacts.ErrDeleted), errors.Is(err, projects.ErrNotFound),
		errors.Is(err, organizations.ErrNotFound), errors.Is(err, handoffs.ErrNotFound), errors.Is(err, history.ErrNotFound),
		errors.Is(err, skills.ErrNotFound), errors.Is(err, gates.ErrNotFound), errors.Is(err, secrets.ErrNotFound):
		// A deleted artifact reads as "not found," not a distinct state —
		// same reasoning as a non-member's Resolve: don't hand out more
		// information than "you can't have this."
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, artifacts.ErrForbidden), errors.Is(err, events.ErrForbidden), errors.Is(err, organizations.ErrForbidden),
		errors.Is(err, audit.ErrForbidden), errors.Is(err, handoffs.ErrForbidden), errors.Is(err, history.ErrForbidden),
		errors.Is(err, skills.ErrForbidden), errors.Is(err, gates.ErrForbidden), errors.Is(err, secrets.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
	case errors.Is(err, artifacts.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "version conflict: the artifact was modified since your expected_version")
	case errors.Is(err, artifacts.ErrBlockedByGate):
		writeError(w, http.StatusUnprocessableEntity, "blocked_by_gate", err.Error())
	case errors.Is(err, artifacts.ErrTooLarge), errors.Is(err, history.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
	case errors.Is(err, skills.ErrNotOverridable):
		writeError(w, http.StatusConflict, "conflict", "shadowed binding is not overridable")
	case errors.Is(err, skills.ErrUnsupportedScope), errors.Is(err, gates.ErrUnsupportedScope):
		writeError(w, http.StatusBadRequest, "invalid_request", "installation-scope bindings are operator-seeded only")
	case errors.Is(err, skills.ErrScopeRefRequired), errors.Is(err, gates.ErrScopeRefRequired):
		writeError(w, http.StatusBadRequest, "invalid_request", "organization/project scope requires scope_ref")
	case errors.Is(err, skills.ErrSkillVersionMismatch):
		writeError(w, http.StatusBadRequest, "invalid_request", "skill_version_id does not belong to skill_id")
	case errors.Is(err, gates.ErrUnknownGate):
		writeError(w, http.StatusBadRequest, "invalid_request", "gate_name is not a registered provider")
	case errors.Is(err, history.ErrInvalidReference):
		writeError(w, http.StatusBadRequest, "invalid_request", "referenced conversation/session/derived_from object does not belong to this project/conversation")
	case errors.Is(err, projects.ErrInvalidResolvePath), errors.Is(err, secrets.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
