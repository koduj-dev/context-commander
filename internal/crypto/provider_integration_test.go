package crypto_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/crypto"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/dbtest"
)

func newTestProvider(t *testing.T) *crypto.Provider {
	t.Helper()
	rootKeyB64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv("CC_ROOT_KEY", rootKeyB64)

	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}
	return crypto.NewProvider(root)
}

// seedProject creates just enough (a personal namespace + project) to
// satisfy project_keys' FK — crypto correctness, not RBAC/RLS, is what this
// package's tests are about; that's covered in internal/auth.
func seedProject(t *testing.T, ctx context.Context, pool *db.Pool) uuid.UUID {
	t.Helper()
	var projectID uuid.UUID
	err := db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		userID := uuid.New()
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
			userID, userID.String()+"@example.test", "Fixture User"); err != nil {
			return err
		}
		namespaceID := uuid.New()
		if _, err := tx.Exec(ctx,
			`INSERT INTO namespaces (id, kind, slug, owner_user_id) VALUES ($1, 'personal', $2, $3)`,
			namespaceID, "fixture-"+namespaceID.String()[:8], userID); err != nil {
			return err
		}
		projectID = uuid.New()
		_, err := tx.Exec(ctx,
			`INSERT INTO projects (id, namespace_id, slug, name) VALUES ($1, $2, 'fixture', 'Fixture')`,
			projectID, namespaceID)
		return err
	})
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return projectID
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	projectID := seedProject(t, ctx, pool)
	provider := newTestProvider(t)

	ec := crypto.EncryptionContext{
		ProjectID: projectID, ResourceType: "artifact_version",
		ResourceID: uuid.New(), ResourceVersion: 1,
	}
	plaintext := []byte("secret artifact content")

	var ciphertext, nonce, wrappedDEK []byte
	var version int
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		ciphertext, nonce, wrappedDEK, version, err = provider.Encrypt(ctx, tx, ec, plaintext)
		return err
	})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if version != 1 {
		t.Errorf("first Encrypt for a project should auto-provision project_key_version=1, got %d", version)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext equals plaintext — not actually encrypted")
	}

	var got []byte
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		got, err = provider.Decrypt(ctx, tx, ec, ciphertext, nonce, wrappedDEK, version)
		return err
	})
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt returned %q, want %q", got, plaintext)
	}
}

// TestDecryptFailsOnMismatchedContext is the end-to-end version of the AAD
// unit tests in context_test.go — proves it holds through the real Encrypt/
// Decrypt path against a real project key, not just at the AAD-encoding
// layer. docs/adr/0005 acceptance criterion.
func TestDecryptFailsOnMismatchedContext(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	projectAID := seedProject(t, ctx, pool)
	projectBID := seedProject(t, ctx, pool)
	provider := newTestProvider(t)

	ecA := crypto.EncryptionContext{
		ProjectID: projectAID, ResourceType: "artifact_version",
		ResourceID: uuid.New(), ResourceVersion: 3,
	}

	var ciphertext, nonce, wrappedDEK []byte
	var version int
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		ciphertext, nonce, wrappedDEK, version, err = provider.Encrypt(ctx, tx, ecA, []byte("project A secret"))
		return err
	})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	ecB := crypto.EncryptionContext{
		ProjectID: projectBID, ResourceType: "artifact_version", // different project
		ResourceID: ecA.ResourceID, ResourceVersion: ecA.ResourceVersion,
	}

	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		_, err := provider.Decrypt(ctx, tx, ecB, ciphertext, nonce, wrappedDEK, version)
		return err
	})
	// project B has its own project_keys, so this typically fails at the
	// DEK-unwrap step (wrong key) rather than reaching the content AAD
	// check — either way, decrypt must fail, never silently succeed.
	if err == nil {
		t.Fatal("Decrypt succeeded across projects — expected failure")
	}
}

