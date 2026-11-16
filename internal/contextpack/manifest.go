package contextpack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/artifacts"
	"github.com/koduj-dev/context-commander/internal/paging"
)

// ManifestPath is where a project declares what an arriving agent must read
// (docs/adr/0034). It is an ordinary artifact, so it is versioned, written
// like any other file and readable by everyone who can read the project.
// It lives under ConventionalArtifactPrefix on purpose: a project that adds
// it keeps its context in one place. It is never carried as content.
const ManifestPath = ConventionalArtifactPrefix + "manifest.json"

// MaxManifestEntries bounds how many entries a manifest may list.
const MaxManifestEntries = 100

// Manifest is the declared reading list.
//
//	{
//	  "version": 1,
//	  "budget": 200000,
//	  "read": [
//	    {"path": "context/overview.md", "why": "what the project is"},
//	    {"path": "docs/architecture.md", "max_bytes": 20000},
//	    {"prefix": "docs/adr/", "max_bytes": 4000}
//	  ]
//	}
//
// Entries are read in order, after the artifacts the current handoff names.
// Budget, when set, is the pack's default budget; a caller's own budget
// still wins. MaxBytes caps one entry: a longer artifact is carried cut at
// that size, with a note to read the rest, rather than left out whole.
type Manifest struct {
	Version int             `json:"version"`
	Budget  int             `json:"budget,omitempty"`
	Read    []ManifestEntry `json:"read"`
}

// ManifestEntry is one artifact, or every artifact under a prefix.
type ManifestEntry struct {
	Path     string `json:"path,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"`
	Why      string `json:"why,omitempty"`
}

// ParseManifest checks a manifest's shape. Unknown fields are refused, so a
// misspelled "max_byte" is an error rather than a silent no-op.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("not valid manifest JSON: %w", err)
	}
	if m.Version != 1 {
		return Manifest{}, fmt.Errorf("version must be 1, got %d", m.Version)
	}
	if m.Budget < 0 || m.Budget > MaxBudget {
		return Manifest{}, fmt.Errorf("budget must be between 0 and %d", MaxBudget)
	}
	if len(m.Read) > MaxManifestEntries {
		return Manifest{}, fmt.Errorf("at most %d entries, got %d", MaxManifestEntries, len(m.Read))
	}
	for i, e := range m.Read {
		if (e.Path == "") == (e.Prefix == "") {
			return Manifest{}, fmt.Errorf("entry %d: exactly one of path and prefix", i+1)
		}
		if e.MaxBytes < 0 {
			return Manifest{}, fmt.Errorf("entry %d: max_bytes must not be negative", i+1)
		}
	}
	return m, nil
}

// selection is one artifact the pack will try to carry, with what the
// manifest said about it.
type selection struct {
	path     string
	maxBytes int
	why      string
}

// readManifest returns the project's manifest, nil when it has none, or an
// error saying why the one it has cannot be used.
func (s *Service) readManifest(ctx context.Context, principalID, projectID uuid.UUID) (*Manifest, error) {
	_, raw, err := s.artifacts.Read(ctx, principalID, projectID, ManifestPath, nil)
	if errors.Is(err, artifacts.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// expandManifest turns entries into paths, prefixes listed in the query
// like the convention. A prefix with more than MaxConventionalArtifacts
// artifacts is cut there and reported.
func (s *Service) expandManifest(ctx context.Context, principalID, projectID uuid.UUID, m *Manifest, b *budget) []selection {
	var out []selection
	for _, e := range m.Read {
		if e.Path != "" {
			out = append(out, selection{path: e.Path, maxBytes: e.MaxBytes, why: e.Why})
			continue
		}
		page, err := s.artifacts.ListByPathPrefix(ctx, principalID, projectID, e.Prefix, paging.Options{Limit: MaxConventionalArtifacts})
		if err != nil {
			b.note("artifact", e.Prefix, 0, reasonFor(err, "could not list the manifest's prefix"))
			continue
		}
		for _, a := range page.Items {
			if a.LogicalPath != ManifestPath {
				out = append(out, selection{path: a.LogicalPath, maxBytes: e.MaxBytes, why: e.Why})
			}
		}
		if page.NextCursor != "" {
			b.note("artifact", e.Prefix, 0,
				fmt.Sprintf("omitted: more than %d artifacts under this prefix — list them with artifact.list", MaxConventionalArtifacts))
		}
	}
	return out
}

// cut shortens content to at most n bytes without splitting a UTF-8
// sequence.
func cut(content []byte, n int) []byte {
	if len(content) <= n {
		return content
	}
	content = content[:n]
	for len(content) > 0 && !utf8.Valid(content) {
		content = content[:len(content)-1]
	}
	return content
}

// manifestNote is the reason attached when a manifest exists and cannot be
// used. The pack falls back to the convention, so an agent still gets
// context/ and is told why the list it may have expected is not there.
func manifestNote(err error) string {
	return "manifest ignored, read context/ by convention instead: " + strings.TrimPrefix(err.Error(), "contextpack: ")
}
