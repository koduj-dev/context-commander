package telemetry

import (
	"strings"
	"testing"
)

func TestExposition(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("x_total", "Things.\nTwo lines.", "route", "status")
	c.Inc("/v1/a", "2xx")
	c.Add(2, "/v1/a", "2xx")
	c.Inc(`we"ird\`, "5xx")
	h := r.Histogram("x_seconds", "Time.", []float64{0.1, 1}, "route")
	h.Observe(0.05, "/v1/a")
	h.Observe(0.5, "/v1/a")
	r.GaugeFunc("x_now", "Now.", func() float64 { return 7 })
	r.Info("x_info", "Build.", map[string]string{"version": "v1"})

	var b strings.Builder
	if err := r.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"# HELP x_total Things.\\nTwo lines.\n# TYPE x_total counter\n",
		`x_total{route="/v1/a",status="2xx"} 3`,
		`x_total{route="we\"ird\\",status="5xx"} 1`,
		`x_seconds_bucket{route="/v1/a",le="0.1"} 1`,
		`x_seconds_bucket{route="/v1/a",le="1"} 2`,
		`x_seconds_bucket{route="/v1/a",le="+Inf"} 2`,
		`x_seconds_sum{route="/v1/a"} 0.55`,
		`x_seconds_count{route="/v1/a"} 2`,
		"x_now 7\n",
		"# TYPE x_info gauge\n" + `x_info{version="v1"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if StatusClass(404) != "4xx" || StatusClass(42) != "other" {
		t.Error("status class")
	}
}
