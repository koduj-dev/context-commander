package http

import (
	"io"
	"net/http"
	"strconv"

	"github.com/koduj-dev/context-commander/internal/artifacts"
)

type artifactResponse struct {
	LogicalPath    string `json:"logical_path"`
	ContentType    string `json:"content_type"`
	CurrentVersion int    `json:"current_version"`
}

type versionResponse struct {
	Version     int    `json:"version"`
	ContentHash string `json:"content_hash"`
	SizeBytes   int64  `json:"size_bytes"`
}

func (d Deps) listArtifacts(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.Artifacts.List(r.Context(), principalID, project.ID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	out := make([]artifactResponse, len(page.Items))
	for i, a := range page.Items {
		out[i] = artifactResponse{LogicalPath: a.LogicalPath, ContentType: a.ContentType, CurrentVersion: a.CurrentVersion}
	}
	writeList(w, out, page.NextCursor)
}

func (d Deps) statArtifact(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "?path= is required")
		return
	}
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	a, err := d.Artifacts.Stat(r.Context(), principalID, project.ID, path)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, artifactResponse{LogicalPath: a.LogicalPath, ContentType: a.ContentType, CurrentVersion: a.CurrentVersion})
}

func (d Deps) historyArtifact(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "?path= is required")
		return
	}
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	versions, err := d.Artifacts.History(r.Context(), principalID, project.ID, path)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]versionResponse, len(versions))
	for i, v := range versions {
		out[i] = versionResponse{Version: v.Version, ContentHash: v.ContentHash, SizeBytes: v.SizeBytes}
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) readArtifact(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "?path= is required")
		return
	}
	var version *int
	if raw := r.URL.Query().Get("version"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?version= must be an integer")
			return
		}
		version = &v
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	v, content, err := d.Artifacts.Read(r.Context(), principalID, project.ID, path, version)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Artifact-Version", strconv.Itoa(v.Version))
	w.Header().Set("X-Content-Hash", v.ContentHash)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content) //nolint:gosec // Content-Type is explicitly application/octet-stream above, never rendered as HTML/script
}

func (d Deps) writeArtifact(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "?path= is required")
		return
	}
	contentType := r.URL.Query().Get("content_type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	expectedVersion, err := strconv.Atoi(r.URL.Query().Get("expected_version"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "?expected_version= is required (0 for a new artifact)")
		return
	}

	content, err := io.ReadAll(io.LimitReader(r.Body, artifacts.MaxSize+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read request body")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())
	clientID := clientIDFrom(r.Context())

	v, err := d.Artifacts.Write(r.Context(), principalID, clientID, project.ID, path, contentType, content, expectedVersion)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, versionResponse{Version: v.Version, ContentHash: v.ContentHash, SizeBytes: v.SizeBytes})
}

func (d Deps) deleteArtifact(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "?path= is required")
		return
	}
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())
	clientID := clientIDFrom(r.Context())

	if err := d.Artifacts.Delete(r.Context(), principalID, clientID, project.ID, path); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
