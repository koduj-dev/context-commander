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
	"github.com/koduj-dev/context-commander/internal/audit"
	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/events"
	"github.com/koduj-dev/context-commander/internal/gates"
	"github.com/koduj-dev/context-commander/internal/handoffs"
	"github.com/koduj-dev/context-commander/internal/history"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/members"
	"github.com/koduj-dev/context-commander/internal/organizations"
	"github.com/koduj-dev/context-commander/internal/projects"
	"github.com/koduj-dev/context-commander/internal/secrets"
	"github.com/koduj-dev/context-commander/internal/skills"
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
	artifactsSvc := artifacts.NewService(appPool, cryptoProvider, store, authz, gates.NewEngine(appPool))

	orgStore := organizations.NewStore()
	eventsSvc := events.NewService(appPool, authz)
	orgSvc := organizations.NewService(appPool, orgStore)
	auditSvc := audit.NewService(appPool, authz)
	handoffsSvc := handoffs.NewService(appPool, cryptoProvider, authz)
	historySvc := history.NewService(appPool, cryptoProvider, authz)
	installationStore := installation.NewStore()
	skillsSvc := skills.NewService(appPool, authz, orgStore, projStore, store, installationStore)
	gatesAdmin := gates.NewAdminService(appPool, authz, orgStore, projStore, installationStore)
	installationSvc := installation.NewService(appPool, installationStore, identity.NewStore())
	secretsSvc := secrets.NewService(appPool, authz, secrets.NewLocalEncryptedBackend(appPool, cryptoProvider))

	handler := ccmcp.NewHandler(ccmcp.Deps{
		Pool: appPool, Projects: projStore, Members: members.NewService(appPool, projStore, authz), Authz: authz, Artifacts: artifactsSvc,
		Events: eventsSvc, Organizations: orgSvc, Audit: auditSvc,
		Handoffs: handoffsSvc, History: historySvc,
		Skills: skillsSvc, Gates: gatesAdmin, Secrets: secretsSvc,
		Installation: installationSvc,
	})
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

// pagedResult mirrors the envelope every list tool returns — the rows plus
// a cursor when more remain. Tests that only care about the rows call
// callToolList and get them directly.
type pagedResult[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

func callToolList[T any](t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, name string, args map[string]any) []T {
	t.Helper()
	return callTool[pagedResult[T]](t, ctx, session, name, args).Items
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

	list := callToolList[map[string]any](t, ctx, session, "artifact.list", map[string]any{
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

// userIDByEmail mirrors adapters/http/http_test.go's helper of the same
// name — organization.add_member needs the target user's id, and the MCP
// tool itself only resolves the *caller's* user id from their bearer token,
// not an arbitrary other user's.
func (f *fixture) userIDByEmail(email string) string {
	f.t.Helper()
	ctx := context.Background()
	var id string
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&id)
	})
	if err != nil {
		f.t.Fatalf("userIDByEmail(%q): %v", email, err)
	}
	return id
}

func TestEventListAfterArtifactWrite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("mcp-events@example.test")
	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	callTool[projectResult](t, ctx, session, "project.create", map[string]any{"project": "mcp-events/proj", "name": "Proj"})
	callTool[versionResult](t, ctx, session, "artifact.write", map[string]any{
		"project": "mcp-events/proj", "path": "f.txt", "content": "v1", "expected_version": 0,
	})

	eventList := callToolList[map[string]any](t, ctx, session, "event.list", map[string]any{"project": "mcp-events/proj"})
	if len(eventList) == 0 {
		t.Fatal("event.list returned no events after artifact.write")
	}
}

func TestOrganizationCreateAndAddMemberMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ownerToken := f.newUserWithToken("mcp-org-owner@example.test")
	f.newUserWithToken("mcp-org-member@example.test")
	memberUserID := f.userIDByEmail("mcp-org-member@example.test")

	session, err := f.connect(ctx, ownerToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	type organizationResult struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	org := callTool[organizationResult](t, ctx, session, "organization.create", map[string]any{"name": "Acme MCP"})
	if org.ID == "" {
		t.Fatalf("organization.create: no id in result %+v", org)
	}

	type membershipResult struct {
		Role string `json:"role"`
	}
	m := callTool[membershipResult](t, ctx, session, "organization.add_member", map[string]any{
		"organization_id": org.ID, "user_id": memberUserID, "role": "MEMBER",
	})
	if m.Role != "MEMBER" {
		t.Fatalf("organization.add_member: role = %q, want MEMBER", m.Role)
	}
}

