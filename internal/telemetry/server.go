package telemetry

import (
	"net/http"
	"runtime"
	"time"

	"github.com/koduj-dev/context-commander/internal/db"
)

// Server is the set of metrics `ccd serve` exposes.
type Server struct {
	*Registry
	HTTPRequests *Counter
	HTTPDuration *Histogram
	MCPCalls     *Counter
	MCPDuration  *Histogram
}

// NewServer registers the server's metrics. pool may be nil in tests.
func NewServer(version string, pool *db.Pool) *Server {
	r := NewRegistry()
	s := &Server{
		Registry: r,
		HTTPRequests: r.Counter("ccd_http_requests_total",
			"HTTP requests by route pattern, method and status class. The route is the router's pattern, never the path.",
			"route", "method", "status"),
		HTTPDuration: r.Histogram("ccd_http_request_duration_seconds",
			"HTTP request duration by route pattern and method. Streams count for as long as they stay open.",
			DefaultSeconds, "route", "method"),
		MCPCalls: r.Counter("ccd_mcp_tool_calls_total",
			"MCP tool calls by tool and outcome: ok, tool_error (the tool reported a failure) or error (the call failed).",
			"tool", "outcome"),
		MCPDuration: r.Histogram("ccd_mcp_tool_call_duration_seconds", "MCP tool call duration by tool.", DefaultSeconds, "tool"),
	}
	r.Info("ccd_build_info", "The running build.", map[string]string{"version": version, "go": runtime.Version()})
	start := time.Now()
	r.GaugeFunc("ccd_start_time_seconds", "When the process started, as a Unix time.", func() float64 { return float64(start.Unix()) })
	r.GaugeFunc("ccd_goroutines", "Goroutines running now.", func() float64 { return float64(runtime.NumGoroutine()) })
	r.GaugeFunc("ccd_memory_heap_bytes", "Bytes of allocated heap objects.", func() float64 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return float64(m.HeapAlloc)
	})
	if pool != nil {
		r.GaugeFunc("ccd_db_connections_acquired", "Database connections in use.", func() float64 { return float64(pool.Stats().Acquired) })
		r.GaugeFunc("ccd_db_connections_idle", "Database connections open and idle.", func() float64 { return float64(pool.Stats().Idle) })
		r.GaugeFunc("ccd_db_connections_max", "The pool's connection limit.", func() float64 { return float64(pool.Stats().Max) })
		r.GaugeFunc("ccd_db_acquire_waits_total", "Times a request had to wait for a free database connection.",
			func() float64 { return float64(pool.Stats().EmptyAcquireCount) })
	}
	return s
}

// Handler serves the metrics in the Prometheus text format.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = s.Write(w)
	})
}

// StatusClass is "2xx", "4xx" and so on: a label with five values, not one
// per status code.
func StatusClass(code int) string {
	if code < 100 || code > 599 {
		return "other"
	}
	return string(rune('0'+code/100)) + "xx"
}
