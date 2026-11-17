package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/koduj-dev/context-commander/internal/telemetry"
)

// countRequests records each request under the router's pattern for it
// (docs/adr/0038): "/v1/projects/{namespace}/{slug}/memories", never the
// path, so no project, id or query ever becomes a label. The wrapped writer
// keeps http.Flusher, which the board streams need.
func countRequests(t *telemetry.Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			t.HTTPRequests.Inc(route, r.Method, telemetry.StatusClass(status))
			t.HTTPDuration.Since(start, route, r.Method)
		})
	}
}
