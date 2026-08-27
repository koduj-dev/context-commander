package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	ccmcp "github.com/koduj-dev/context-commander/adapters/mcp"
	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

type fixture struct {
	t         *testing.T
	server    *httptest.Server
	ownerPool *db.Pool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsns := dbtest.New(t)
	ctx := context.Background()

	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}
	appPool, err := db.NewPool(ctx, dsns.App)
	if err != nil {
		t.Fatalf("connect as cc_app: %v", err)
	}

	rootKeyB64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv("CC_ROOT_KEY", rootKeyB64)
	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}
	cryptoProvider := crypto.NewProvider(root)

	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem.New: %v", err)
	}

	projStore := projects.NewStore()
	authz := auth.NewAuthzService(appPool, projStore)
	artifactsSvc := artifacts.NewService(appPool, cryptoProvider, store, authz)

	handler := ccmcp.NewHandler(ccmcp.Deps{Pool: appPool, Projects: projStore, Artifacts: artifactsSvc})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &fixture{t: t, server: server, ownerPool: ownerPool}
}

func (f *fixture) newUserWithToken(email string) string {
	f.t.Helper()
	ctx := context.Background()
	identityStore := identity.NewStore()

	var token string
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		u, err := identityStore.CreateUser(ctx, tx, email, email)
		if err != nil {
			return err
		}
		principal, err := identityStore.CreatePrincipalForUser(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		token, _, err = identityStore.IssueCredential(ctx, tx, principal.ID, "test", nil)
		return err
	})
	if err != nil {
		f.t.Fatalf("newUserWithToken: %v", err)
	}
	return token
}

// bearerTransport injects the Authorization header the MCP server's own
// authenticateMiddleware requires — the SDK's StreamableClientTransport has
// no notion of bearer tokens itself, only a pluggable *http.Client.
type bearerTransport struct {
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func (f *fixture) connect(ctx context.Context, token string) (*sdkmcp.ClientSession, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:   f.server.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
	}
	return client.Connect(ctx, transport, nil)
}

func callTool[Out any](t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, name string, args map[string]any) Out {
	t.Helper()
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): transport/protocol error: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("CallTool(%s): tool error: %v", name, result.Content)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("CallTool(%s): marshal structured content: %v", name, err)
	}
	var out Out
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("CallTool(%s): unmarshal into %T: %v", name, out, err)
	}
	return out
}

type projectResult struct {
	ID         string `json:"id"`
	Namespace  string `json:"namespace"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

type versionResult struct {
	Version     int    `json:"version"`
	ContentHash string `json:"content_hash"`
	SizeBytes   int64  `json:"size_bytes"`
}

type readResult struct {
	Version     int    `json:"version"`
	ContentHash string `json:"content_hash"`
	Content     string `json:"content"`
}

func TestProjectCreateAndArtifactRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("filip@example.test")

	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	proj := callTool[projectResult](t, ctx, session, "project.create", map[string]any{
		"project": "filip/es-migration", "name": "ES Migration",
	})
	if proj.Namespace != "filip" || proj.Slug != "es-migration" {
		t.Fatalf("project.create result = %+v", proj)
	}

	v := callTool[versionResult](t, ctx, session, "artifact.write", map[string]any{
		"project": "filip/es-migration", "path": "notes.md", "content": "# ES 8 notes", "expected_version": 0,
	})
	if v.Version != 1 {
		t.Fatalf("artifact.write version = %d, want 1", v.Version)
	}

	read := callTool[readResult](t, ctx, session, "artifact.read", map[string]any{
		"project": "filip/es-migration", "path": "notes.md",
	})
	if read.Content != "# ES 8 notes" {
		t.Fatalf("artifact.read content = %q", read.Content)
	}

	list := callTool[[]map[string]any](t, ctx, session, "artifact.list", map[string]any{
		"project": "filip/es-migration",
	})
	if len(list) != 1 {
		t.Fatalf("artifact.list returned %d entries, want 1", len(list))
	}
}

func TestArtifactWriteConflictIsToolError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("conflict@example.test")

	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	callTool[projectResult](t, ctx, session, "project.create", map[string]any{
		"project": "conflict/proj", "name": "Proj",
	})
	callTool[versionResult](t, ctx, session, "artifact.write", map[string]any{
		"project": "conflict/proj", "path": "f.txt", "content": "v1", "expected_version": 0,
	})

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact.write",
		Arguments: map[string]any{
			"project": "conflict/proj", "path": "f.txt", "content": "v1-conflicting", "expected_version": 0,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: transport/protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected a stale expected_version to produce a tool error (isError=true), got success")
	}
}

func TestConnectWithoutTokenFails(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.connect(ctx, "")
	if err == nil {
		t.Fatal("connecting without a bearer token should fail, got a session")
	}
}
