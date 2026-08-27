package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateForOwner creates a project at "namespace/slug" — creating a
// personal namespace for ownerUserID first if it doesn't exist yet — and
// adds principalID as its OWNER, all as one call. Every adapter (CLI, HTTP,
// MCP) that lets a caller create a project goes through this instead of
// reimplementing the same three-step sequence: a project committed without
// an immediate self-membership insert would be permanently inaccessible to
// everyone, itself included (docs/adr/0015-project-creation-rls-bootstrap.md).
//
// ownerUserID and principalID are deliberately separate parameters rather
// than this package resolving a Principal itself — that would pull
// internal/identity into internal/projects for no benefit; the caller
// already has both from wherever it authenticated the request.
func (s *Store) CreateForOwner(ctx context.Context, tx pgx.Tx, ownerUserID, principalID uuid.UUID, path, name string) (Project, error) {
	namespaceSlug, projectSlug, ok := strings.Cut(path, "/")
	if !ok || namespaceSlug == "" || projectSlug == "" {
		return Project{}, ErrInvalidResolvePath
	}

	ns, err := s.GetNamespaceBySlug(ctx, tx, namespaceSlug)
	if errors.Is(err, ErrNotFound) {
		ns, err = s.CreatePersonalNamespace(ctx, tx, ownerUserID, namespaceSlug)
	}
	if err != nil {
		return Project{}, fmt.Errorf("projects: resolve/create namespace %q: %w", namespaceSlug, err)
	}
	if ns.OwnerUserID == nil || *ns.OwnerUserID != ownerUserID {
		return Project{}, fmt.Errorf("projects: namespace %q is not yours to create a project in", namespaceSlug)
	}

	project, err := s.Create(ctx, tx, ns.ID, projectSlug, name)
	if err != nil {
		return Project{}, fmt.Errorf("projects: create: %w", err)
	}
	if _, err := s.AddMember(ctx, tx, project.ID, principalID, RoleOwner); err != nil {
		return Project{}, fmt.Errorf("projects: add self as OWNER: %w", err)
	}
	return project, nil
}
