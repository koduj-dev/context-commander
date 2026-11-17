// Package telemetry counts what the server does, for its operator, without
// recording anything a project holds (docs/adr/0038). It exposes counters,
// gauges and histograms in the Prometheus text format on a separate,
// opt-in listener.
//
// What it may record is decided here, not at each call site: labels are
// route patterns ("/v1/projects/{namespace}/{slug}/memories/"), never paths;
// MCP tool names, never arguments; status classes; outcomes. No project,
// principal, client, path, query, title or body ever becomes a label, so
// nothing in /metrics is a project's content or says who did what.
//
// It is a few hundred lines rather than the Prometheus client library: the
// format is simple and stable, and a metrics endpoint is no reason to take
// on a dependency tree.
package telemetry

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Registry holds every metric the process exposes.
type Registry struct {
	mu      sync.Mutex
	metrics []metric
	names   map[string]bool
}

func NewRegistry() *Registry { return &Registry{names: map[string]bool{}} }

type metric interface {
	write(w io.Writer) error
}

func (r *Registry) add(name string, m metric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.names[name] {
		panic("telemetry: metric registered twice: " + name)
	}
	r.names[name] = true
	r.metrics = append(r.metrics, m)
}

// Write writes every metric in the Prometheus text exposition format.
func (r *Registry) Write(w io.Writer) error {
	r.mu.Lock()
	list := append([]metric(nil), r.metrics...)
	r.mu.Unlock()
	for _, m := range list {
		if err := m.write(w); err != nil {
			return err
		}
	}
	return nil
}

// series is one label combination's values, keyed by the joined label
// values.
type family struct {
	name, help, kind string
	labels           []string
	mu               sync.Mutex
	series           map[string]*seriesValue
}

type seriesValue struct {
	values []string // label values, in family.labels order
	v      float64
	// histogram
	buckets []uint64
	sum     float64
	count   uint64
}

func newFamily(name, help, kind string, labels []string) *family {
	return &family{name: name, help: help, kind: kind, labels: labels, series: map[string]*seriesValue{}}
}

func (f *family) get(values []string) *seriesValue {
	if len(values) != len(f.labels) {
		panic(fmt.Sprintf("telemetry: %s wants %d label values, got %d", f.name, len(f.labels), len(values)))
	}
	key := strings.Join(values, "\x00")
	s, ok := f.series[key]
	if !ok {
		s = &seriesValue{values: append([]string(nil), values...)}
		f.series[key] = s
	}
	return s
}

func (f *family) sorted() []*seriesValue {
	out := make([]*seriesValue, 0, len(f.series))
	for _, s := range f.series {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i].values, "\x00") < strings.Join(out[j].values, "\x00")
	})
	return out
}

func (f *family) header(w io.Writer) error {
	_, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", f.name, escapeHelp(f.help), f.name, f.kind)
	return err
}

func labelText(names, values []string, extra ...string) string {
	var parts []string
	for i, n := range names {
		parts = append(parts, n+`="`+escapeLabel(values[i])+`"`)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		parts = append(parts, extra[i]+`="`+escapeLabel(extra[i+1])+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabel(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func escapeHelp(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`).Replace(s)
}

func number(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// Counter only goes up.
type Counter struct{ f *family }

func (r *Registry) Counter(name, help string, labels ...string) *Counter {
	c := &Counter{f: newFamily(name, help, "counter", labels)}
	r.add(name, c)
	return c
}

// Add adds n to the series with these label values.
func (c *Counter) Add(n float64, values ...string) {
	c.f.mu.Lock()
	c.f.get(values).v += n
	c.f.mu.Unlock()
}

func (c *Counter) Inc(values ...string) { c.Add(1, values...) }

func (c *Counter) write(w io.Writer) error {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	if err := c.f.header(w); err != nil {
		return err
	}
	for _, s := range c.f.sorted() {
		if _, err := fmt.Fprintf(w, "%s%s %s\n", c.f.name, labelText(c.f.labels, s.values), number(s.v)); err != nil {
			return err
		}
	}
	return nil
}

// GaugeFunc is read when /metrics is scraped.
type GaugeFunc struct {
	f    *family
	read func() float64
}

func (r *Registry) GaugeFunc(name, help string, read func() float64) {
	r.add(name, &GaugeFunc{f: newFamily(name, help, "gauge", nil), read: read})
}

func (g *GaugeFunc) write(w io.Writer) error {
	if err := g.f.header(w); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s %s\n", g.f.name, number(g.read()))
	return err
}

// Info is a gauge fixed at 1 that carries facts as labels, such as the
// version.
func (r *Registry) Info(name, help string, labels map[string]string) {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values := make([]string, len(keys))
	for i, k := range keys {
		values[i] = labels[k]
	}
	c := r.Counter(name, help, keys...)
	c.f.kind = "gauge"
	c.Add(1, values...)
}

// Histogram counts observations into buckets.
type Histogram struct {
	f      *family
	bounds []float64
}

// DefaultSeconds are latency buckets from 5 ms to 10 s.
var DefaultSeconds = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

func (r *Registry) Histogram(name, help string, bounds []float64, labels ...string) *Histogram {
	h := &Histogram{f: newFamily(name, help, "histogram", labels), bounds: bounds}
	r.add(name, h)
	return h
}

func (h *Histogram) Observe(v float64, values ...string) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	s := h.f.get(values)
	if s.buckets == nil {
		s.buckets = make([]uint64, len(h.bounds))
	}
	for i, b := range h.bounds {
		if v <= b {
			s.buckets[i]++
		}
	}
	s.sum += v
	s.count++
}

// Since observes the seconds elapsed since start.
func (h *Histogram) Since(start time.Time, values ...string) {
	h.Observe(time.Since(start).Seconds(), values...)
}

func (h *Histogram) write(w io.Writer) error {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if err := h.f.header(w); err != nil {
		return err
	}
	for _, s := range h.f.sorted() {
		for i, b := range h.bounds {
			if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", h.f.name, labelText(h.f.labels, s.values, "le", number(b)), s.buckets[i]); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n%s_sum%s %s\n%s_count%s %d\n",
			h.f.name, labelText(h.f.labels, s.values, "le", "+Inf"), s.count,
			h.f.name, labelText(h.f.labels, s.values), number(s.sum),
			h.f.name, labelText(h.f.labels, s.values), s.count); err != nil {
			return err
		}
	}
	return nil
}
