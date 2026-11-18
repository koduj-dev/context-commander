package http

import (
	"net/http"
	"time"
)

type installationVersionResponse struct {
	Current         string     `json:"current"`
	UpdateCheck     bool       `json:"update_check"`
	Latest          string     `json:"latest,omitempty"`
	URL             string     `json:"url,omitempty"`
	UpdateAvailable bool       `json:"update_available"`
	CheckedAt       *time.Time `json:"checked_at,omitempty"`
	Error           string     `json:"error,omitempty"`
}

// getVersion is GET /installation/version: the running version and, when
// the operator turned the update check on, the newest release
// (docs/adr/0043). Readable by anyone signed in: a version number is no
// secret, and the interface shows the notice to administrators only.
func (d Deps) getVersion(w http.ResponseWriter, _ *http.Request) {
	if d.Updates == nil {
		writeJSON(w, http.StatusOK, installationVersionResponse{Current: "unknown"})
		return
	}
	s := d.Updates.Status()
	writeJSON(w, http.StatusOK, installationVersionResponse{Current: s.Current, UpdateCheck: s.Enabled, Latest: s.Latest, URL: s.URL,
		UpdateAvailable: s.UpdateAvailable, CheckedAt: s.CheckedAt, Error: s.Error})
}