func TestRotateProjectKeyDoesNotBreakOldCiphertexts(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	projectID := seedProject(t, ctx, pool)
	provider := newTestProvider(t)

	ec := crypto.EncryptionContext{
		ProjectID: projectID, ResourceType: "artifact_version",
		ResourceID: uuid.New(), ResourceVersion: 1,
	}

	var ciphertext, nonce, wrappedDEK []byte
	var oldVersion int
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		ciphertext, nonce, wrappedDEK, oldVersion, err = provider.Encrypt(ctx, tx, ec, []byte("v1 content"))
		return err
	})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return provider.RotateProjectKey(ctx, tx, projectID)
	})
	if err != nil {
		t.Fatalf("RotateProjectKey: %v", err)
	}

	// The old ciphertext, still referencing oldVersion, must remain
	// decryptable — rotation must not have touched it.
	var got []byte
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		got, err = provider.Decrypt(ctx, tx, ec, ciphertext, nonce, wrappedDEK, oldVersion)
		return err
	})
	if err != nil {
		t.Fatalf("Decrypt after RotateProjectKey: %v", err)
	}
	if string(got) != "v1 content" {
		t.Fatalf("got %q after rotation, want %q", got, "v1 content")
	}

	// A new Encrypt after rotation must use the new (higher) version.
	var newVersion int
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		_, _, _, newVersion, err = provider.Encrypt(ctx, tx, crypto.EncryptionContext{
			ProjectID: projectID, ResourceType: "artifact_version",
			ResourceID: uuid.New(), ResourceVersion: 1,
		}, []byte("v2 content"))
		return err
	})
	if err != nil {
		t.Fatalf("Encrypt after rotation: %v", err)
	}
	if newVersion <= oldVersion {
		t.Fatalf("post-rotation Encrypt used project_key_version=%d, want > %d", newVersion, oldVersion)
	}
}

func TestRotateRootKeyKeepsProjectKeysDecryptable(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	projectID := seedProject(t, ctx, pool)

	rootKeyB64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv("CC_ROOT_KEY", rootKeyB64)
	root, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}
	provider := crypto.NewProvider(root)

	ec := crypto.EncryptionContext{
		ProjectID: projectID, ResourceType: "artifact_version",
		ResourceID: uuid.New(), ResourceVersion: 1,
	}

	var ciphertext, nonce, wrappedDEK []byte
	var version int
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		ciphertext, nonce, wrappedDEK, version, err = provider.Encrypt(ctx, tx, ec, []byte("pre-rotation content"))
		return err
	})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Simulate a root key rotation: a NEW root key is now current...
	newRootKeyB64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv("CC_ROOT_KEY", newRootKeyB64)
	newRoot, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}
	rotatingProvider := crypto.NewProvider(newRoot)

	// ...and RotateRootKey rewraps every project_keys row from the old key
	// (`root`, constructed above before CC_ROOT_KEY changed, so it still
	// holds the old key material) to the new one.
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return rotatingProvider.RotateRootKey(ctx, tx, root)
	})
	if err != nil {
		t.Fatalf("RotateRootKey: %v", err)
	}

	// The artifact ciphertext itself never changed — decrypting it under
	// the SAME project_key_version, now via the rotated-root-key-aware
	// provider, must still work. This proves RotateRootKey rewrapped the
	// project key without touching (or needing to touch) the DEK/ciphertext.
	var got []byte
	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		var err error
		got, err = rotatingProvider.Decrypt(ctx, tx, ec, ciphertext, nonce, wrappedDEK, version)
		return err
	})
	if err != nil {
		t.Fatalf("Decrypt after RotateRootKey: %v", err)
	}
	if string(got) != "pre-rotation content" {
		t.Fatalf("got %q after root key rotation, want %q", got, "pre-rotation content")
	}
}

// TestRotateRootKeyRejectsWrongPreviousKey guards the failure mode
// TestRotateRootKeyKeepsProjectKeysDecryptable's first draft actually hit:
// RotateRootKey must refuse to proceed (not silently corrupt project keys)
// if the supplied `previous` provider doesn't match what a row was really
// wrapped under.
func TestRotateRootKeyRejectsWrongPreviousKey(t *testing.T) {
	dsns := dbtest.New(t)
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsns.Owner)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	projectID := seedProject(t, ctx, pool)
	provider := newTestProvider(t)

	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		_, _, _, _, err := provider.Encrypt(ctx, tx, crypto.EncryptionContext{
			ProjectID: projectID, ResourceType: "artifact_version",
			ResourceID: uuid.New(), ResourceVersion: 1,
		}, []byte("content"))
		return err
	})
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	wrongPreviousB64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv("CC_WRONG_ROOT_KEY", wrongPreviousB64)
	wrongPrevious, err := crypto.NewEnvRootKeyProvider("CC_WRONG_ROOT_KEY")
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}

	newRootB64, err := crypto.GenerateRootKeyBase64()
	if err != nil {
		t.Fatalf("GenerateRootKeyBase64: %v", err)
	}
	t.Setenv("CC_ROOT_KEY", newRootB64)
	newRoot, err := crypto.NewEnvRootKeyProvider("CC_ROOT_KEY")
	if err != nil {
		t.Fatalf("NewEnvRootKeyProvider: %v", err)
	}

	err = db.WithoutPrincipal(ctx, pool, func(tx pgx.Tx) error {
		return crypto.NewProvider(newRoot).RotateRootKey(ctx, tx, wrongPrevious)
	})
	if err == nil {
		t.Fatal("RotateRootKey with a previous-key provider that doesn't match the wrapped row should fail, got nil error")
	}
}
