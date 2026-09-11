package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
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

	handler := cchttp.NewHandler(cchttp.Deps{
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

// newUserWithToken creates a User+Principal+PAT via the owner pool (identity
// isn't project-scoped, so this doesn't need to go through the RLS-relevant
// path) and returns the caller-facing token.
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

func (f *fixture) do(method, path, token string, body []byte) *http.Response {
	f.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, f.server.URL+path, bytes.NewReader(body))
	if err != nil {
		f.t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestCreateProjectWriteReadArtifact(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("filip@example.test")

	createBody, _ := json.Marshal(map[string]string{"project": "filip/es-migration", "name": "ES Migration"})
	resp := f.do(http.MethodPost, "/v1/projects", token, createBody)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create project: status=%d body=%s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	resp = f.do(http.MethodGet, "/v1/projects/filip/es-migration", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get project: status=%d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = f.do(http.MethodPut, "/v1/projects/filip/es-migration/artifacts/content?path=notes.md&content_type=text/markdown&expected_version=0",
		token, []byte("# ES 8 notes"))
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("write artifact: status=%d body=%s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	resp = f.do(http.MethodGet, "/v1/projects/filip/es-migration/artifacts/content?path=notes.md", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read artifact: status=%d", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(got) != "# ES 8 notes" {
		t.Fatalf("read artifact body = %q, want %q", got, "# ES 8 notes")
	}

	resp = f.do(http.MethodGet, "/v1/projects/filip/es-migration/artifacts", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list artifacts: status=%d", resp.StatusCode)
	}
	list, _ := decodeList(t, resp)
	_ = resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("list artifacts: got %d entries, want 1", len(list))
	}
}

func TestWriteVersionConflict(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("owner@example.test")

	createBody, _ := json.Marshal(map[string]string{"project": "owner/conflict-test", "name": "Conflict Test"})
	_ = f.do(http.MethodPost, "/v1/projects", token, createBody).Body.Close()

	resp := f.do(http.MethodPut, "/v1/projects/owner/conflict-test/artifacts/content?path=f.txt&expected_version=0", token, []byte("v1"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first write: status=%d", resp.StatusCode)
	}

	// Same expected_version=0 again -> the artifact is now at version 1, so
	// this must be a 409, not a silent overwrite.
	resp = f.do(http.MethodPut, "/v1/projects/owner/conflict-test/artifacts/content?path=f.txt&expected_version=0", token, []byte("v1-conflicting"))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("conflicting write: status=%d, want 409. body=%s", resp.StatusCode, body)
	}
}

func TestNoTokenIsUnauthorized(t *testing.T) {
	f := newFixture(t)
	resp := f.do(http.MethodGet, "/v1/projects/anyone/anything", "", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: status=%d, want 401", resp.StatusCode)
	}
}

// createProject is a shared helper for the new-domain tests below — they
// all need a project to anchor project-scoped capability checks against,
// and repeating the raw POST /v1/projects call in every test would just be
// noise.
func (f *fixture) createProject(token, path, name string) {
	f.t.Helper()
	body, _ := json.Marshal(map[string]string{"project": path, "name": name})
	resp := f.do(http.MethodPost, "/v1/projects", token, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		f.t.Fatalf("create project %q: status=%d body=%s", path, resp.StatusCode, b)
	}
}

// decodeList unwraps the {items, next_cursor} envelope every paginated list
// endpoint returns, so a test that only cares about the rows says so. The
// second return is the cursor, for the tests that do care.
func decodeList(t *testing.T, r *http.Response) ([]map[string]any, string) {
	t.Helper()
	var body struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode list envelope: %v", err)
	}
	return body.Items, body.NextCursor
}

func TestEventsListAfterArtifactWrite(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("events@example.test")
	f.createProject(token, "events/proj", "Events Proj")

	resp := f.do(http.MethodPut, "/v1/projects/events/proj/artifacts/content?path=f.txt&expected_version=0", token, []byte("v1"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("write artifact: status=%d", resp.StatusCode)
	}

	resp = f.do(http.MethodGet, "/v1/projects/events/proj/events", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list events: status=%d", resp.StatusCode)
	}
	list, _ := decodeList(t, resp)
	if len(list) == 0 {
		t.Fatal("expected at least one event after artifact.write, got none")
	}
}

// userIDByEmail looks up a User row's id directly — the HTTP organizations
// surface takes a raw user_id in its request body (unlike every
// project-scoped endpoint, which resolves identity from the bearer token),
// so a test that adds a *different* person as a member needs their id from
// somewhere other than the token it issued them.
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

// grantInstallationAdmin makes email an installation admin directly against
// ownerPool — the test equivalent of `ccd installation grant-admin`
// (docs/adr/0017).
func (f *fixture) grantInstallationAdmin(email string) {
	f.t.Helper()
	ctx := context.Background()
	userID := f.userIDByEmail(email)
	err := db.WithoutPrincipal(ctx, f.ownerPool, func(tx pgx.Tx) error {
		_, err := installation.NewStore().Grant(ctx, tx, uuid.MustParse(userID), nil)
		return err
	})
	if err != nil {
		f.t.Fatalf("grantInstallationAdmin(%q): %v", email, err)
	}
}

func TestOrganizationCreateAndAddMember(t *testing.T) {
	f := newFixture(t)
	ownerToken := f.newUserWithToken("org-owner@example.test")
	f.newUserWithToken("org-member@example.test")
	memberUserID := f.userIDByEmail("org-member@example.test")

	createBody, _ := json.Marshal(map[string]string{"name": "Acme"})
	resp := f.do(http.MethodPost, "/v1/organizations", ownerToken, createBody)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create organization: status=%d body=%s", resp.StatusCode, b)
	}
	var org map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&org)
	_ = resp.Body.Close()
	orgID, _ := org["id"].(string)
	if orgID == "" {
		t.Fatalf("create organization: response had no id: %+v", org)
	}

	addBody, _ := json.Marshal(map[string]string{"user_id": memberUserID, "role": "MEMBER"})
	resp = f.do(http.MethodPost, "/v1/organizations/"+orgID+"/members", ownerToken, addBody)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("add_member: status=%d body=%s", resp.StatusCode, b)
	}
	var membership map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&membership)
	if membership["role"] != "MEMBER" {
		t.Fatalf("add_member: role = %+v, want MEMBER", membership["role"])
	}
}

