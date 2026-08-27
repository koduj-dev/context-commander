// Package filesystem is a local-disk ObjectStore implementation for
// development, using the same crypto path as the s3 backend — see
// internal/storage's package doc.
package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/koduj-dev/context-commander/internal/storage"
)

type Store struct {
	root string
}

// New creates a filesystem-backed ObjectStore rooted at dir, which must
// already exist.
func New(dir string) (*Store, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("storage/filesystem: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("storage/filesystem: %q is not a directory", dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("storage/filesystem: %w", err)
	}
	return &Store{root: abs}, nil
}

// resolve maps a logical key to an on-disk path, rejecting anything that
// could escape root (absolute paths, "..", empty segments) — a hostile or
// buggy key must not be able to read/write outside the store's directory.
func (s *Store) resolve(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") {
		return "", storage.ErrInvalidKey
	}
	cleaned := filepath.Clean(key)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, string(filepath.Separator)+"..") {
		return "", storage.ErrInvalidKey
	}

	full := filepath.Join(s.root, cleaned)
	if full != s.root && !strings.HasPrefix(full, s.root+string(filepath.Separator)) {
		return "", storage.ErrInvalidKey
	}
	return full, nil
}

// Put writes atomically: content lands fully in a temp file in the same
// directory, then a single os.Rename makes it visible at key — a reader
// never observes a partially-written object, and a crash mid-write leaves
// only an orphan temp file, never a truncated "real" object.
func (s *Store) Put(_ context.Context, key string, r io.Reader, size int64) (string, error) {
	path, err := s.resolve(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("storage/filesystem: mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("storage/filesystem: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath) // no-op once Rename below has succeeded
	}()

	hasher := sha256.New()
	written, err := io.Copy(tmp, io.TeeReader(io.LimitReader(r, size), hasher))
	closeErr := tmp.Close()
	if err != nil {
		return "", fmt.Errorf("storage/filesystem: write: %w", err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("storage/filesystem: close temp file: %w", closeErr)
	}
	if written != size {
		return "", fmt.Errorf("storage/filesystem: wrote %d bytes, expected %d", written, size)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("storage/filesystem: rename into place: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (s *Store) Get(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path) //nolint:gosec // path is built and bounds-checked by resolve(), never raw user input
	if errors.Is(err, os.ErrNotExist) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage/filesystem: open: %w", err)
	}
	return f, nil
}

func (s *Store) Delete(_ context.Context, key string) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storage/filesystem: remove: %w", err)
	}
	return nil
}

func (s *Store) List(_ context.Context, prefix string) ([]storage.ObjectInfo, error) {
	root, err := s.resolve(prefix)
	if err != nil {
		// A prefix that isn't a valid key in its own right (e.g. it just
		// doesn't exist yet) is not an error for List — it just means "no
		// objects," matching S3's ListObjectsV2 behavior for an absent prefix.
		if errors.Is(err, storage.ErrInvalidKey) {
			return nil, err
		}
	}

	var out []storage.ObjectInfo
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil // prefix directory doesn't exist yet: no objects, not an error
		}
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, storage.ObjectInfo{
			Key:          filepath.ToSlash(rel),
			LastModified: info.ModTime(),
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("storage/filesystem: list %q: %w", prefix, walkErr)
	}
	return out, nil
}

var _ storage.ObjectStore = (*Store)(nil)
