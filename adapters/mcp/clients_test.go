package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NEXT.md Phase 1 asks for two concrete MCP client implementations, with
// "their configuration as an executable smoke test or at least a versioned
// fixture". This file is both: it reads the committed fixtures in
// deploy/mcp-clients/, derives from each one the request a real client
// would send, and then makes that exact request against a real server.
//
// Reading the fixtures rather than restating their contents is the point. A
// test with its own hardcoded copy of the config would keep passing while
// the file a user actually copies rots.
const (
	claudeCodeFixture = "../../deploy/mcp-clients/claude-code.mcp.json"
	codexFixture      = "../../deploy/mcp-clients/codex-config.toml"

	// Both fixtures must read the token from the environment, never embed
	// it: a project-local MCP config is exactly the file that gets
	// committed by accident.
	tokenEnvVar = "CC_TOKEN"
)

// clientConfig is the wire behavior a fixture describes, reduced to the two
// things that decide whether a client can talk to this server at all.
type clientConfig struct {
	endpointPath string // the path component of the configured URL
	authHeader   string // the Authorization header the client would send, with the token still as a placeholder
}

// parseClaudeCodeFixture reads the .mcp.json shape Claude Code uses:
// {"mcpServers": {"<name>": {"type": "http", "url": ..., "headers": {...}}}}.
func parseClaudeCodeFixture(t *testing.T) clientConfig {
	t.Helper()
	raw, err := os.ReadFile(claudeCodeFixture)
	if err != nil {
		t.Fatalf("read %s: %v", claudeCodeFixture, err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("%s is not valid JSON: %v", claudeCodeFixture, err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("%s declares %d servers, want exactly 1", claudeCodeFixture, len(cfg.MCPServers))
	}
	for name, server := range cfg.MCPServers {
		// "http" is Claude Code's Streamable HTTP transport. A fixture that
		// said "stdio" would describe a server this binary does not run.
		if server.Type != "http" {
			t.Fatalf("%s server %q has type %q, want \"http\"", claudeCodeFixture, name, server.Type)
		}
		if !strings.Contains(server.URL, tokenEnvVar) && !strings.Contains(server.Headers["Authorization"], tokenEnvVar) {
			t.Fatalf("%s does not read the token from $%s — it must not embed a credential", claudeCodeFixture, tokenEnvVar)
		}
		return clientConfig{endpointPath: pathOf(t, server.URL), authHeader: server.Headers["Authorization"]}
	}
	panic("unreachable: the length check above guarantees one iteration")
}

// parseCodexFixture reads the handful of keys Codex CLI's config.toml needs
// for a Streamable HTTP server. A line-oriented reader, not a TOML library:
// pulling in a parser dependency to read two keys out of a six-line file
// would cost more than it checks.
func parseCodexFixture(t *testing.T) clientConfig {
	t.Helper()
	raw, err := os.ReadFile(codexFixture)
	if err != nil {
		t.Fatalf("read %s: %v", codexFixture, err)
	}
	text := string(raw)

	if !regexp.MustCompile(`(?m)^\[mcp_servers\.[A-Za-z0-9_]+\]`).MatchString(text) {
		t.Fatalf("%s has no [mcp_servers.<name>] table; Codex would not see a server at all", codexFixture)
	}
	url := tomlString(t, text, "url")
	// Codex selects the transport from the shape of the entry: a `url` key
	// means Streamable HTTP, and there is no transport field. A `command`
	// key would silently make it a stdio server instead.
	if regexp.MustCompile(`(?m)^\s*command\s*=`).MatchString(text) {
		t.Fatalf("%s sets `command`, which makes Codex launch a stdio subprocess instead of connecting to the HTTP server", codexFixture)
	}
	envVar := tomlString(t, text, "bearer_token_env_var")
	if envVar != tokenEnvVar {
		t.Fatalf("%s reads the token from $%s, want $%s", codexFixture, envVar, tokenEnvVar)
	}
	// Codex reads that variable at connect time and sends it as a bearer
	// token; expressing it this way lets both clients be exercised through
	// the same code path below.
	return clientConfig{endpointPath: pathOf(t, url), authHeader: "Bearer ${" + envVar + "}"}
}

func tomlString(t *testing.T, text, key string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*"([^"]*)"`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("%s has no %s key", codexFixture, key)
	}
	return m[1]
}