func TestAuditListAfterArtifactWrite(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("audit@example.test")
	f.createProject(token, "audit/proj", "Audit Proj")

	resp := f.do(http.MethodPut, "/v1/projects/audit/proj/artifacts/content?path=f.txt&expected_version=0", token, []byte("v1"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("write artifact: status=%d", resp.StatusCode)
	}

	resp = f.do(http.MethodGet, "/v1/projects/audit/proj/audit", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("list audit: status=%d body=%s", resp.StatusCode, b)
	}
	list, _ := decodeList(t, resp)
	if len(list) == 0 {
		t.Fatal("expected at least one audit record after artifact.write, got none")
	}
	if got := list[0]["actor_email"]; got != "audit@example.test" {
		t.Errorf("audit record actor_email = %+v, want audit@example.test", got)
	}
	if principalID, _ := list[0]["principal_id"].(string); principalID == "" {
		t.Error("audit record principal_id is empty, want the writing principal's id")
	}
}

func TestHandoffCreateGetListRoundTrip(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("handoff@example.test")
	f.createProject(token, "handoff/proj", "Handoff Proj")

	createBody, _ := json.Marshal(map[string]any{
		"summary": "migrated ES cluster", "completed_work": "reindexed", "next_actions": "verify latency",
	})
	resp := f.do(http.MethodPost, "/v1/projects/handoff/proj/handoffs", token, createBody)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create handoff: status=%d body=%s", resp.StatusCode, b)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()
	handoffID, _ := created["id"].(string)
	if handoffID == "" {
		t.Fatalf("create handoff: response had no id: %+v", created)
	}

	resp = f.do(http.MethodGet, "/v1/projects/handoff/proj/handoffs/"+handoffID, token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get handoff: status=%d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	_ = resp.Body.Close()
	if got["summary"] != "migrated ES cluster" {
		t.Fatalf("get handoff: summary = %+v, want %q", got["summary"], "migrated ES cluster")
	}

	resp = f.do(http.MethodGet, "/v1/projects/handoff/proj/handoffs", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list handoffs: status=%d", resp.StatusCode)
	}
	list, _ := decodeList(t, resp)
	if len(list) != 1 {
		t.Fatalf("list handoffs: got %d entries, want 1", len(list))
	}
}

func TestHistoryConversationSessionObjectLineage(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("history@example.test")
	f.createProject(token, "history/proj", "History Proj")

	resp := f.do(http.MethodPost, "/v1/projects/history/proj/conversations", token, nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create conversation: status=%d body=%s", resp.StatusCode, b)
	}
	var conv map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&conv)
	_ = resp.Body.Close()
	conversationID, _ := conv["id"].(string)
	if conversationID == "" {
		t.Fatalf("create conversation: response had no id: %+v", conv)
	}

	sessionBody, _ := json.Marshal(map[string]string{
		"provider": "anthropic", "client": "claude-code", "provider_session_id": "sess-1", "relation": "started",
	})
	resp = f.do(http.MethodPost, "/v1/projects/history/proj/conversations/"+conversationID+"/sessions", token, sessionBody)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create external session: status=%d body=%s", resp.StatusCode, b)
	}
	var session map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&session)
	_ = resp.Body.Close()
	externalSessionID, _ := session["id"].(string)

	rawObjBody, _ := json.Marshal(map[string]string{
		"external_session_id": externalSessionID, "type": "raw", "content": "hello world",
	})
	resp = f.do(http.MethodPost, "/v1/projects/history/proj/conversations/"+conversationID+"/objects", token, rawObjBody)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("append raw history object: status=%d body=%s", resp.StatusCode, b)
	}
	var rawObj map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&rawObj)
	_ = resp.Body.Close()
	rawID, _ := rawObj["id"].(string)

	compactedBody, _ := json.Marshal(map[string]string{
		"external_session_id": externalSessionID, "type": "compacted", "derived_from": rawID, "content": "hello",
	})
	resp = f.do(http.MethodPost, "/v1/projects/history/proj/conversations/"+conversationID+"/objects", token, compactedBody)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("append compacted history object: status=%d", resp.StatusCode)
	}

	resp = f.do(http.MethodGet, "/v1/projects/history/proj/conversations/"+conversationID+"/objects", token, nil)
	objects, _ := decodeList(t, resp)
	_ = resp.Body.Close()
	if len(objects) != 2 {
		t.Fatalf("list history objects: got %d, want 2", len(objects))
	}

	resp = f.do(http.MethodGet, "/v1/projects/history/proj/conversations/"+conversationID+"/lineage", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get lineage: status=%d", resp.StatusCode)
	}
	var edges []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&edges)
	if len(edges) == 0 {
		t.Fatal("expected at least one lineage edge (raw -> compacted), got none")
	}
}

