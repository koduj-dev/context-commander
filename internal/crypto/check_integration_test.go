package crypto_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
)

func rootKey(t *testing.T, env string) crypto.RootKeyProvider {
	t.Helper()
	b64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv(env, b64)
	root, err := crypto.NewEnvRootKeyProvider(env)
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}
	return root
}

func connect(t *testing.T, dsn string) *db.Pool {
	t.Helper()
	pool, err := db.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func encryptInto(t *testing.T, pool *db.Pool, p *crypto.Provider, projectID uuid.UUID, text string) func(*crypto.Provider) (string, error) {
	t.Helper()
	ctx := context.Background()
	ec := crypto.EncryptionContext{ProjectID: projectID, ResourceType: "artifact_version", ResourceID: uuid.New(), ResourceVersion: 1}
	var ct, nonce, dek []byte
	var version int
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		ct, nonce, dek, version, err = p.Encrypt(ctx, tx, ec, []byte(text))
		return err
	})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return func(reader *crypto.Provider) (string, error) {
		var got []byte
		err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
			var err error
			got, err = reader.Decrypt(ctx, tx, ec, ct, nonce, dek, version)
			return err
		})
		return string(got), err
	}
}

// The startup check, as cc_app with no principal (as `ccd serve` runs it):
// a fresh database passes, the key the data was written under passes, any
// other key is ErrWrongRootKey naming both fingerprints, and a half-done
// rotation passes but names the rest.
func TestCheckRootKey(t *testing.T) {
	ctx := context.Background()
	dsns := dbtest.New(t)
	owner, app := connect(t, dsns.Owner), connect(t, dsns.App)
	first := rootKey(t, "CC_ROOT_KEY_A")
	other := rootKey(t, "CC_ROOT_KEY_B")
	_, firstRef, _ := first.CurrentKey(ctx)
	_, otherRef, _ := other.CurrentKey(ctx)

	check, err := crypto.CheckRootKey(ctx, app, first)
	if err != nil || !check.Fresh() {
		t.Fatalf("fresh database: %+v, %v", check, err)
	}

	encryptInto(t, owner, crypto.NewProvider(first), seedProject(t, ctx, owner), "a")
	encryptInto(t, owner, crypto.NewProvider(first), seedProject(t, ctx, owner), "b")
	check, err = crypto.CheckRootKey(ctx, app, first)
	if err != nil || check.Matching != 2 || len(check.Others) != 0 || check.Fresh() {
		t.Fatalf("the right key: %+v, %v", check, err)
	}

	_, err = crypto.CheckRootKey(ctx, app, other)
	if !errors.Is(err, crypto.ErrWrongRootKey) || !strings.Contains(err.Error(), firstRef) || !strings.Contains(err.Error(), otherRef) {
		t.Fatalf("the wrong key: %v", err)
	}

	// A project key minted under another key, as a server still running with
	// the old key after a rotation would.
	encryptInto(t, owner, crypto.NewProvider(other), seedProject(t, ctx, owner), "c")
	check, err = crypto.CheckRootKey(ctx, app, first)
	if err != nil || check.Matching != 2 || len(check.Others) != 1 || check.Others[0] != (crypto.RootKeyUse{Ref: otherRef, ProjectKeys: 1}) {
		t.Fatalf("a mixed database: %+v, %v", check, err)
	}
}

// `ccd root-key rotate`: everything written under the old key reads under
// the new one, the old key reads nothing, and running it again is a no-op.
// Through cc_app it refuses rather than rewrapping nothing and succeeding.
func TestRotateInstallationRootKey(t *testing.T) {
	ctx := context.Background()
	dsns := dbtest.New(t)
	owner, app := connect(t, dsns.Owner), connect(t, dsns.App)
	oldRoot := rootKey(t, "CC_ROOT_KEY_OLD")
	newRoot := rootKey(t, "CC_ROOT_KEY_NEW")
	before := crypto.NewProvider(oldRoot)
	after := crypto.NewProvider(newRoot)

	readA := encryptInto(t, owner, before, seedProject(t, ctx, owner), "first project")
	readB := encryptInto(t, owner, before, seedProject(t, ctx, owner), "second project")

	if _, err := after.RotateInstallationRootKey(ctx, app, oldRoot); err == nil || !strings.Contains(err.Error(), "cc_owner") {
		t.Fatalf("rotation through cc_app: %v", err)
	}
	if _, err := after.RotateInstallationRootKey(ctx, owner, newRoot); err == nil {
		t.Fatal("rotation from a key to itself succeeded")
	}

	n, err := after.RotateInstallationRootKey(ctx, owner, oldRoot)
	if err != nil || n != 2 {
		t.Fatalf("rotate: %d, %v", n, err)
	}
	for _, read := range []func(*crypto.Provider) (string, error){readA, readB} {
		if got, err := read(after); err != nil || !strings.HasSuffix(got, "project") {
			t.Errorf("read with the new key: %q, %v", got, err)
		}
		if _, err := read(before); err == nil {
			t.Error("the old key still reads")
		}
	}
	if check, err := crypto.CheckRootKey(ctx, app, newRoot); err != nil || check.Matching != 2 || len(check.Others) != 0 {
		t.Errorf("check after rotation: %+v, %v", check, err)
	}
	if n, err := after.RotateInstallationRootKey(ctx, owner, oldRoot); err != nil || n != 0 {
		t.Errorf("rotating again: %d, %v", n, err)
	}
}
