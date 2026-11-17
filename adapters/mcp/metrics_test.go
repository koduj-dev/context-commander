package mcp_test

import (
	"context"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	ccmcp "github.com/koduj-dev/context-commander/adapters/mcp"
	"github.com/koduj-dev/context-commander/internal/telemetry"
)

// Tool calls are counted by tool and outcome, never by argument, and a tool
// name the server does not have cannot become a label.
func TestMetricsCountToolCalls(t *testing.T) {
	metrics := telemetry.NewServer("test", nil)
	f := newFixtureWith(t, func(d *ccmcp.Deps) { d.Telemetry = metrics })
	ctx := context.Background()
	s, err := f.connect(ctx, f.newUserWithToken("mcp-metrics@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	callTool[projectResult](t, ctx, s, "project.create", map[string]any{"project": "mcp-metrics/hidden-name", "name": "Hidden"})
	res, err := s.CallTool(ctx, &sdkmcp.CallToolParams{Name: "memory.get", Arguments: map[string]any{"project": "mcp-metrics/hidden-name", "memory_id": "not-a-uuid"}})
	if err != nil || !res.IsError {
		t.Fatalf("a failing tool call: %v %+v", err, res)
	}
	_, _ = s.CallTool(ctx, &sdkmcp.CallToolParams{Name: "invented.tool-name-xyz", Arguments: map[string]any{}})

	var b strings.Builder
	_ = metrics.Write(&b)
	out := b.String()
	for _, want := range []string{
		`ccd_mcp_tool_calls_total{tool="project.create",outcome="ok"} 1`,
		`ccd_mcp_tool_calls_total{tool="memory.get",outcome="tool_error"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, leak := range []string{"hidden-name", "invented", "not-a-uuid"} {
		if strings.Contains(out, leak) {
			t.Errorf("metrics contain %q", leak)
		}
	}
}