func TestSkillPublishInstallListEffective(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("skills@example.test")
	f.createProject(token, "skills/proj", "Skills Proj")

	resp := f.do(http.MethodGet, "/v1/projects/skills/proj", token, nil)
	var proj map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&proj)
	_ = resp.Body.Close()
	projectID, _ := proj["id"].(string)
	if projectID == "" {
		t.Fatalf("get project: response had no id: %+v", proj)
	}

	publishBody, _ := json.Marshal(map[string]any{
		"name": "code-review", "author": "filip", "content": "review checklist",
	})
	resp = f.do(http.MethodPost, "/v1/projects/skills/proj/skills", token, publishBody)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("publish skill: status=%d body=%s", resp.StatusCode, b)
	}
	var version map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&version)
	_ = resp.Body.Close()
	skillID, _ := version["skill_id"].(string)
	skillVersionID, _ := version["id"].(string)
	if skillID == "" || skillVersionID == "" {
		t.Fatalf("publish skill: response missing ids: %+v", version)
	}

	installBody, _ := json.Marshal(map[string]any{
		"scope": "project", "scope_ref": projectID, "skill_id": skillID, "skill_version_id": skillVersionID,
	})
	resp = f.do(http.MethodPost, "/v1/projects/skills/proj/skills/install", token, installBody)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("install skill: status=%d body=%s", resp.StatusCode, b)
	}
	_ = resp.Body.Close()

	resp = f.do(http.MethodGet, "/v1/projects/skills/proj/skills/effective", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list effective skills: status=%d", resp.StatusCode)
	}
	var effective []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&effective)
	if len(effective) != 1 {
		t.Fatalf("list effective skills: got %d, want 1", len(effective))
	}
}

