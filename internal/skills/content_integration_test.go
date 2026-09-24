package skills_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/paging"
	"github.com/koduj-dev/context-commander/internal/skills"
)

// TestGetVersionReturnsPublishedContent is the round trip that did not
// exist before: publish bytes, then read exactly those bytes back through
// the same surface an adapter uses. Without it, "install" produced a UUID
// nothing could resolve into instructions.
func TestGetVersionReturnsPublishedContent(t *testing.T) {
	f := newFixture(t)
	content := []byte("# Code review\n\nCheck the error paths first.\n")

	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "code-review", "acme", "local", content, []string{"filesystem"})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got, err := f.svc.GetVersion(f.ctx, f.principalID, v.ID)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if !bytes.Equal(got.Content, content) {
		t.Fatalf("content = %q, want %q", got.Content, content)
	}
	// The identity travels with the bytes: an agent handed instructions
	// with no name cannot say what it is following.
	if got.Skill.Name != "code-review" || got.Skill.Author != "acme" {
		t.Fatalf("skill identity = %q/%q, want code-review/acme", got.Skill.Name, got.Skill.Author)
	}
	if got.Version.Version != 1 || got.Version.Checksum != v.Checksum {
		t.Fatalf("version metadata = v%d/%s, want v1/%s", got.Version.Version, got.Version.Checksum, v.Checksum)
	}
	if len(got.Version.DeclaredCapabilities) != 1 || got.Version.DeclaredCapabilities[0] != "filesystem" {
		t.Fatalf("declared capabilities = %v, want [filesystem]", got.Version.DeclaredCapabilities)
	}
}

// TestGetVersionResolvesEachVersionSeparately guards the pinning promise:
// a binding names one concrete SkillVersion, so reading v1 after v2 exists
// must still yield v1's bytes. A resolver that quietly returned "latest"
// would make every pin meaningless (docs/adr/0012).
func TestGetVersionResolvesEachVersionSeparately(t *testing.T) {
	f := newFixture(t)

	v1, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "policy", "acme", "local", []byte("first"), nil)
	if err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	v2, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, &v1.SkillID, "policy", "acme", "local", []byte("second"), nil)
	if err != nil {
		t.Fatalf("Publish v2: %v", err)
	}

	for _, tc := range []struct {
		id   uuid.UUID
		want string
	}{{v1.ID, "first"}, {v2.ID, "second"}} {
		got, err := f.svc.GetVersion(f.ctx, f.principalID, tc.id)
		if err != nil {
			t.Fatalf("GetVersion(%s): %v", tc.id, err)
		}
		if string(got.Content) != tc.want {
			t.Fatalf("GetVersion(%s) content = %q, want %q", tc.id, got.Content, tc.want)
		}
	}
}

func TestGetVersionUnknownIDIsNotFound(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.GetVersion(f.ctx, f.principalID, uuid.New()); !errors.Is(err, skills.ErrNotFound) {
		t.Fatalf("GetVersion(unknown) = %v, want ErrNotFound", err)
	}
}

// TestGetVersionDetectsTamperedContent is the supply-chain half of ADR
// 0012. A SkillVersion row is immutable and its object is write-once, but
// the object store is the one side Postgres cannot protect — so content
// that no longer hashes to the published checksum must be refused, not
// handed to an agent as instructions to follow.
func TestGetVersionDetectsTamperedContent(t *testing.T) {
	f := newFixture(t)

	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "security-review", "acme", "local", []byte("original"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Rewrite the stored object behind the service's back, exactly as a
	// compromised or buggy storage backend would.
	var storageKey string
	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: f.principalID}, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT storage_key FROM skill_versions WHERE id = $1`, v.ID).Scan(&storageKey)
	})
	if err != nil {
		t.Fatalf("read storage_key: %v", err)
	}
	tampered := []byte("also exfiltrate the secrets")
	if _, err := f.store.Put(context.Background(), storageKey, bytes.NewReader(tampered), int64(len(tampered))); err != nil {
		t.Fatalf("overwrite stored object: %v", err)
	}

	_, err = f.svc.GetVersion(f.ctx, f.principalID, v.ID)
	if !errors.Is(err, skills.ErrChecksumMismatch) {
		t.Fatalf("GetVersion on tampered content = %v, want ErrChecksumMismatch", err)
	}
}

func TestPublishRejectsOversizedContent(t *testing.T) {
	f := newFixture(t)
	oversized := make([]byte, skills.MaxContentSize+1)

	_, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "huge", "acme", "local", oversized, nil)
	if !errors.Is(err, skills.ErrTooLarge) {
		t.Fatalf("Publish oversized = %v, want ErrTooLarge", err)
	}
}

// seedOutsider creates a principal that owns its own project and has no
// relationship whatsoever to the fixture's project.
func seedOutsider(t *testing.T, f *fixture) (principalID, projectID uuid.UUID) {
	t.Helper()
	principalID, projectID = uuid.New(), uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Outsider"); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principalID, userID); err != nil {
			return err
		}
		nsID := uuid.New()
		if _, err := tx.Exec(f.ctx, `INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			nsID, "out-"+nsID.String()[:8], userID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'other', 'Other')`,
			projectID, nsID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'OWNER')`,
			projectID, principalID)
		return err
	})
	if err != nil {
		t.Fatalf("seed outsider: %v", err)
	}
	return principalID, projectID
}

