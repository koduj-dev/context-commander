package installation_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/installation"
	"github.com/koduj-dev/context-commander/internal/paging"
)

// namespaceWithBytes gives a user a personal namespace already holding
// usedBytes, seeded as the owner: the triggers are tested where the writes
// happen (internal/artifacts, internal/history, internal/skills).
func (f *fixture) namespaceWithBytes(owner principal, slug string, usedBytes int64) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			id, slug, owner.userID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO namespace_storage (namespace_id, used_bytes) VALUES ($1, $2)`, id, usedBytes)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed namespace: %v", err)
	}
	return id
}

func (f *fixture) orgNamespace(member principal, slug string) uuid.UUID {
	f.t.Helper()
	orgID, nsID := uuid.New(), uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `INSERT INTO organizations (id, name) VALUES ($1, $2)`, orgID, "Org "+slug); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx,
			`INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, 'MEMBER')`,
			orgID, member.userID); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx,
			`INSERT INTO namespaces (id, kind, slug, organization_id) VALUES ($1, 'organization', $2, $3)`,
			nsID, slug, orgID)
		return err
	})
	if err != nil {
		f.t.Fatalf("seed organization namespace: %v", err)
	}
	return nsID
}

func TestAnAdminSeesEveryNamespaceAndSetsExceptions(t *testing.T) {
	f := newFixture(t)
	f.appPool.SetStorageQuotaDefault(1000)
	admin := f.newUser("storage-admin@example.test")
	f.grant(admin.userID)
	alice := f.newUser("storage-alice@example.test")
	ns := f.namespaceWithBytes(alice, "alice", 400)

	find := func() installation.NamespaceStorage {
		t.Helper()
		page, err := f.svc.ListStorage(f.ctx, admin.principalID, admin.userID, paging.Options{})
		if err != nil {
			t.Fatalf("ListStorage: %v", err)
		}
		for _, n := range page.Items {
			if n.NamespaceID == ns {
				return n
			}
		}
		t.Fatalf("alice's namespace is not listed")
		return installation.NamespaceStorage{}
	}

	n := find()
	if n.UsedBytes != 400 || n.Kind != "personal" || n.Slug != "alice" {
		t.Errorf("listed %+v", n)
	}
	if n.LimitBytes != nil || n.Unlimited || n.EffectiveLimit == nil || *n.EffectiveLimit != 1000 {
		t.Errorf("on the default: limit=%v unlimited=%v effective=%v, want nil/false/1000", n.LimitBytes, n.Unlimited, n.EffectiveLimit)
	}

	five := int64(5000)
	if err := f.svc.SetStorageLimit(f.ctx, admin.principalID, admin.userID, ns, installation.StorageLimit{Bytes: &five}); err != nil {
		t.Fatalf("SetStorageLimit: %v", err)
	}
	if n := find(); n.LimitBytes == nil || *n.LimitBytes != 5000 || *n.EffectiveLimit != 5000 {
		t.Errorf("after an exception of 5000: limit=%v effective=%v", n.LimitBytes, n.EffectiveLimit)
	}

	if err := f.svc.SetStorageLimit(f.ctx, admin.principalID, admin.userID, ns, installation.StorageLimit{Unlimited: true}); err != nil {
		t.Fatalf("SetStorageLimit unlimited: %v", err)
	}
	if n := find(); !n.Unlimited || n.EffectiveLimit != nil {
		t.Errorf("after unlimited: unlimited=%v effective=%v", n.Unlimited, n.EffectiveLimit)
	}

	if err := f.svc.SetStorageLimit(f.ctx, admin.principalID, admin.userID, ns, installation.StorageLimit{}); err != nil {
		t.Fatalf("SetStorageLimit back to default: %v", err)
	}
	if n := find(); n.Unlimited || n.LimitBytes != nil || *n.EffectiveLimit != 1000 {
		t.Errorf("back on the default: %+v", n)
	}

	if err := f.svc.SetStorageLimit(f.ctx, admin.principalID, admin.userID, uuid.New(), installation.StorageLimit{}); !errors.Is(err, installation.ErrNotFound) {
		t.Errorf("a namespace that does not exist returned %v, want ErrNotFound", err)
	}
	minus := int64(-1)
	if err := f.svc.SetStorageLimit(f.ctx, admin.principalID, admin.userID, ns, installation.StorageLimit{Bytes: &minus}); !errors.Is(err, installation.ErrInvalidLimit) {
		t.Errorf("a negative limit returned %v, want ErrInvalidLimit", err)
	}
}

func TestOnlyAnAdminListsOrSetsStorage(t *testing.T) {
	f := newFixture(t)
	bob := f.newUser("storage-bob@example.test")
	ns := f.namespaceWithBytes(bob, "bob", 1)

	if _, err := f.svc.ListStorage(f.ctx, bob.principalID, bob.userID, paging.Options{}); !errors.Is(err, installation.ErrForbidden) {
		t.Errorf("ListStorage as a non-admin returned %v, want ErrForbidden", err)
	}
	if err := f.svc.SetStorageLimit(f.ctx, bob.principalID, bob.userID, ns, installation.StorageLimit{Unlimited: true}); !errors.Is(err, installation.ErrForbidden) {
		t.Errorf("SetStorageLimit as a non-admin on their own namespace returned %v, want ErrForbidden", err)
	}
}

func TestAUserSeesTheirOwnNamespaceAndTheirOrganizationsOnly(t *testing.T) {
	f := newFixture(t)
	f.appPool.SetStorageQuotaDefault(2048)
	carol := f.newUser("storage-carol@example.test")
	dave := f.newUser("storage-dave@example.test")
	own := f.namespaceWithBytes(carol, "carol", 10)
	f.namespaceWithBytes(dave, "dave", 20)
	org := f.orgNamespace(carol, "carols-org")
	f.orgNamespace(dave, "daves-org")

	mine, err := f.svc.MyStorage(f.ctx, carol.principalID)
	if err != nil {
		t.Fatalf("MyStorage: %v", err)
	}
	got := map[uuid.UUID]installation.NamespaceStorage{}
	for _, n := range mine {
		got[n.NamespaceID] = n
	}
	if len(mine) != 2 {
		t.Fatalf("carol sees %d namespaces, want 2 (her own and her organization's): %+v", len(mine), mine)
	}
	if n, ok := got[own]; !ok || n.UsedBytes != 10 || n.EffectiveLimit == nil || *n.EffectiveLimit != 2048 {
		t.Errorf("own namespace: %+v", n)
	}
	if n, ok := got[org]; !ok || n.UsedBytes != 0 || n.Kind != "organization" {
		t.Errorf("organization namespace: %+v", n)
	}
	if mine[0].NamespaceID != own {
		t.Errorf("the personal namespace should come first")
	}
}