func TestGateBindListUnbind(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("gates@example.test")
	f.createProject(token, "gates/proj", "Gates Proj")

	resp := f.do(http.MethodGet, "/v1/projects/gates/proj", token, nil)
	var proj map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&proj)
	_ = resp.Body.Close()
	projectID, _ := proj["id"].(string)

	bindBody, _ := json.Marshal(map[string]any{
		"scope": "project", "scope_ref": projectID, "gate_name": "secret-scanner", "hook": "artifact.before_write",
	})
	resp = f.do(http.MethodPost, "/v1/projects/gates/proj/gates/bindings", token, bindBody)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("bind gate: status=%d body=%s", resp.StatusCode, b)
	}
	var binding map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&binding)
	_ = resp.Body.Close()
	bindingID, _ := binding["id"].(string)
	if bindingID == "" {
		t.Fatalf("bind gate: response had no id: %+v", binding)
	}

	resp = f.do(http.MethodGet, "/v1/projects/gates/proj/gates/bindings", token, nil)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	_ = resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("list gate bindings: got %d, want 1", len(list))
	}

	resp = f.do(http.MethodDelete, "/v1/projects/gates/proj/gates/bindings?id="+bindingID, token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("unbind gate: status=%d body=%s", resp.StatusCode, b)
	}
}

func TestSecretCreateListDeleteNeverReturnsValue(t *testing.T) {
	f := newFixture(t)
	token := f.newUserWithToken("secrets@example.test")
	f.createProject(token, "secrets/proj", "Secrets Proj")

	createBody, _ := json.Marshal(map[string]string{"name": "github-token", "value": "ghp_supersecret"})
	resp := f.do(http.MethodPost, "/v1/projects/secrets/proj/secrets", token, createBody)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create secret: status=%d body=%s", resp.StatusCode, b)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if bytes.Contains(raw, []byte("ghp_supersecret")) {
		t.Fatalf("create secret response leaked the plaintext value: %s", raw)
	}

	resp = f.do(http.MethodGet, "/v1/projects/secrets/proj/secrets", token, nil)
	raw, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list secrets: status=%d body=%s", resp.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte("ghp_supersecret")) {
		t.Fatalf("list secrets response leaked the plaintext value: %s", raw)
	}
	var secretsEnvelope struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(raw, &secretsEnvelope)
	refs := secretsEnvelope.Items
	if len(refs) != 1 {
		t.Fatalf("list secrets: got %d entries, want 1", len(refs))
	}

	resp = f.do(http.MethodDelete, "/v1/projects/secrets/proj/secrets/github-token", token, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete secret: status=%d", resp.StatusCode)
	}
}

func TestNonMemberGetsNotFoundNotForbidden(t *testing.T) {
	f := newFixture(t)
	ownerToken := f.newUserWithToken("private-owner@example.test")
	outsiderToken := f.newUserWithToken("outsider@example.test")

	createBody, _ := json.Marshal(map[string]string{"project": "private-owner/secret", "name": "Secret"})
	_ = f.do(http.MethodPost, "/v1/projects", ownerToken, createBody).Body.Close()

	resp := f.do(http.MethodGet, "/v1/projects/private-owner/secret", outsiderToken, nil)
	defer func() { _ = resp.Body.Close() }()
	// Not a member -> RLS hides the row entirely -> 404, never a 403 that
	// would confirm the project exists (docs/adr/0015).
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("outsider GET project: status=%d, want 404", resp.StatusCode)
	}
}
