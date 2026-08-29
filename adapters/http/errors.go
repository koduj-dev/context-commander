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
	case errors.Is(err, artifacts.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
	case errors.Is(err, skills.ErrNotOverridable):
		writeError(w, http.StatusConflict, "conflict", "shadowed binding is not overridable")
	case errors.Is(err, skills.ErrUnsupportedScope), errors.Is(err, gates.ErrUnsupportedScope):
		writeError(w, http.StatusBadRequest, "invalid_request", "installation-scope bindings are operator-seeded only")
	case errors.Is(err, skills.ErrScopeRefRequired), errors.Is(err, gates.ErrScopeRefRequired):
		writeError(w, http.StatusBadRequest, "invalid_request", "organization/project scope requires scope_ref")
	case errors.Is(err, projects.ErrInvalidResolvePath):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
