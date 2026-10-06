package s3_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/koduj-dev/context-commander/internal/storage"
	cstorage "github.com/koduj-dev/context-commander/internal/storage/s3"
)

// The S3 server these tests run against.
//
// It used to be MinIO, via testcontainers' module for it. MinIO withdrew its
// community images from the public registries in 2025 — `minio/minio` now
// answers "pull access denied ... repository does not exist", on Docker Hub
// and on quay.io, for `latest` and for pinned releases alike. The failure
// arrived in CI rather than locally, because a machine that has run
// `make dev-up` already holds the image in its cache.
//
// adobe/s3mock rather than localstack: it implements the S3 API and nothing
// else, which is all this package needs, and starts in about a second. It
// authenticates nothing, so the credentials below are arbitrary — which is
// honest for a test of our own code's S3 interaction rather than of a
// server's access control.
const (
	s3mockImage = "adobe/s3mock:latest"
	s3mockPort  = "9090/tcp"
	testBucket  = "cc-test-bucket"
)

// newTestStore starts a disposable S3 server so this package's tests don't
// depend on `docker compose up` having been run beforehand — same rationale
// as internal/dbtest for Postgres.
func newTestStore(t *testing.T) *cstorage.Store {
	t.Helper()
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        s3mockImage,
			ExposedPorts: []string{s3mockPort},
			// The bucket is created by the server at startup rather than by
			// EnsureBucket below, so a failure in EnsureBucket is about our
			// code and not about a race with a server still booting.
			Env:        map[string]string{"initialBuckets": testBucket},
			WaitingFor: wait.ForListeningPort(s3mockPort),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start s3 container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate s3 container: %v", err)
		}
	})

	endpoint, err := container.PortEndpoint(ctx, s3mockPort, "http")
	if err != nil {
		t.Fatalf("s3 endpoint: %v", err)
	}

	store, err := cstorage.New(ctx, cstorage.Config{
		Bucket:          testBucket,
		Endpoint:        endpoint,
		UsePathStyle:    true,
		AccessKeyID:     "cc_test",
		SecretAccessKey: "cc_test_password",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Still called, and still has to succeed: creating a bucket that already
	// exists is the normal case in a real deployment and must not be an
	// error.
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
