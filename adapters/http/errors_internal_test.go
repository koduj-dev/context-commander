package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/koduj-dev/context-commander/internal/projects"
)

// A 500 that leaves nothing in the log is undiagnosable. It happened: a
// missing column surfaced as `{"error":{"code":"internal"}}` and an empty
// log, and the cause had to be found by inspecting the schema by hand —
// writeDomainError's default branch was discarding the error it was given.
//
// These are internal tests (package http) because the functions are
// package-private, which is correct: nothing outside this adapter should be
// writing the API's error envelope.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	fn()
	return buf.String()
}

func TestUnclassifiedErrorIsLoggedAndNotDisclosed(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/installation/service-principals", nil)
	cause := errors.New("ERROR: column \"created_by\" of relation \"service_principals\" does not exist")

	out := captureLog(t, func() { writeDomainError(rec, req, cause) })

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	// The operator gets everything needed to find it.
	for _, want := range []string{"created_by", "/v1/installation/service-principals", "POST"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log does not mention %q:\n%s", want, out)
		}
	}
	// The caller gets nothing. An internal error's text is a chain of
	// package prefixes written for an operator, and some of it describes
	// rows the caller may not be allowed to see.
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "internal" {
		t.Errorf("code = %q, want internal", body.Error.Code)
	}
	if strings.Contains(rec.Body.String(), "created_by") {
		t.Errorf("the response leaked the internal cause: %s", rec.Body.String())
	}
}

func TestClassifiedErrorIsNotLogged(t *testing.T) {
	// A 404 is an ordinary answer, not an incident. Logging every one would
	// bury the 500s this logging exists for — which is how a log stops being
	// read at all.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/acme/thing", nil)

	out := captureLog(t, func() { writeDomainError(rec, req, projects.ErrNotFound) })

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("a classified error was logged:\n%s", out)
	}
}

func TestInternalErrorNamesTheOperation(t *testing.T) {
	// The difference between "unhandled error" and "could not store the
	// password" in a log someone is reading at the wrong hour.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/me/password", nil)
	cause := errors.New("connection reset by peer")

	out := captureLog(t, func() { internalError(rec, req, cause, "could not store the password") })

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	for _, want := range []string{"could not store the password", "connection reset by peer", "/v1/me/password"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log does not mention %q:\n%s", want, out)
		}
	}
}