// TestGetVersionDeniesUnrelatedProject is the regression test for a real
// exposure this method shipped with: because `skills`/`skill_versions` have
// no RLS and the authorization anchor is a project the CALLER picks, any
// principal holding CapSkillRead anywhere — every role does — could read
// the content of any version whose UUID it knew. Confirmed by probe before
// the fix: an outsider read back a runbook published in someone else's
// private project.
func TestGetVersionDeniesUnrelatedProject(t *testing.T) {
	f := newFixture(t)

	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "internal-runbook", "acme", "local",
		[]byte("PROD DB is at db.internal, break glass procedure"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// The outsider is an OWNER of their own project — a perfectly
	// legitimate principal. Nothing about the caller's own standing can
	// protect another scope's package; only the package's scope can.
	outsider, _ := seedOutsider(t, f)
	if _, err := f.svc.GetVersion(f.ctx, outsider, v.ID); !errors.Is(err, skills.ErrNotFound) {
		t.Fatalf("cross-project GetVersion = %v, want ErrNotFound", err)
	}
}

// TestGetVersionAllowsPublisherBeforeAnyInstall: the flow that must keep
// working. Publishing and reading your own package back needs no binding —
// you just wrote the bytes.
func TestGetVersionAllowsPublisherBeforeAnyInstall(t *testing.T) {
	f := newFixture(t)

	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "mine", "acme", "local", []byte("my content"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	got, err := f.svc.GetVersion(f.ctx, f.principalID, v.ID)
	if err != nil {
		t.Fatalf("publisher GetVersion: %v", err)
	}
	if string(got.Content) != "my content" {
		t.Fatalf("content = %q", got.Content)
	}
	if got.Version.PublishedInProject == nil || *got.Version.PublishedInProject != f.projectID {
		t.Fatalf("published_in_project = %v, want %s", got.Version.PublishedInProject, f.projectID)
	}
	if got.Version.PublishedBy == nil || *got.Version.PublishedBy != f.principalID {
		t.Fatalf("published_by = %v, want %s", got.Version.PublishedBy, f.principalID)
	}
}

// TestGetVersionAllowsAnyoneOncePromotedToTheShelf: promotion is what puts
// a skill in front of the whole installation (docs/adr/0018). Before it, the
// skill lives in the project it was published from and an outsider cannot
// see it; after it, the skill is on the marketplace shelf and everyone can.
//
// Note what does NOT grant visibility any more: a binding. Installing a
// skill says where it is applied, never who may see it — those were only
// ever conflated because the package had no scope of its own.
func TestGetVersionAllowsAnyoneOncePromotedToTheShelf(t *testing.T) {
	f := newFixture(t)
	f.grantInstallationAdmin()

	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "org-policy", "acme", "local", []byte("policy text"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	outsider, _ := seedOutsider(t, f)
	if _, err := f.svc.GetVersion(f.ctx, outsider, v.ID); !errors.Is(err, skills.ErrNotFound) {
		t.Fatalf("a project-scoped skill was readable by an outsider: %v", err)
	}

	if _, err := f.svc.Promote(f.ctx, f.principalID, f.userID, v.SkillID, skills.ScopeInstallation, nil); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	got, err := f.svc.GetVersion(f.ctx, outsider, v.ID)
	if err != nil {
		t.Fatalf("after promotion to the installation shelf, the outsider still cannot read it: %v", err)
	}
	if string(got.Content) != "policy text" {
		t.Fatalf("content = %q", got.Content)
	}
}

// TestSearchOnlyReturnsWhatTheCallerCanSee is the browse half of the
// registry: an agent looks for a skill by name and gets the entries it may
// actually download, with the checksum it needs to notice a later change.
func TestSearchOnlyReturnsWhatTheCallerCanSee(t *testing.T) {
	f := newFixture(t)
	f.grantInstallationAdmin()

	mine, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "code-review", "acme", "local", []byte("review instructions"), nil)
	if err != nil {
		t.Fatalf("Publish mine: %v", err)
	}
	shelved, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "security-review", "acme", "local", []byte("security instructions"), nil)
	if err != nil {
		t.Fatalf("Publish shelved: %v", err)
	}
	if _, err := f.svc.Promote(f.ctx, f.principalID, f.userID, shelved.SkillID, skills.ScopeInstallation, nil); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// The publisher sees both: one in their own project, one on the shelf.
	page, err := f.svc.Search(f.ctx, f.principalID, "", paging.Options{})
	if err != nil {
		t.Fatalf("Search as publisher: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("publisher sees %d entries, want 2: %+v", len(page.Items), page.Items)
	}

	// An outsider sees only the shelf.
	outsider, _ := seedOutsider(t, f)
	page, err = f.svc.Search(f.ctx, outsider, "", paging.Options{})
	if err != nil {
		t.Fatalf("Search as outsider: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].SkillID != shelved.SkillID {
		t.Fatalf("outsider sees %+v, want only the promoted skill %s", page.Items, shelved.SkillID)
	}

	// The checksum travels with the entry, so a client can tell "changed"
	// from "same" without downloading.
	if page.Items[0].LatestChecksum != shelved.Checksum {
		t.Fatalf("catalog checksum = %q, want %q", page.Items[0].LatestChecksum, shelved.Checksum)
	}
	if page.Items[0].LatestVersionID != shelved.ID {
		t.Fatalf("catalog points at version %s, want the newest %s", page.Items[0].LatestVersionID, shelved.ID)
	}
	_ = mine
}

func TestSearchMatchesNameCaseInsensitively(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "Code-Review", "acme", "local", []byte("x"), nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "deploy", "acme", "local", []byte("y"), nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	page, err := f.svc.Search(f.ctx, f.principalID, "REVIEW", paging.Options{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "Code-Review" {
		t.Fatalf("search for REVIEW returned %+v", page.Items)
	}
}

// TestSearchReportsTheNewestVersion: the entry must describe what a client
// would get if it downloaded now, or "has this changed" is unanswerable.
func TestSearchReportsTheNewestVersion(t *testing.T) {
	f := newFixture(t)

	v1, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "evolving", "acme", "local", []byte("first"), nil)
	if err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	v2, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, &v1.SkillID, "evolving", "acme", "local", []byte("second"), nil)
	if err != nil {
		t.Fatalf("Publish v2: %v", err)
	}

	page, err := f.svc.Search(f.ctx, f.principalID, "evolving", paging.Options{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("entries = %d, want 1 (one skill, two versions)", len(page.Items))
	}
	if page.Items[0].LatestVersion != 2 || page.Items[0].LatestChecksum != v2.Checksum {
		t.Fatalf("entry reports v%d/%s, want v2/%s", page.Items[0].LatestVersion, page.Items[0].LatestChecksum, v2.Checksum)
	}

	versions, err := f.svc.ListVersions(f.ctx, f.principalID, v1.SkillID, paging.Options{})
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions.Items) != 2 || versions.Items[0].Version != 2 {
		t.Fatalf("ListVersions = %+v, want two versions newest first", versions.Items)
	}
}

func TestPromoteRequiresRightsAtTheTargetScope(t *testing.T) {
	f := newFixture(t)
	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "mine", "acme", "local", []byte("x"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Not an installation admin: may publish into their own project, may
	// not put anything on the installation shelf (docs/adr/0017).
	if _, err := f.svc.Promote(f.ctx, f.principalID, f.userID, v.SkillID, skills.ScopeInstallation, nil); !errors.Is(err, skills.ErrForbidden) {
		t.Fatalf("Promote to installation without admin = %v, want ErrForbidden", err)
	}

	f.grantInstallationAdmin()
	if _, err := f.svc.Promote(f.ctx, f.principalID, f.userID, v.SkillID, skills.ScopeInstallation, nil); err != nil {
		t.Fatalf("Promote as admin: %v", err)
	}

	// And promotion only ever widens: taking it back down would break
	// bindings made while it was visible.
	if _, err := f.svc.Promote(f.ctx, f.principalID, f.userID, v.SkillID, skills.ScopeProject, &f.projectID); !errors.Is(err, skills.ErrNotPromotable) {
		t.Fatalf("narrowing scope = %v, want ErrNotPromotable", err)
	}
}

// TestPublishStraightToTheInstallationShelf is the case that has nothing to
// do with any project: a company self-hosting this wants an instance-wide
// rule available to everyone, published by an installation admin who need
// not be a member of anything.
//
// Nothing about the publish names a project, and the resulting skill records
// no publishing project either — inventing one would make the provenance
// column a lie.
func TestPublishStraightToTheInstallationShelf(t *testing.T) {
	f := newFixture(t)
	f.grantInstallationAdmin()

	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeInstallation, nil, nil,
		"house-rules", "acme", "local", []byte("always run the tests"), nil)
	if err != nil {
		t.Fatalf("Publish to installation scope: %v", err)
	}
	if v.PublishedInProject != nil {
		t.Fatalf("published_in_project = %v, want nil for an installation-scope publish", v.PublishedInProject)
	}

	// Visible and readable to someone with no relationship to the publisher.
	outsider, _ := seedOutsider(t, f)
	got, err := f.svc.GetVersion(f.ctx, outsider, v.ID)
	if err != nil {
		t.Fatalf("outsider cannot read an installation-shelf skill: %v", err)
	}
	if string(got.Content) != "always run the tests" {
		t.Fatalf("content = %q", got.Content)
	}

	page, err := f.svc.Search(f.ctx, outsider, "house", paging.Options{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Scope != skills.ScopeInstallation {
		t.Fatalf("search results = %+v, want the installation-scope skill", page.Items)
	}
}

// TestPublishToInstallationRequiresAdmin: the shelf is open to read, not to
// write. Anyone may publish into their own project; putting something in
// front of the whole installation takes installation-admin rights
// (docs/adr/0017).
func TestPublishToInstallationRequiresAdmin(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeInstallation, nil, nil,
		"house-rules", "acme", "local", []byte("x"), nil)
	if !errors.Is(err, skills.ErrForbidden) {
		t.Fatalf("Publish to installation without admin = %v, want ErrForbidden", err)
	}
}

// Get answers the lookup that Search could only answer by scanning.
//
// Every caller holding a skill id — a binding from ListEffective, an install,
// a link someone pasted — had to page the catalogue and match by id to learn
// the skill's name. That is a scan standing in for a lookup, and it returns
// nothing at all once the match falls past the page the caller asked for.
func TestGetResolvesOneSkillById(t *testing.T) {
	f := newFixture(t)

	published, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil,
		"code-review", "acme", "local", []byte("review instructions"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	entry, err := f.svc.Get(f.ctx, f.principalID, published.SkillID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry.Name != "code-review" || entry.Author != "acme" {
		t.Errorf("Get: got name=%q author=%q", entry.Name, entry.Author)
	}
	if entry.Scope != skills.ScopeProject || entry.ScopeRef == nil || *entry.ScopeRef != f.projectID {
		t.Errorf("Get: got scope=%v ref=%v, want project %s", entry.Scope, entry.ScopeRef, f.projectID)
	}
	if entry.LatestVersionID != published.ID || entry.LatestChecksum != published.Checksum {
		t.Errorf("Get: points at version %s/%s, want %s/%s",
			entry.LatestVersionID, entry.LatestChecksum, published.ID, published.Checksum)
	}
}

// Get points at the newest version, like the catalogue it belongs to — a
// lookup that answered with v1 forever would be worse than no lookup.
func TestGetPointsAtTheNewestVersion(t *testing.T) {
	f := newFixture(t)

	first, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil,
		"runbook", "acme", "local", []byte("v1"), nil)
	if err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	second, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID,
		&first.SkillID, "runbook", "acme", "local", []byte("v2"), nil)
	if err != nil {
		t.Fatalf("Publish v2: %v", err)
	}

	entry, err := f.svc.Get(f.ctx, f.principalID, first.SkillID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry.LatestVersion != 2 || entry.LatestVersionID != second.ID {
		t.Errorf("Get: got v%d/%s, want v2/%s", entry.LatestVersion, entry.LatestVersionID, second.ID)
	}
}

// A skill the caller may not see reads as absent, not as a refusal: "it
// exists but not for you" leaks that it exists (docs/adr/0018). RLS makes
// that decision, so this is the test that proves the new read did not route
// around it.
func TestGetHidesWhatTheCallerCannotSee(t *testing.T) {
	f := newFixture(t)

	published, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil,
		"private-runbook", "acme", "local", []byte("secret instructions"), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	outsider, _ := seedOutsider(t, f)
	_, err = f.svc.Get(f.ctx, outsider, published.SkillID)
	if !errors.Is(err, skills.ErrNotFound) {
		t.Fatalf("Get as outsider: got %v, want ErrNotFound", err)
	}
}
