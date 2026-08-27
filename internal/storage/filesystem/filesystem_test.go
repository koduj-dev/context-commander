package filesystem_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/koduj-dev/context-commander/internal/storage"
	"github.com/koduj-dev/context-commander/internal/storage/filesystem"
)

func TestPutGetRoundTrip(t *testing.T) {
	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
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
	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := store.Get(context.Background(), "does/not/exist"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get missing key: got err=%v, want ErrNotFound", err)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
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

// TestRejectsKeysThatEscapeRoot is the pitfall this backend exists to
// avoid: a key must never be able to read/write outside the store's root
// directory.
func TestRejectsKeysThatEscapeRoot(t *testing.T) {
	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	content := []byte("x")

	badKeys := []string{
		"../escape",
		"../../etc/passwd",
		"/etc/passwd",
		"a/../../escape",
		"",
	}
	for _, key := range badKeys {
		if _, err := store.Put(ctx, key, bytes.NewReader(content), int64(len(content))); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Put(%q): got err=%v, want ErrInvalidKey", key, err)
		}
		if _, err := store.Get(ctx, key); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Get(%q): got err=%v, want ErrInvalidKey", key, err)
		}
	}
}

func TestPutRejectsSizeMismatch(t *testing.T) {
	store, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	content := []byte("hello")
	if _, err := store.Put(context.Background(), "k", bytes.NewReader(content), 999); err == nil {
		t.Fatal("Put with a size that doesn't match the reader's actual content should fail")
	}
}
