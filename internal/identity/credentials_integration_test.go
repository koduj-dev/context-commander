package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
	"github.com/koduj-dev/context-commander/internal/identity"
)

// TestListAndRevokeCredentials exercises the self-service PAT surface: a
// principal can list their own credentials, and revoking one that belongs
// to a *different* principal fails exactly like the credential didn't
// exist — self-only scoping has no RLS backstop on client_credentials, so
// this is what actually proves RevokeCredential's WHERE clause does the job.
func TestListAndRevokeCredentials(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()

	ownerPool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect as cc_owner: %v", err)
	}

	store := identity.NewStore()
	aliceID, bobID := uuid.New(), uuid.New()
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		for _, id := range []uuid.UUID{aliceID, bobID} {
			userID := uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
				userID, id.String()+"@example.test", id.String()); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, id, userID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed principals: %v", err)
	}

	var aliceCredID uuid.UUID
	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		_, cred, err := store.IssueCredential(ctx, tx, aliceID, "test", nil)
		if err != nil {
			return err
		}
		aliceCredID = cred.ID
		_, _, err = store.IssueCredential(ctx, tx, bobID, "test", nil)
		return err
	})
	if err != nil {
		t.Fatalf("issue credentials: %v", err)
	}

	err = db.WithoutPrincipal(ctx, ownerPool, func(tx pgx.Tx) error {
		aliceList, err := store.ListCredentialsForPrincipal(ctx, tx, aliceID)
		if err != nil {
			return err
		}
		if len(aliceList) != 1 || aliceList[0].ID != aliceCredID {
			t.Fatalf("alice's credentials = %+v, want exactly [%s]", aliceList, aliceCredID)
		}

		// Bob cannot revoke Alice's credential — it must fail exactly as if
		// it didn't exist, not with some other "forbidden" distinction.
		if err := store.RevokeCredential(ctx, tx, aliceCredID, bobID); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("RevokeCredential(alice's cred, as bob) = %v, want ErrNotFound", err)
		}

		// Alice can revoke her own.
		if err := store.RevokeCredential(ctx, tx, aliceCredID, aliceID); err != nil {
			t.Fatalf("RevokeCredential(alice's cred, as alice): %v", err)
		}

		// Revoking it again is also ErrNotFound (revoked_at IS NULL no longer matches).
		if err := store.RevokeCredential(ctx, tx, aliceCredID, aliceID); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("RevokeCredential (already revoked) = %v, want ErrNotFound", err)
		}

		after, err := store.ListCredentialsForPrincipal(ctx, tx, aliceID)
		if err != nil {
			return err
		}
		if len(after) != 1 || after[0].RevokedAt == nil {
			t.Fatalf("alice's credential after revoke = %+v, want RevokedAt set", after)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestValidateExpiresAt(t *testing.T) {
	if err := identity.ValidateExpiresAt(nil); err != nil {
		t.Errorf("ValidateExpiresAt(nil) = %v, want nil", err)
	}
	future := time.Now().Add(time.Hour)
	if err := identity.ValidateExpiresAt(&future); err != nil {
		t.Errorf("ValidateExpiresAt(future) = %v, want nil", err)
	}
	past := time.Now().Add(-time.Hour)
	if err := identity.ValidateExpiresAt(&past); !errors.Is(err, identity.ErrInvalidExpiry) {
		t.Errorf("ValidateExpiresAt(past) = %v, want ErrInvalidExpiry", err)
	}
}
