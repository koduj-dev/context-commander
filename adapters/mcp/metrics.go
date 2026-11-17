package mcp

import (
	"context"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/telemetry"
)

// countToolCalls records each tool call by tool and outcome (docs/adr/0038),
// never its arguments or result. A call the server could not dispatch, such
// as an unknown tool, is counted under "unknown", so a client cannot mint
// label values by inventing tool names.
func countToolCalls(t *telemetry.Server) sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			start := time.Now()
			res, err := next(ctx, method, req)
			tool := "unknown"
			if call, ok := req.(*sdkmcp.CallToolRequest); ok && call.Params != nil && err == nil {
				tool = call.Params.Name
			}
			outcome := "ok"
			switch result, _ := res.(*sdkmcp.CallToolResult); {
			case err != nil:
				outcome = "error"
			case result != nil && result.IsError:
				outcome = "tool_error"
			}
			t.MCPCalls.Inc(tool, outcome)
			t.MCPDuration.Since(start, tool)
			return res, err
		}
	}
}
