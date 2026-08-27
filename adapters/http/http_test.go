package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	cchttp "github.com/koduj-dev/context-commander/adapters/http"
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

	handler := cchttp.NewHandler(cchttp.Deps{Pool: appPool, Projects: projStore, Artifacts: artifactsSvc})
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
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
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
