package s3_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/koduj-dev/context-commander/internal/storage"
	cstorage "github.com/koduj-dev/context-commander/internal/storage/s3"
)

// newTestStore starts a disposable MinIO container (S3-compatible) so this
// package's tests don't depend on `docker compose up` having been run
// beforehand — same rationale as internal/dbtest for Postgres.
func newTestStore(t *testing.T) *cstorage.Store {
	t.Helper()
	ctx := context.Background()

	const (
		user     = "cc_test"
		password = "cc_test_password"
	)

	container, err := tcminio.Run(ctx, "minio/minio:latest",
		tcminio.WithUsername(user),
		tcminio.WithPassword(password),
	)
	if err != nil {
		t.Fatalf("start minio container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate minio container: %v", err)
		}
	})

	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("minio connection string: %v", err)
	}

	store, err := cstorage.New(ctx, cstorage.Config{
		Bucket:          "cc-test-bucket",
		Endpoint:        "http://" + endpoint,
		UsePathStyle:    true,
		AccessKeyID:     user,
		SecretAccessKey: password,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	return store
}

func TestPutGetRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	content := []byte("ciphertext bytes, opaque to this layer")

	etag, err := store.Put(ctx, "projects/p1/artifacts/a1/v1", bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if etag == "" {
		t.Error("Put returned an empty etag")
	}

	r, err := store.Get(ctx, "projects/p1/artifacts/a1/v1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = r.Close() }()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestGetMissingKeyReturnsErrNotFound(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Get(context.Background(), "does/not/exist"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get missing key: got err=%v, want ErrNotFound", err)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.Delete(ctx, "never/written"); err != nil {
		t.Fatalf("Delete of a nonexistent key should not error, got: %v", err)
	}

	content := []byte("x")
	if _, err := store.Put(ctx, "k", bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, "k"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get after Delete: got err=%v, want ErrNotFound", err)
	}
}