// envPlaceholder matches Claude Code's ${VAR} and ${VAR:-default} syntax.
// Substituting the default (or nothing) is what the client itself does for
// an unset variable, which is exactly the situation a reader copying the
// fixture is in before they export anything.
var envPlaceholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// pathOf extracts the URL's path. The test cares which endpoint the client
// hits, not which host the reader happens to run it on — so the host is
// discarded and any unset placeholder collapses to its default first.
func pathOf(t *testing.T, rawURL string) string {
	t.Helper()
	expanded := envPlaceholder.ReplaceAllString(rawURL, "$2")
	trimmed := expanded
	if i := strings.Index(trimmed, "://"); i >= 0 {
		trimmed = trimmed[i+3:]
	}
	if i := strings.Index(trimmed, "/"); i >= 0 {
		return trimmed[i:]
	}
	t.Fatalf("configured URL %q (expanded: %q) has no path; it must point at the server's /mcp endpoint", rawURL, expanded)
	return ""
}

// connectAs dials the test server the way the given fixture's client would:
// same endpoint path, same Authorization header shape, with the placeholder
// replaced by the real token.
func (f *fixture) connectAs(ctx context.Context, cfg clientConfig, token, clientName string) (*sdkmcp.ClientSession, error) {
	header := strings.ReplaceAll(cfg.authHeader, "${"+tokenEnvVar+"}", token)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: clientName, Version: "0.0.1"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:   f.server.URL + strings.TrimSuffix(cfg.endpointPath, "/mcp"),
		HTTPClient: &http.Client{Transport: fixtureTransport{header: header}},
	}
	return client.Connect(ctx, transport, nil)
}

// fixtureTransport sends exactly the header the fixture describes — not
// f.connect's known-good one. If a fixture stopped producing a usable
// Authorization header, the handshake below must fail.
type fixtureTransport struct{ header string }

func (t fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.header != "" {
		req.Header.Set("Authorization", t.header)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// TestSupportedClientFixturesConnectAndBootstrap is the guarantee behind
// "two named clients" instead of "works with any MCP client": both
// committed configurations authenticate, complete the MCP handshake, see
// the tool an agent is told to call first, and get a usable pack out of it.
func TestSupportedClientFixturesConnectAndBootstrap(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		cfg  clientConfig
	}{
		{"claude-code", parseClaudeCodeFixture(t)},
		{"codex-cli", parseCodexFixture(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Both fixtures must point at /mcp — the path adapters/cli/serve.go
			// mounts the MCP handler on.
			if tc.cfg.endpointPath != "/mcp" {
				t.Fatalf("%s endpoint path = %q, want /mcp", tc.name, tc.cfg.endpointPath)
			}

			token := f.newUserWithToken(tc.name + "@example.test")
			session, err := f.connectAs(ctx, tc.cfg, token, tc.name)
			if err != nil {
				t.Fatalf("%s could not connect with its committed configuration: %v", tc.name, err)
			}
			defer func() { _ = session.Close() }()

			tools, err := session.ListTools(ctx, nil)
			if err != nil {
				t.Fatalf("%s ListTools: %v", tc.name, err)
			}
			byName := map[string]bool{}
			for _, tool := range tools.Tools {
				byName[tool.Name] = true
			}
			// The first-call tool and the two operations that make a
			// cross-client handoff possible at all. A client that cannot see
			// these has no path to the workflow this server exists for.
			for _, required := range []string{"context.bootstrap", "handoff.current", "handoff.claim", "handoff.close", "skill.get"} {
				if !byName[required] {
					t.Fatalf("%s does not see the %s tool; advertised: %v", tc.name, required, byName)
				}
			}

			// And the tool actually works over this client's connection —
			// listing a tool proves registration, not that calling it
			// returns something an agent can use.
			project := tc.name + "/smoke"
			callTool[projectResult](t, ctx, session, "project.create", map[string]any{
				"project": project, "name": "Smoke Test",
			})
			pack := callTool[struct {
				Project struct {
					Path string `json:"path"`
				} `json:"project"`
				Role   string `json:"role"`
				Budget struct {
					LimitBytes int `json:"limit_bytes"`
				} `json:"budget"`
			}](t, ctx, session, "context.bootstrap", map[string]any{"project": project})

			if pack.Project.Path != project {
				t.Fatalf("%s bootstrap returned project %q, want %q", tc.name, pack.Project.Path, project)
			}
			if pack.Role != "OWNER" {
				t.Fatalf("%s bootstrap role = %q, want OWNER (it just created the project)", tc.name, pack.Role)
			}
			if pack.Budget.LimitBytes == 0 {
				t.Fatalf("%s bootstrap reported no budget; a pack must always state its limit", tc.name)
			}
		})
	}
}
