package http_test

import (
	"net/http"
	"strings"
	"testing"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
	"github.com/koduj-dev/context-commander/internal/ratelimit"
	"github.com/koduj-dev/context-commander/internal/telemetry"
)

// Requests are counted under the router's pattern, and nothing a request
// names — a project's slug, an id, a query — appears in the metrics.
func TestMetricsCountRoutesNotPaths(t *testing.T) {
	metrics := telemetry.NewServer("test", nil)
	f := newFixtureWithDeps(t, nil, ratelimit.Limits{}, func(d *cchttp.Deps) { d.Telemetry = metrics })
	token := f.newUserWithToken("metrics@example.test")
	f.createProject(token, "metrics/secret-project-name", "Secret")
	f.mustDo(http.MethodGet, "/v1/projects/metrics/secret-project-name/memories/?q=confidential", token, nil, http.StatusOK, "list")
	f.mustDo(http.MethodGet, "/v1/projects/metrics/nope/memories/", token, nil, http.StatusNotFound, "missing")

	var b strings.Builder
	if err := metrics.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	// A project that does not exist is refused before the subrouter, so its
	// request counts under the project mount's catch-all.
	if !strings.Contains(out, `ccd_http_requests_total{route="/v1/projects/{namespace}/{slug}/memories",method="GET",status="2xx"} 1`) ||
		!strings.Contains(out, `ccd_http_requests_total{route="/v1/projects/{namespace}/{slug}/*",method="GET",status="4xx"} 1`) {
		t.Errorf("route counts missing:\n%s", out)
	}
	for _, leak := range []string{"secret-project-name", "confidential", "metrics@example.test", "nope"} {
		if strings.Contains(out, leak) {
			t.Errorf("metrics contain %q", leak)
		}
	}
}
