package installation_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/installation"
)

// seedProjectWithArtifact creates a project the given principal is NOT a
// member of, with one artifact in it. That is the whole point: an
// installation admin is deliberately not a member of every project, so a
// count taken through their own RLS view would miss this.
func (f *fixture) seedForeignProject(owner principal) uuid.UUID {
	f.t.Helper()
	projectID := uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		namespaceID := uuid.New()
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "foreign-"+namespaceID.String()[:8], owner.userID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'foreign', 'Foreign')`,
			projectID, namespaceID); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, 'OWNER')`,
			projectID, owner.principalID); err != nil {
			return err
		}
		artifactID := uuid.New()
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO artifacts (id, project_id, logical_path, content_type, current_version, created_by_principal)
			 VALUES ($1, $2, 'notes/foreign.md', 'text/markdown', 1, $3)`,
			artifactID, projectID, owner.principalID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx,
			`INSERT INTO artifact_versions
			   (id, project_id, artifact_id, version, content_hash, size_bytes, storage_key, created_by_principal)
			 VALUES ($1, $2, $3, 1, 'deadbeef', 4096, 'objects/foreign', $4)`,
			uuid.New(), projectID, artifactID, owner.principalID)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed foreign project: %v", err)
	}
	return projectID
}

// The reason Stats needs a SECURITY DEFINER function rather than a plain
// count: an installation admin is not a member of every project, and almost
// everything worth counting is behind RLS. Counting through the admin's own
// view would report "the installation holds 0 artifacts" while it holds one
// — a wrong number, which is worse than no number.
func TestStatsCountsProjectsTheAdminCannotSee(t *testing.T) {
	f := newFixture(t)
	admin := f.newUser("stats-admin@example.test")
	f.grant(admin.userID)
	stranger := f.newUser("stranger@example.test")

	f.seedForeignProject(stranger)

	st, err := f.svc.Stats(f.ctx, admin.principalID, admin.userID)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Projects != 1 {
		t.Errorf("Projects = %d, want 1 — the admin's own RLS view is leaking into the count", st.Projects)
	}
	if st.Artifacts != 1 {
		t.Errorf("Artifacts = %d, want 1", st.Artifacts)
	}
	if st.StoredBytes != 4096 {
		t.Errorf("StoredBytes = %d, want 4096", st.StoredBytes)
	}
	// Both users exist, and neither is hidden: users has no RLS.
	if st.Users != 2 {
		t.Errorf("Users = %d, want 2", st.Users)
	}

	// Prove the premise rather than asserting it: the same count taken
	// through the admin's own view sees nothing, which is exactly the wrong
	// answer this function exists to avoid.
	var visible int64
	err = db.WithPrincipal(f.ctx, f.appPool, db.Principal{ID: admin.principalID}, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT count(*) FROM projects`).Scan(&visible)
	})
	if err != nil {
		t.Fatalf("count as admin: %v", err)
	}
	if visible != 0 {
		t.Errorf("the admin can see %d projects through RLS — this test no longer proves anything", visible)
	}
}

// A non-admin gets nothing. The counts leak existence — "47 artifacts live in
// projects you cannot open" — which is a fact about the installation and
// belongs to whoever administers it.
func TestStatsRefusesANonAdmin(t *testing.T) {
	f := newFixture(t)
	plain := f.newUser("plain@example.test")

	_, err := f.svc.Stats(f.ctx, plain.principalID, plain.userID)
	if !errors.Is(err, installation.ErrForbidden) {
		t.Fatalf("Stats as non-admin: got %v, want ErrForbidden", err)
	}
}

// An empty installation reports zeros and no timestamps, rather than failing
// on the NULL that min()/max() return over no rows.
func TestStatsOnAnEmptyInstallation(t *testing.T) {
	f := newFixture(t)
	admin := f.newUser("empty-admin@example.test")
	f.grant(admin.userID)

	st, err := f.svc.Stats(f.ctx, admin.principalID, admin.userID)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Projects != 0 || st.Artifacts != 0 || st.StoredBytes != 0 {
		t.Errorf("empty installation reports %+v", st)
	}
	if st.OldestProject != nil || st.NewestActivity != nil {
		t.Errorf("empty installation invented timestamps: oldest=%v newest=%v",
			st.OldestProject, st.NewestActivity)
	}
}
