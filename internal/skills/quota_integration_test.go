package skills_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/quota"
	"github.com/koduj-dev/context-commander/internal/skills"
)

func (f *fixture) usedIn(namespaceID uuid.UUID) int64 {
	f.t.Helper()
	var n int64
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx,
			`SELECT COALESCE((SELECT used_bytes FROM namespace_storage WHERE namespace_id = $1), 0)`, namespaceID).Scan(&n)
	})
	if err != nil {
		f.t.Fatalf("read used bytes: %v", err)
	}
	return n
}

func (f *fixture) projectNamespace() uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT namespace_id FROM projects WHERE id = $1`, f.projectID).Scan(&id)
	})
	if err != nil {
		f.t.Fatalf("read project namespace: %v", err)
	}
	return id
}

func TestASkillVersionIsChargedToTheNamespaceItsScopeBelongsTo(t *testing.T) {
	f := newFixture(t)
	ns := f.projectNamespace()

	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil,
		"sized", "acme", "local", make([]byte, 300), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	// A second version of the same skill, published without naming the
	// scope again, still lands in the skill's own namespace.
	if _, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, &v.SkillID,
		"sized", "acme", "local", make([]byte, 200), nil); err != nil {
		t.Fatalf("Publish v2: %v", err)
	}
	if got := f.usedIn(ns); got != 500 {
		t.Errorf("used = %d after two versions of 300 and 200 bytes, want 500", got)
	}

	f.appPool.SetStorageQuotaDefault(600)
	if _, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, &v.SkillID,
		"sized", "acme", "local", make([]byte, 101), nil); !errors.Is(err, quota.ErrExceeded) {
		t.Errorf("a version that does not fit returned %v, want quota.ErrExceeded", err)
	}
}

func TestPromotingASkillMovesItsBytesAndIsRefusedWhereTheyDoNotFit(t *testing.T) {
	f := newFixture(t)
	f.grantInstallationAdmin()
	projectNS := f.projectNamespace()

	org, err := f.orgSvc.Create(f.ctx, f.principalID, f.userID, "Quota Org")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	orgNS := uuid.New()
	err = db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO namespaces (id, kind, slug, organization_id) VALUES ($1, 'organization', $2, $3)`,
			orgNS, "quota-org-"+orgNS.String()[:8], org.ID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `SELECT cc_set_namespace_quota($1, 100, false, NULL)`, orgNS)
		return err
	})
	if err != nil {
		t.Fatalf("seed organization namespace: %v", err)
	}

	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil,
		"movable", "acme", "local", make([]byte, 250), nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if _, err := f.svc.Promote(f.ctx, f.principalID, f.userID, v.SkillID, skills.ScopeOrganization, &org.ID); !errors.Is(err, quota.ErrExceeded) {
		t.Fatalf("promoting 250 bytes into a 100-byte organization returned %v, want quota.ErrExceeded", err)
	}
	if got := f.usedIn(projectNS); got != 250 {
		t.Errorf("after a refused promotion the project namespace holds %d, want 250", got)
	}

	// Installation scope belongs to no namespace: the bytes leave the
	// project's count and are charged to nobody.
	if _, err := f.svc.Promote(f.ctx, f.principalID, f.userID, v.SkillID, skills.ScopeInstallation, nil); err != nil {
		t.Fatalf("Promote to installation: %v", err)
	}
	if got := f.usedIn(projectNS); got != 0 {
		t.Errorf("after promotion to the installation the project namespace holds %d, want 0", got)
	}
}
