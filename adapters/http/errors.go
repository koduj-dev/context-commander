package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/projects"
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
	case errors.Is(err, artifacts.ErrNotFound), errors.Is(err, artifacts.ErrDeleted), errors.Is(err, projects.ErrNotFound):
		// A deleted artifact reads as "not found," not a distinct state —
		// same reasoning as a non-member's Resolve: don't hand out more
		// information than "you can't have this."
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, artifacts.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
	case errors.Is(err, artifacts.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "version conflict: the artifact was modified since your expected_version")
	case errors.Is(err, artifacts.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
	case errors.Is(err, projects.ErrInvalidResolvePath):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