func TestAuditListAfterArtifactWrite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("mcp-audit@example.test")
	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	callTool[projectResult](t, ctx, session, "project.create", map[string]any{"project": "mcp-audit/proj", "name": "Proj"})
	callTool[versionResult](t, ctx, session, "artifact.write", map[string]any{
		"project": "mcp-audit/proj", "path": "f.txt", "content": "v1", "expected_version": 0,
	})

	records := callToolList[map[string]any](t, ctx, session, "audit.list", map[string]any{"project": "mcp-audit/proj"})
	if len(records) == 0 {
		t.Fatal("audit.list returned no records after artifact.write")
	}
}

func TestHandoffCreateGetListMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("mcp-handoff@example.test")
	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	callTool[projectResult](t, ctx, session, "project.create", map[string]any{"project": "mcp-handoff/proj", "name": "Proj"})

	type handoffResult struct {
		ID string `json:"id"`
	}
	created := callTool[handoffResult](t, ctx, session, "handoff.create", map[string]any{
		"project": "mcp-handoff/proj", "summary": "did the thing", "completed_work": "x", "next_actions": "y",
	})
	if created.ID == "" {
		t.Fatal("handoff.create returned no id")
	}

	type handoffDetailResult struct {
		Summary string `json:"summary"`
	}
	got := callTool[handoffDetailResult](t, ctx, session, "handoff.get", map[string]any{
		"project": "mcp-handoff/proj", "id": created.ID,
	})
	if got.Summary != "did the thing" {
		t.Fatalf("handoff.get summary = %q", got.Summary)
	}

	list := callToolList[handoffResult](t, ctx, session, "handoff.list", map[string]any{"project": "mcp-handoff/proj"})
	if len(list) != 1 {
		t.Fatalf("handoff.list: got %d, want 1", len(list))
	}
}

func TestHistoryLineageMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("mcp-history@example.test")
	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	callTool[projectResult](t, ctx, session, "project.create", map[string]any{"project": "mcp-history/proj", "name": "Proj"})

	type conversationResult struct {
		ID string `json:"id"`
	}
	conv := callTool[conversationResult](t, ctx, session, "history.create_conversation", map[string]any{"project": "mcp-history/proj"})

	type externalSessionResult struct {
		ID string `json:"id"`
	}
	sess := callTool[externalSessionResult](t, ctx, session, "history.create_external_session", map[string]any{
		"project": "mcp-history/proj", "conversation_id": conv.ID,
		"provider": "anthropic", "client": "claude-code", "provider_session_id": "sess-1", "relation": "started",
	})

	type historyObjectResult struct {
		ID string `json:"id"`
	}
	raw := callTool[historyObjectResult](t, ctx, session, "history.append_object", map[string]any{
		"project": "mcp-history/proj", "conversation_id": conv.ID, "external_session_id": sess.ID,
		"type": "raw", "content": "hello world",
	})
	callTool[historyObjectResult](t, ctx, session, "history.append_object", map[string]any{
		"project": "mcp-history/proj", "conversation_id": conv.ID, "external_session_id": sess.ID,
		"type": "compacted", "derived_from": raw.ID, "content": "hello",
	})

	objects := callToolList[historyObjectResult](t, ctx, session, "history.list_objects", map[string]any{
		"project": "mcp-history/proj", "conversation_id": conv.ID,
	})
	if len(objects) != 2 {
		t.Fatalf("history.list_objects: got %d, want 2", len(objects))
	}

	type lineageEdgeResult struct {
		FromID string `json:"from_id"`
		ToID   string `json:"to_id"`
	}
	edges := callTool[[]lineageEdgeResult](t, ctx, session, "history.get_lineage", map[string]any{
		"project": "mcp-history/proj", "conversation_id": conv.ID,
	})
	if len(edges) == 0 {
		t.Fatal("history.get_lineage returned no edges")
	}
}

func TestSkillPublishInstallListEffectiveMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("mcp-skills@example.test")
	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	proj := callTool[projectResult](t, ctx, session, "project.create", map[string]any{"project": "mcp-skills/proj", "name": "Proj"})

	type skillVersionResult struct {
		ID      string `json:"id"`
		SkillID string `json:"skill_id"`
	}
	v := callTool[skillVersionResult](t, ctx, session, "skill.publish", map[string]any{
		"project": "mcp-skills/proj", "name": "code-review", "content": "checklist",
	})
	if v.ID == "" || v.SkillID == "" {
		t.Fatalf("skill.publish: missing ids in %+v", v)
	}

	type skillBindingResult struct {
		ID string `json:"id"`
	}
	binding := callTool[skillBindingResult](t, ctx, session, "skill.install", map[string]any{
		"project": "mcp-skills/proj", "scope": "project", "scope_ref": proj.ID,
		"skill_id": v.SkillID, "skill_version_id": v.ID,
	})
	if binding.ID == "" {
		t.Fatal("skill.install returned no binding id")
	}

	effective := callTool[[]skillBindingResult](t, ctx, session, "skill.list_effective", map[string]any{"project": "mcp-skills/proj"})
	if len(effective) != 1 {
		t.Fatalf("skill.list_effective: got %d, want 1", len(effective))
	}
}

func TestGateBindListUnbindMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("mcp-gates@example.test")
	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	proj := callTool[projectResult](t, ctx, session, "project.create", map[string]any{"project": "mcp-gates/proj", "name": "Proj"})

	type gateBindingResult struct {
		ID string `json:"id"`
	}
	binding := callTool[gateBindingResult](t, ctx, session, "gate.bind", map[string]any{
		"project": "mcp-gates/proj", "scope": "project", "scope_ref": proj.ID,
		"gate_name": "secret-scanner", "hook": "artifact.before_write",
	})
	if binding.ID == "" {
		t.Fatal("gate.bind returned no binding id")
	}

	list := callTool[[]gateBindingResult](t, ctx, session, "gate.list_bindings", map[string]any{"project": "mcp-gates/proj"})
	if len(list) != 1 {
		t.Fatalf("gate.list_bindings: got %d, want 1", len(list))
	}

	type unbindResult struct {
		Unbound bool `json:"unbound"`
	}
	unbound := callTool[unbindResult](t, ctx, session, "gate.unbind", map[string]any{
		"project": "mcp-gates/proj", "binding_id": binding.ID,
	})
	if !unbound.Unbound {
		t.Fatal("gate.unbind: unbound = false")
	}
}

func TestSecretCreateListDeleteNeverReturnsValueMCP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.newUserWithToken("mcp-secrets@example.test")
	session, err := f.connect(ctx, token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	callTool[projectResult](t, ctx, session, "project.create", map[string]any{"project": "mcp-secrets/proj", "name": "Proj"})

	type secretReferenceResult struct {
		Reference string `json:"reference"`
		Name      string `json:"name"`
	}
	created := callTool[secretReferenceResult](t, ctx, session, "secret.create", map[string]any{
		"project": "mcp-secrets/proj", "name": "github-token", "value": "ghp_supersecret",
	})
	if created.Name != "github-token" {
		t.Fatalf("secret.create: name = %q", created.Name)
	}

	list := callToolList[secretReferenceResult](t, ctx, session, "secret.list", map[string]any{"project": "mcp-secrets/proj"})
	if len(list) != 1 {
		t.Fatalf("secret.list: got %d, want 1", len(list))
	}
	for _, ref := range list {
		if ref.Reference == "ghp_supersecret" || ref.Name == "ghp_supersecret" {
			t.Fatal("secret.list leaked the plaintext value")
		}
	}

	// There is deliberately no secret.resolve tool at all — verify a client
	// can't even name it, rather than merely not calling it.
	if _, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "secret.resolve", Arguments: map[string]any{}}); err == nil {
		t.Fatal("expected calling a non-existent secret.resolve tool to fail")
	}

	type deleteResult struct {
		Deleted bool `json:"deleted"`
	}
	del := callTool[deleteResult](t, ctx, session, "secret.delete", map[string]any{
		"project": "mcp-secrets/proj", "name": "github-token",
	})
	if !del.Deleted {
		t.Fatal("secret.delete: deleted = false")
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
